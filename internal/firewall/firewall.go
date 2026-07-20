// Package firewall locks the host down via ufw so that nothing except the
// WireGuard tunnel is reachable from the public internet — every admin UI
// (AdGuard, WireGuard's own admin panel, Traefik, CasaOS) is only allowed
// from the WireGuard client subnet and localhost. If you're locked out of
// an admin UI, that's what SSH + nullwatch itself are for — this firewall
// step always allows SSH first, specifically so that door never closes.
package firewall

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// rule is one `ufw <args...>` invocation.
type rule struct {
	args    []string
	comment string
}

// detectSSHPort reads a custom Port directive from sshd_config, falling
// back to 22. Getting this right matters: it's the one rule that must never
// be skipped.
func detectSSHPort() int {
	data, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return 22
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[0], "Port") {
			if p, err := strconv.Atoi(fields[1]); err == nil {
				return p
			}
		}
	}
	return 22
}

type portProto struct {
	port  int
	proto string
	label string
}

func buildRules(cfg *config.Config) []rule {
	sshPort := detectSSHPort()
	rules := []rule{{
		args:    []string{"allow", fmt.Sprintf("%d/tcp", sshPort)},
		comment: fmt.Sprintf("SSH (port %d) — always allowed, never restricted", sshPort),
	}}

	if cfg.WireGuard == nil || !cfg.WireGuard.Enabled || cfg.WireGuard.Subnet == "" {
		return rules
	}

	rules = append(rules, rule{
		args:    []string{"allow", fmt.Sprintf("%d/udp", cfg.WireGuard.Port)},
		comment: "WireGuard tunnel — the one thing meant to be internet-facing",
	})

	var restricted []portProto
	if cfg.AdGuard != nil && cfg.AdGuard.Enabled {
		restricted = append(restricted,
			portProto{cfg.AdGuard.HTTPPort, "tcp", "AdGuard UI"},
			portProto{cfg.AdGuard.DNSPort, "tcp", "AdGuard DNS"},
			portProto{cfg.AdGuard.DNSPort, "udp", "AdGuard DNS"},
		)
	}
	restricted = append(restricted, portProto{cfg.WireGuard.WebUIPort, "tcp", "WireGuard admin UI"})
	if cfg.Traefik != nil && cfg.Traefik.Enabled {
		restricted = append(restricted,
			portProto{cfg.Traefik.HTTPPort, "tcp", "Traefik HTTP"},
			portProto{cfg.Traefik.HTTPSPort, "tcp", "Traefik HTTPS"},
		)
		if cfg.Traefik.DashboardEnabled {
			restricted = append(restricted, portProto{cfg.Traefik.DashboardPort, "tcp", "Traefik dashboard"})
		}
	}
	restricted = append(restricted, portProto{81, "tcp", "CasaOS"})

	for _, source := range []string{cfg.WireGuard.Subnet, "127.0.0.1/32"} {
		for _, p := range restricted {
			rules = append(rules, rule{
				args:    []string{"allow", "from", source, "to", "any", "port", strconv.Itoa(p.port), "proto", p.proto},
				comment: fmt.Sprintf("%s (port %d/%s) — only from %s", p.label, p.port, p.proto, source),
			})
		}
	}

	return rules
}

// Apply shows the exact ufw rules it's about to run and, on confirmation,
// applies them: allow SSH and the WireGuard port from anywhere, restrict
// every other managed port to the WireGuard subnet and localhost, then set
// default-deny on incoming traffic and enable ufw.
func Apply(cfg *config.Config) error {
	if _, err := exec.LookPath("ufw"); err != nil {
		return fmt.Errorf("ufw not found — install it (e.g. `sudo apt install ufw`) and re-run")
	}

	rules := buildRules(cfg)

	fmt.Println("The following firewall rules will be applied:")
	for _, r := range rules {
		fmt.Printf("  ufw %-60s # %s\n", strings.Join(r.args, " "), r.comment)
	}
	fmt.Println("  ufw default deny incoming")
	fmt.Println("  ufw default allow outgoing")
	fmt.Println("  ufw --force enable")

	confirm := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Apply these firewall rules?").
			Description("SSH is always allowed first, before anything is restricted, so you can't be locked out. Everything except SSH and the WireGuard tunnel becomes reachable only from the VPN subnet and localhost.").
			Affirmative("Apply").
			Negative("Skip").
			Value(&confirm),
	)).Run(); err != nil {
		return fmt.Errorf("firewall prompt: %w", err)
	}
	if !confirm {
		return nil
	}

	for _, r := range rules {
		if err := run("ufw", r.args...); err != nil {
			return fmt.Errorf("ufw %s: %w", strings.Join(r.args, " "), err)
		}
	}
	if err := run("ufw", "default", "deny", "incoming"); err != nil {
		return err
	}
	if err := run("ufw", "default", "allow", "outgoing"); err != nil {
		return err
	}
	if err := run("ufw", "--force", "enable"); err != nil {
		return err
	}

	fmt.Println("==> firewall rules applied")
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{name}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
