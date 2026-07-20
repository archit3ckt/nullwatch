// Package firewall locks the host down so that nothing except the
// WireGuard tunnel is reachable from the public internet — full stop,
// regardless of what's actually running. Rather than allowlisting specific
// known ports (AdGuard, Traefik, ...), which can never keep up with
// whatever CasaOS's app store lets you install later, the entire WireGuard
// subnet is trusted for every port: connect to the VPN, and everything on
// this host is reachable; don't, and only SSH and the tunnel itself are.
//
// This needs two separate mechanisms, not just one:
//
//   - ufw governs native host processes (sshd, CasaOS's own gateway
//     service) via the INPUT chain.
//   - Docker's published container ports (AdGuard, Traefik, WireGuard's
//     admin UI, and anything installed later via CasaOS) bypass ufw
//     entirely — Docker manipulates iptables' nat/FORWARD chains directly,
//     ahead of ufw's INPUT-chain rules, so `ufw deny` has no effect on
//     them. The fix is Docker's own DOCKER-USER chain, which it
//     deliberately leaves empty for exactly this purpose and evaluates
//     before its own permissive rules.
//
// If you're locked out of something, that's what SSH + nullwatch are for —
// this step always allows SSH first, specifically so that door never
// closes.
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

func buildUfwRules(cfg *config.Config) []rule {
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

	// Trust the whole VPN subnet (and localhost) for every port, rather
	// than allowlisting specific ones. Covers native host processes (e.g.
	// CasaOS's own gateway); Docker-published ports need the DOCKER-USER
	// rules below instead, since Docker bypasses this entirely.
	for _, source := range []string{cfg.WireGuard.Subnet, "127.0.0.1/32"} {
		rules = append(rules, rule{
			args:    []string{"allow", "from", source},
			comment: fmt.Sprintf("everything, any port — only from %s", source),
		})
	}

	return rules
}

// dockerUserComment tags every rule this package inserts into DOCKER-USER,
// so a re-apply can find and remove exactly its own rules — and nothing a
// user or another tool added — before inserting fresh ones.
const dockerUserComment = "nullwatch-vpn-only"

// dockerUserRuleSpecs returns the DOCKER-USER rule bodies (everything after
// "-I DOCKER-USER") in the final top-to-bottom evaluation order: WireGuard
// subnet first, then the tunnel port itself, then drop everything else
// arriving via the public interface. Container-to-container traffic on the
// Docker bridge never matches any of these (they're scoped to iface), so
// wiring between AdGuard/Traefik/WireGuard keeps working.
func dockerUserRuleSpecs(cfg *config.Config, iface string) [][]string {
	if cfg.WireGuard == nil || !cfg.WireGuard.Enabled || cfg.WireGuard.Subnet == "" {
		return nil
	}
	return [][]string{
		{"-i", iface, "-s", cfg.WireGuard.Subnet, "-m", "comment", "--comment", dockerUserComment, "-j", "RETURN"},
		{"-i", iface, "-p", "udp", "--dport", strconv.Itoa(cfg.WireGuard.Port), "-m", "comment", "--comment", dockerUserComment, "-j", "RETURN"},
		{"-i", iface, "-m", "comment", "--comment", dockerUserComment, "-j", "DROP"},
	}
}

// removeDockerUserRules deletes every previously-inserted nullwatch rule
// from DOCKER-USER, so re-applying (e.g. after the WireGuard subnet
// changes) doesn't leave stale duplicates behind.
func removeDockerUserRules() error {
	for {
		out, err := exec.Command("sudo", "iptables", "-L", "DOCKER-USER", "-n", "--line-numbers").CombinedOutput()
		if err != nil {
			return fmt.Errorf("list DOCKER-USER chain: %w\n%s", err, out)
		}

		lineNum := ""
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, dockerUserComment) {
				if fields := strings.Fields(line); len(fields) > 0 {
					lineNum = fields[0]
				}
				break
			}
		}
		if lineNum == "" {
			return nil
		}
		if err := run("iptables", "-D", "DOCKER-USER", lineNum); err != nil {
			return fmt.Errorf("remove existing DOCKER-USER rule %s: %w", lineNum, err)
		}
	}
}

func applyDockerUserRules(cfg *config.Config) error {
	iface, err := detectPublicInterface()
	if err != nil {
		return err
	}

	if err := removeDockerUserRules(); err != nil {
		return fmt.Errorf("clear old rules: %w", err)
	}

	specs := dockerUserRuleSpecs(cfg, iface)
	// -I prepends, so insert in reverse to land in the intended
	// top-to-bottom order.
	for i := len(specs) - 1; i >= 0; i-- {
		args := append([]string{"-I", "DOCKER-USER"}, specs[i]...)
		if err := run("iptables", args...); err != nil {
			return fmt.Errorf("iptables %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

// Apply shows the exact rules it's about to run and, on confirmation,
// applies both layers: ufw for native host processes, and Docker's
// DOCKER-USER chain for published container ports, which ufw can't reach.
func Apply(cfg *config.Config) error {
	if _, err := exec.LookPath("ufw"); err != nil {
		return fmt.Errorf("ufw not found — install it (e.g. `sudo apt install ufw`) and re-run")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		return fmt.Errorf("iptables not found — needed to lock down Docker's published ports, which ufw alone can't restrict")
	}

	iface, err := detectPublicInterface()
	if err != nil {
		return err
	}
	ufwRules := buildUfwRules(cfg)
	dockerSpecs := dockerUserRuleSpecs(cfg, iface)

	fmt.Println("The following rules will be applied:")
	for _, r := range ufwRules {
		fmt.Printf("  ufw %-40s # %s\n", strings.Join(r.args, " "), r.comment)
	}
	fmt.Println("  ufw default deny incoming")
	fmt.Println("  ufw default allow outgoing")
	fmt.Println("  ufw --force enable")
	fmt.Println("  (docker-published ports bypass ufw entirely, so also:)")
	for _, s := range dockerSpecs {
		fmt.Printf("  iptables -I DOCKER-USER %s\n", strings.Join(s, " "))
	}

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

	for _, r := range ufwRules {
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

	if err := applyDockerUserRules(cfg); err != nil {
		return fmt.Errorf("lock down docker-published ports: %w", err)
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
