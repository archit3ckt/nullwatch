// Package casaos installs CasaOS — the dashboard and app-store layer that
// sits on top of the infrastructure nullwatch provisions. nullwatch only
// installs it; it never manages CasaOS's own config, app store, or
// containers afterward, and CasaOS auto-detects nullwatch's containers on
// its own with no integration step needed.
package casaos

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/huh"
)

// installScriptURL is CasaOS's own official convenience script.
const installScriptURL = "https://get.casaos.io"

// GatewayIP is compose.NetworkName's Docker-assigned gateway address.
// CasaOS is a native process, not a container on that network, so it has no
// static IP of its own the way other modules do — but it listens on every
// interface the host has, including this one, which is reachable from VPN
// clients and other containers the same way any other module's static IP
// is. Not sourced from the compose package directly to avoid a needless
// import cycle risk (compose doesn't need to know about CasaOS).
const GatewayIP = "172.30.0.1"

// Port is CasaOS's default web UI port.
const Port = 81

// Installed reports whether CasaOS's systemd service is present, i.e. its
// installer has already run on this host.
func Installed() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "casaos").Run() == nil
}

// EnsureInstalled installs CasaOS if it isn't already present, with the
// user's confirmation (pre-accepted, since it's part of the default setup
// flow) before running anything as root. No-ops silently if CasaOS is
// already installed.
func EnsureInstalled() error {
	if Installed() {
		return nil
	}

	install := true
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Install CasaOS?").
			Description(fmt.Sprintf("CasaOS is the dashboard and app store (Nextcloud, Jellyfin, etc.) for this stack. Installs via the official %s script (requires sudo).", installScriptURL)).
			Affirmative("Install it").
			Negative("Skip").
			Value(&install),
	)).Run(); err != nil {
		return fmt.Errorf("casaos install prompt: %w", err)
	}
	if !install {
		return nil
	}

	fmt.Println("==> installing CasaOS (you may be prompted for your sudo password)")
	cmd := exec.Command("sh", "-c", "curl -fsSL "+installScriptURL+" | sudo bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("casaos install script failed: %w", err)
	}
	return nil
}

// Uninstall removes CasaOS and every app it manages, via the
// casaos-uninstall helper the official installer places on PATH — no
// network call needed, and it's CasaOS's own removal logic rather than
// nullwatch reimplementing it. No-ops if CasaOS isn't installed.
func Uninstall() error {
	if !Installed() {
		return nil
	}

	if _, err := exec.LookPath("casaos-uninstall"); err != nil {
		return fmt.Errorf("casaos-uninstall script not found on PATH — remove CasaOS manually, see https://casaos.io")
	}

	fmt.Println("==> uninstalling CasaOS (you may be prompted for your sudo password)")
	cmd := exec.Command("sudo", "casaos-uninstall")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("casaos-uninstall failed: %w", err)
	}
	return nil
}
