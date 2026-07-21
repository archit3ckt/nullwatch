package wizard

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/huh"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// Each group builder below owns local string mirrors for the int fields huh
// can't bind directly, validates them as part of the form, and returns an
// apply func that writes the parsed values back into desired. Callers must
// run apply only after the form has completed successfully.

func requireNonEmpty(label string) func(string) error {
	return func(s string) error {
		if s == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

func validatePort(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("must be a number")
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("must be between 1 and 65535")
	}
	return nil
}

// validateAdGuardPassword matches AdGuard Home's own minimum, so a weak
// password fails here instead of after the container's already up.
func validateAdGuardPassword(s string) error {
	if len(s) < 8 {
		return fmt.Errorf("must be at least 8 characters (AdGuard's own requirement)")
	}
	return nil
}

// defaultDomain is IANA-reserved for private/internal use (RFC 8375) and
// will never resolve publicly or collide with a real domain. Since
// Traefik's cert is self-signed and everything here is VPN-only anyway,
// this domain never needs to be one you actually own — it only needs to
// mean something to AdGuard's own DNS rewrite for VPN clients.
const defaultDomain = "home.arpa"

func domainGroup(desired *config.Config) *huh.Group {
	if desired.Global.Domain == "" {
		desired.Global.Domain = defaultDomain
	}
	return huh.NewGroup(
		huh.NewInput().
			Title("Base domain").
			Description("Traefik routes *.<domain> to backend containers; AdGuard resolves it internally for VPN clients. Defaults to home.arpa, reserved for exactly this kind of private use — change it only if you specifically want something else.").
			Value(&desired.Global.Domain).
			Validate(requireNonEmpty("domain")),
	)
}

func adguardGroup(cfg *config.AdGuardConfig) *huh.Group {
	httpPort := strconv.Itoa(cfg.HTTPPort)
	dnsPort := strconv.Itoa(cfg.DNSPort)

	group := huh.NewGroup(
		huh.NewInput().
			Title("AdGuard web UI port").
			Value(&httpPort).
			Validate(validatePort),
		huh.NewInput().
			Title("AdGuard DNS port").
			Value(&dnsPort).
			Validate(validatePort),
		huh.NewInput().
			Title("AdGuard admin username").
			Value(&cfg.AdminUser).
			Validate(requireNonEmpty("admin username")),
		huh.NewInput().
			Title("AdGuard admin password").
			Password(true).
			Value(&cfg.AdminPassword).
			Validate(validateAdGuardPassword),
		huh.NewMultiSelect[string]().
			Title("Blocklists").
			Description("Tracker/analytics/telemetry lists, not just ad-blocking — this is the DNS-level privacy layer.").
			Options(blocklistOptions(cfg.Blocklists)...).
			Value(&cfg.Blocklists),
	)

	pendingIntFields = append(pendingIntFields, func() {
		cfg.HTTPPort, _ = strconv.Atoi(httpPort)
		cfg.DNSPort, _ = strconv.Atoi(dnsPort)
	})

	return group
}

func wireguardGroup(cfg *config.WireGuardConfig) *huh.Group {
	port := strconv.Itoa(cfg.Port)
	webUIPort := strconv.Itoa(cfg.WebUIPort)

	group := huh.NewGroup(
		huh.NewInput().
			Title("WireGuard host").
			Description("Public IP or DNS name clients will connect to.").
			Value(&cfg.Host).
			Validate(requireNonEmpty("host")),
		huh.NewInput().
			Title("WireGuard UDP port").
			Value(&port).
			Validate(validatePort),
		huh.NewInput().
			Title("VPN subnet (CIDR, e.g. 10.8.0.0/24)").
			Value(&cfg.Subnet).
			Validate(requireNonEmpty("subnet")),
		huh.NewInput().
			Title("Web UI port").
			Value(&webUIPort).
			Validate(validatePort),
		huh.NewInput().
			Title("Web UI password").
			Password(true).
			Value(&cfg.WebUIPassword).
			Validate(requireNonEmpty("web UI password")),
	)

	pendingIntFields = append(pendingIntFields, func() {
		cfg.Port, _ = strconv.Atoi(port)
		cfg.WebUIPort, _ = strconv.Atoi(webUIPort)
	})

	return group
}

func traefikGroup(cfg *config.TraefikConfig) *huh.Group {
	httpPort := strconv.Itoa(cfg.HTTPPort)
	httpsPort := strconv.Itoa(cfg.HTTPSPort)
	dashboardPort := strconv.Itoa(cfg.DashboardPort)

	group := huh.NewGroup(
		huh.NewInput().
			Title("HTTP port").
			Value(&httpPort).
			Validate(validatePort),
		huh.NewInput().
			Title("HTTPS port").
			Value(&httpsPort).
			Validate(validatePort),
		huh.NewConfirm().
			Title("Enable the Traefik dashboard?").
			Description("Reachable only over the WireGuard tunnel, not the public internet.").
			Value(&cfg.DashboardEnabled),
		huh.NewInput().
			Title("Dashboard port").
			Value(&dashboardPort).
			Validate(validatePort),
	)

	pendingIntFields = append(pendingIntFields, func() {
		cfg.HTTPPort, _ = strconv.Atoi(httpPort)
		cfg.HTTPSPort, _ = strconv.Atoi(httpsPort)
		cfg.DashboardPort, _ = strconv.Atoi(dashboardPort)
	})

	return group
}

// pendingIntFields accumulates the string->int commit closures created by
// the group builders above. wizard.Run drains and calls them after the form
// completes successfully. Reset at the start of every Run so repeated calls
// within a process (tests, or a future non-exiting loop) don't double-apply.
var pendingIntFields []func()

func commitIntFields() {
	for _, f := range pendingIntFields {
		f()
	}
	pendingIntFields = nil
}

func blocklistOptions(currentlyEnabled []string) []huh.Option[string] {
	curated := []struct{ label, url string }{
		{"AdGuard DNS filter (ads)", "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt"},
		{"AdGuard Tracking Protection filter (trackers/analytics)", "https://adguardteam.github.io/HostlistsRegistry/assets/filter_3.txt"},
		{"OISD Big (ads + trackers, broad coverage)", "https://big.oisd.nl/domainswild"},
		{"Steven Black hosts (ads + malware)", "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"},
	}

	enabled := map[string]bool{}
	for _, u := range currentlyEnabled {
		enabled[u] = true
	}

	opts := make([]huh.Option[string], 0, len(curated))
	for _, c := range curated {
		opts = append(opts, huh.NewOption(c.label, c.url).Selected(enabled[c.url]))
	}
	return opts
}
