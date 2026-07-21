// Package firewall locks the host down so that nothing except the
// WireGuard tunnel is reachable from the public internet — full stop,
// regardless of what's actually running. Rather than allowlisting specific
// known ports (AdGuard, Traefik, ...), which can never keep up with
// whatever CasaOS's app store lets you install later, the entire WireGuard
// subnet is trusted for every port: connect to the VPN, and everything on
// this host is reachable; don't, and only SSH and the tunnel itself are.
//
// This talks to iptables directly rather than going through ufw. ufw was
// the first approach here, but it turned out to fail ("ERROR: problem
// running", no further detail, no log output) on *every* new rule addition
// on at least one real deployment (Debian trixie, ufw 0.36.2) — confirmed
// via a direct side-by-side comparison against raw iptables-legacy, which
// worked correctly for the identical operation.
//
// Two separate chains need rules, covering both IPv4 and IPv6 (Docker
// publishes container ports on both by default) — and, on hosts where both
// the legacy and nftables-compat iptables frontends are present, they need
// different binaries entirely:
//
//   - INPUT, for native host processes (sshd, CasaOS's own gateway
//     service). Managed via iptables-legacy/ip6tables-legacy, which is not
//     Docker-managed and confirmed to work directly.
//   - DOCKER-USER, for Docker's published container ports (AdGuard,
//     Traefik, WireGuard's admin UI, and anything installed later via
//     CasaOS). Docker manipulates iptables' nat/FORWARD chains directly to
//     expose published ports, ahead of INPUT-chain rules entirely — a
//     restrictive INPUT policy has no effect on them. DOCKER-USER is the
//     chain Docker deliberately leaves empty for exactly this purpose,
//     evaluated before its own permissive rules. Confirmed (on a real
//     deployment) that Docker creates this chain via the nftables-compat
//     frontend specifically — iptables-legacy reports "No chain/target/
//     match by that name" for it, while iptables-nft finds and writes to
//     it fine. So DOCKER-USER rules go through iptables-nft/ip6tables-nft,
//     regardless of which alternative the plain "iptables" command
//     currently resolves to.
//
// Persistence also needs two different mechanisms to match: INPUT (legacy)
// persists via iptables-persistent/netfilter-persistent as usual. But
// Docker recreates DOCKER-USER *empty* every time dockerd starts, and
// netfilter-persistent only restores whichever backend the "iptables"
// alternative points at (legacy) — so the DOCKER-USER (nft) rules would
// silently vanish on every reboot without something re-applying them after
// Docker starts. A systemd oneshot unit, ordered After=docker.service,
// handles that instead of trying to race dockerd's own startup with a raw
// nft ruleset restore.
//
// Rules are applied idempotently (checked with -C before adding with -A).
//
// If you're locked out of something, that's what SSH + nullwatch are for —
// SSH is always allowed first, specifically so that door never closes.
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

// detectPublicInterface returns the network interface carrying the default
// route — the one facing the public internet. DOCKER-USER rules are scoped
// to it specifically so they never touch container-to-container traffic on
// the internal Docker bridge, only externally-arriving traffic.
func detectPublicInterface() (string, error) {
	out, err := exec.Command("sh", "-c", "ip route show default | awk '{print $5; exit}'").Output()
	if err != nil {
		return "", fmt.Errorf("detect public network interface: %w", err)
	}
	iface := strings.TrimSpace(string(out))
	if iface == "" {
		return "", fmt.Errorf("could not determine the public network interface (no default route?)")
	}
	return iface, nil
}

// rule is one rule to ensure exists in a chain, for a specific binary.
type rule struct {
	bin   string
	chain string
	args  []string
}

func buildInputRules(sshPort, wgPort int, wgSubnet string) []rule {
	rules := []rule{
		{"iptables-legacy", "INPUT", []string{"-i", "lo", "-j", "ACCEPT"}},
		{"iptables-legacy", "INPUT", []string{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}},
		{"iptables-legacy", "INPUT", []string{"-p", "tcp", "--dport", strconv.Itoa(sshPort), "-j", "ACCEPT"}},
		{"ip6tables-legacy", "INPUT", []string{"-i", "lo", "-j", "ACCEPT"}},
		{"ip6tables-legacy", "INPUT", []string{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}},
		{"ip6tables-legacy", "INPUT", []string{"-p", "tcp", "--dport", strconv.Itoa(sshPort), "-j", "ACCEPT"}},
	}
	if wgSubnet == "" {
		return rules
	}
	wgPortStr := strconv.Itoa(wgPort)
	return append(rules,
		rule{"iptables-legacy", "INPUT", []string{"-p", "udp", "--dport", wgPortStr, "-j", "ACCEPT"}},
		rule{"iptables-legacy", "INPUT", []string{"-s", wgSubnet, "-j", "ACCEPT"}},
		rule{"iptables-legacy", "INPUT", []string{"-s", "127.0.0.1", "-j", "ACCEPT"}},
		rule{"ip6tables-legacy", "INPUT", []string{"-p", "udp", "--dport", wgPortStr, "-j", "ACCEPT"}},
		rule{"ip6tables-legacy", "INPUT", []string{"-s", "::1", "-j", "ACCEPT"}},
	)
}

// dockerUserRules returns the DOCKER-USER rules: trust the VPN subnet and
// the tunnel port for every other Docker-published port, drop everything
// else arriving via the public interface. No IPv6 VPN client subnet exists
// in this setup, so IPv6 only keeps the tunnel port itself open.
//
// Also RETURNs already-established/related connections first — this is
// the full-tunnel VPN's gateway path: a client's own outbound request (say,
// to a website) leaves fine regardless of this chain, but the response
// packet arrives back in via the public interface, from the site's IP, not
// the WireGuard subnet or port. Without this exception it's indistinguishable
// from unsolicited inbound traffic and gets dropped — silently breaking
// internet access over the VPN despite the tunnel itself working fine.
func dockerUserRules(wgPort int, wgSubnet, iface string) []rule {
	if wgSubnet == "" {
		return nil
	}
	wgPortStr := strconv.Itoa(wgPort)
	return []rule{
		{"iptables-nft", "DOCKER-USER", []string{"-i", iface, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN"}},
		{"iptables-nft", "DOCKER-USER", []string{"-i", iface, "-s", wgSubnet, "-j", "RETURN"}},
		{"iptables-nft", "DOCKER-USER", []string{"-i", iface, "-p", "udp", "--dport", wgPortStr, "-j", "RETURN"}},
		{"iptables-nft", "DOCKER-USER", []string{"-i", iface, "-j", "DROP"}},
		{"ip6tables-nft", "DOCKER-USER", []string{"-i", iface, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN"}},
		{"ip6tables-nft", "DOCKER-USER", []string{"-i", iface, "-p", "udp", "--dport", wgPortStr, "-j", "RETURN"}},
		{"ip6tables-nft", "DOCKER-USER", []string{"-i", iface, "-j", "DROP"}},
	}
}

// applyRule adds r if it isn't already present (checked via -C), so
// re-applying is idempotent instead of accumulating duplicates.
func applyRule(r rule) error {
	checkArgs := append([]string{"-C", r.chain}, r.args...)
	if exec.Command("sudo", append([]string{r.bin}, checkArgs...)...).Run() == nil {
		return nil // already present
	}
	addArgs := append([]string{"-A", r.chain}, r.args...)
	if err := run(r.bin, addArgs...); err != nil {
		return fmt.Errorf("%s %s: %w", r.bin, strings.Join(addArgs, " "), err)
	}
	return nil
}

// ensureInputPersistence installs iptables-persistent if missing
// (pre-seeding its install-time prompts so it doesn't block waiting for
// input) and saves the current legacy rules, so INPUT survives a reboot.
func ensureInputPersistence() error {
	// Check for the actual binary, not `dpkg -s`: dpkg still reports a
	// package as "known" (deinstall ok config-files) after a plain
	// removal, which would incorrectly skip reinstalling it here.
	if _, err := exec.LookPath("netfilter-persistent"); err != nil {
		if err := run("apt-get", "update", "-qq"); err != nil {
			return fmt.Errorf("apt-get update: %w", err)
		}
		preseed := "iptables-persistent iptables-persistent/autosave_v4 boolean true\n" +
			"iptables-persistent iptables-persistent/autosave_v6 boolean true\n"
		cmd := exec.Command("sudo", "debconf-set-selections")
		cmd.Stdin = strings.NewReader(preseed)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("debconf-set-selections: %w\n%s", err, out)
		}
		if err := run("apt-get", "install", "-y", "iptables-persistent"); err != nil {
			return fmt.Errorf("apt-get install iptables-persistent: %w", err)
		}
	}
	if err := run("netfilter-persistent", "save"); err != nil {
		return fmt.Errorf("netfilter-persistent save: %w", err)
	}
	return nil
}

const dockerUserUnitScript = "/usr/local/sbin/nullwatch-docker-user-firewall.sh"
const dockerUserUnitPath = "/etc/systemd/system/nullwatch-docker-user-firewall.service"

// ensureDockerUserPersistence installs a systemd oneshot unit that
// re-applies the DOCKER-USER rules after every docker.service start, since
// Docker recreates that chain empty on its own startup and
// netfilter-persistent can't reach it (see package doc).
func ensureDockerUserPersistence(rules []rule) error {
	var script strings.Builder
	script.WriteString("#!/bin/sh\nset -eu\n")
	script.WriteString("add_if_missing() {\n\tbin=\"$1\"; chain=\"$2\"\n\tshift 2\n\tif ! \"$bin\" -C \"$chain\" \"$@\" 2>/dev/null; then\n\t\t\"$bin\" -A \"$chain\" \"$@\"\n\tfi\n}\n")
	for _, r := range rules {
		script.WriteString(fmt.Sprintf("add_if_missing %s %s %s\n", r.bin, r.chain, strings.Join(r.args, " ")))
	}

	writeCmd := exec.Command("sudo", "tee", dockerUserUnitScript)
	writeCmd.Stdin = strings.NewReader(script.String())
	if out, err := writeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write %s: %w\n%s", dockerUserUnitScript, err, out)
	}
	if err := run("chmod", "+x", dockerUserUnitScript); err != nil {
		return err
	}

	unit := fmt.Sprintf(`[Unit]
Description=Re-apply nullwatch's DOCKER-USER firewall rules (docker recreates this chain empty on every start)
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
ExecStart=%s
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`, dockerUserUnitScript)

	writeUnit := exec.Command("sudo", "tee", dockerUserUnitPath)
	writeUnit.Stdin = strings.NewReader(unit)
	if out, err := writeUnit.CombinedOutput(); err != nil {
		return fmt.Errorf("write %s: %w\n%s", dockerUserUnitPath, err, out)
	}

	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "enable", "--now", "nullwatch-docker-user-firewall.service"); err != nil {
		return err
	}
	return nil
}

// knownUfwChains are the custom chains ufw creates in INPUT. Cleaned up
// whenever ufw is found, since removing the package alone doesn't clean up
// rules it already inserted — confirmed on a real deployment, this
// silently left INPUT's policy at ACCEPT with ufw's jump-chains (and a
// couple of its own stray allow rules) still live in the ruleset even
// after `apt purge ufw` had already run.
var knownUfwChains = []string{
	"ufw-before-logging-input", "ufw-before-input", "ufw-after-input",
	"ufw-after-logging-input", "ufw-reject-input", "ufw-track-input",
}
var knownUfw6Chains = []string{
	"ufw6-before-logging-input", "ufw6-before-input", "ufw6-after-input",
	"ufw6-after-logging-input", "ufw6-reject-input", "ufw6-track-input",
}

// purgeUfw removes ufw and any chains it left behind in INPUT. Safe to run
// even if ufw was never installed — apt purge on an absent package and
// flushing/deleting nonexistent chains both no-op harmlessly. This package
// manages INPUT/DOCKER-USER directly; ufw managing the same chains at the
// same time is exactly what caused the corruption above.
func purgeUfw() error {
	if err := run("apt-get", "purge", "-y", "ufw"); err != nil {
		return fmt.Errorf("purge ufw: %w", err)
	}
	for _, chain := range knownUfwChains {
		_ = exec.Command("sudo", "iptables-legacy", "-F", chain).Run()
		_ = exec.Command("sudo", "iptables-legacy", "-X", chain).Run()
	}
	for _, chain := range knownUfw6Chains {
		_ = exec.Command("sudo", "ip6tables-legacy", "-F", chain).Run()
		_ = exec.Command("sudo", "ip6tables-legacy", "-X", chain).Run()
	}
	return nil
}

// Apply shows the exact rules it's about to run and, on confirmation,
// purges ufw (see purgeUfw), applies the rules, sets default-deny on INPUT
// (both protocols), and persists everything across reboots — INPUT via
// netfilter-persistent, DOCKER-USER via a systemd unit ordered after
// docker.service.
func Apply(cfg *config.Config) error {
	for _, bin := range []string{"iptables-legacy", "ip6tables-legacy", "iptables-nft", "ip6tables-nft"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found — it ships with the iptables package, which Docker itself depends on; install it and re-run", bin)
		}
	}

	iface, err := detectPublicInterface()
	if err != nil {
		return err
	}

	sshPort := detectSSHPort()
	var wgPort int
	var wgSubnet string
	if cfg.WireGuard != nil && cfg.WireGuard.Enabled {
		wgPort = cfg.WireGuard.Port
		wgSubnet = cfg.WireGuard.Subnet
	}

	inputRules := buildInputRules(sshPort, wgPort, wgSubnet)
	dockerRules := dockerUserRules(wgPort, wgSubnet, iface)

	fmt.Println("The following will be applied (idempotent — already-present rules are skipped):")
	fmt.Println("  apt-get purge -y ufw (and clean up any of its leftover chains — it manages")
	fmt.Println("  the same chains this does, and the two conflict if both are present)")
	for _, r := range inputRules {
		fmt.Printf("  %s -A %-12s %s\n", r.bin, r.chain, strings.Join(r.args, " "))
	}
	for _, r := range dockerRules {
		fmt.Printf("  %s -A %-12s %s\n", r.bin, r.chain, strings.Join(r.args, " "))
	}
	fmt.Println("  iptables-legacy/ip6tables-legacy -P INPUT DROP")
	fmt.Println("  (persisted: INPUT via iptables-persistent; DOCKER-USER via a systemd unit after docker.service)")

	confirm := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Apply these firewall rules?").
			Description("SSH is always allowed first, before anything is restricted, so you can't be locked out. Everything except SSH and the WireGuard tunnel becomes reachable only from the VPN subnet — whatever's running, container or not, including anything you install later via CasaOS.").
			Affirmative("Apply").
			Negative("Skip").
			Value(&confirm),
	)).Run(); err != nil {
		return fmt.Errorf("firewall prompt: %w", err)
	}
	if !confirm {
		return nil
	}

	if err := purgeUfw(); err != nil {
		return fmt.Errorf("purge ufw: %w", err)
	}

	for _, r := range inputRules {
		if err := applyRule(r); err != nil {
			return err
		}
	}
	for _, r := range dockerRules {
		if err := applyRule(r); err != nil {
			return err
		}
	}

	if err := run("iptables-legacy", "-P", "INPUT", "DROP"); err != nil {
		return err
	}
	if err := run("iptables-legacy", "-P", "FORWARD", "ACCEPT"); err != nil {
		return err
	}
	if err := run("iptables-legacy", "-P", "OUTPUT", "ACCEPT"); err != nil {
		return err
	}
	if err := run("ip6tables-legacy", "-P", "INPUT", "DROP"); err != nil {
		return err
	}
	if err := run("ip6tables-legacy", "-P", "FORWARD", "ACCEPT"); err != nil {
		return err
	}
	if err := run("ip6tables-legacy", "-P", "OUTPUT", "ACCEPT"); err != nil {
		return err
	}

	if err := ensureInputPersistence(); err != nil {
		return fmt.Errorf("persist INPUT rules across reboots: %w", err)
	}
	if len(dockerRules) > 0 {
		if err := ensureDockerUserPersistence(dockerRules); err != nil {
			return fmt.Errorf("persist DOCKER-USER rules across reboots: %w", err)
		}
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
