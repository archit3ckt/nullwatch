package casaosapps

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/archit3ckt/nullwatch/internal/casaos"
)

const watcherUnitPath = "/etc/systemd/system/nullwatch-casaos-watcher.service"
const watcherServiceName = "nullwatch-casaos-watcher.service"

// EnsureWatcherService installs and enables a systemd service that keeps
// Traefik's CasaOS-app routes in sync automatically — reacting to `docker
// events` rather than polling, so a newly-installed app gets a route within
// moments of its container starting, and an uninstalled one loses its route
// as soon as its container stops. No-ops (with a message, not an error) if
// CasaOS isn't actually installed, since there'd be nothing to watch.
func EnsureWatcherService() error {
	if !casaos.Installed() {
		fmt.Println("CasaOS isn't installed — skipping the app-route watcher.")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find nullwatch binary: %w", err)
	}

	// systemd services start with a minimal environment that doesn't
	// inherit HOME from anyone's shell — config.Load() (via os.UserHomeDir,
	// which on Linux just reads $HOME) fails outright without it. Baked in
	// at install time, captured from the environment this is actually
	// running in right now (interactively, from the menu), rather than
	// assumed to be /root.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	unit := fmt.Sprintf(`[Unit]
Description=nullwatch CasaOS app-route watcher
After=docker.service casaos.service
Requires=docker.service

[Service]
Type=simple
Environment=HOME=%s
ExecStart=%s --casaos-watch
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, home, exe)

	writeCmd := exec.Command("sudo", "tee", watcherUnitPath)
	writeCmd.Stdin = strings.NewReader(unit)
	if out, err := writeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write %s: %w\n%s", watcherUnitPath, err, out)
	}

	if out, err := exec.Command("sudo", "systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w\n%s", err, out)
	}
	if out, err := exec.Command("sudo", "systemctl", "enable", "--now", watcherServiceName).CombinedOutput(); err != nil {
		return fmt.Errorf("enable %s: %w\n%s", watcherServiceName, err, out)
	}
	return nil
}
