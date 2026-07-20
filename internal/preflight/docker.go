// Package preflight checks that the runtime dependencies nullwatch shells
// out to (docker, docker compose, a reachable daemon) are present, and — with
// explicit user consent — installs what's missing rather than failing deep
// inside a `docker compose up` call with a confusing error.
package preflight

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/charmbracelet/huh"
)

// Ensure verifies docker + the compose plugin are installed and the daemon
// is reachable, offering to install/fix what's missing. Returns an error
// only if a hard requirement is unmet and the user declined (or the fix
// itself failed) — nullwatch can't do anything useful without Docker.
func Ensure() error {
	if !commandExists("docker") {
		if err := offerDockerInstall(); err != nil {
			return err
		}
	}

	if !commandExists("docker") {
		return fmt.Errorf("docker still not found after install attempt — install it manually and re-run nullwatch")
	}

	if !composeAvailable() {
		if err := offerComposePluginInstall(); err != nil {
			return err
		}
	}

	if !daemonReachable() {
		if err := offerDaemonFix(); err != nil {
			return err
		}
	}

	return nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func composeAvailable() bool {
	return exec.Command("docker", "compose", "version").Run() == nil
}

func daemonReachable() bool {
	return exec.Command("docker", "info").Run() == nil
}

// dockerInstallScriptURL is Docker's own official convenience script. It
// detects the host distro and installs the right packages; run it directly
// rather than reimplementing per-distro package management here.
const dockerInstallScriptURL = "https://get.docker.com"

func offerDockerInstall() error {
	install := false
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Docker was not found on this system.").
			Description(fmt.Sprintf("Install it now by running the official convenience script from %s? This runs as root (via sudo) and modifies your package manager's sources.", dockerInstallScriptURL)).
			Affirmative("Install Docker").
			Negative("Skip").
			Value(&install),
	)).Run()
	if err != nil {
		return fmt.Errorf("docker install prompt: %w", err)
	}
	if !install {
		return fmt.Errorf("docker is required and was not installed — install it manually (see %s) and re-run nullwatch", dockerInstallScriptURL)
	}

	fmt.Println("==> running the Docker install script (you may be prompted for your sudo password)")
	cmd := exec.Command("sh", "-c", "curl -fsSL "+dockerInstallScriptURL+" | sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker install script failed: %w", err)
	}

	return offerAddToDockerGroup()
}

// offerAddToDockerGroup adds the current user to the docker group so they
// don't need sudo for every docker command. Purely a convenience step: if
// declined or it fails, nullwatch itself still works as long as it's run
// with sufficient privilege to reach the daemon.
func offerAddToDockerGroup() error {
	u, err := user.Current()
	if err != nil || u.Username == "root" {
		return nil
	}

	add := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Add %s to the docker group?", u.Username)).
			Description("Lets you (and nullwatch) run docker without sudo. Requires logging out and back in to take effect.").
			Affirmative("Add me").
			Negative("Skip").
			Value(&add),
	)).Run(); err != nil {
		return nil // non-fatal, don't block on a convenience step
	}
	if !add {
		return nil
	}

	cmd := exec.Command("sudo", "usermod", "-aG", "docker", u.Username)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not add %s to the docker group: %v\n", u.Username, err)
		return nil
	}

	fmt.Printf("Added %s to the docker group — log out and back in for it to take effect. Continuing this run with sudo where needed.\n", u.Username)
	return nil
}

// unameArch maps Go's GOARCH to the arch suffix docker/compose release
// assets use (uname -m style), since that's what the download URL expects.
func unameArch() (string, bool) {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", true
	case "arm64":
		return "aarch64", true
	case "arm":
		return "armv7", true
	case "s390x":
		return "s390x", true
	case "ppc64le":
		return "ppc64le", true
	default:
		return "", false
	}
}

// offerComposePluginInstall installs the compose CLI plugin for the current
// user only (~/.docker/cli-plugins), leaving the rest of the existing
// Docker install untouched — no sudo, no package manager involved.
func offerComposePluginInstall() error {
	install := false
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Docker is installed, but the compose plugin isn't.").
			Description("Install just the compose plugin for your user (~/.docker/cli-plugins), without touching the rest of your Docker install?").
			Affirmative("Install it").
			Negative("Skip").
			Value(&install),
	)).Run()
	if err != nil {
		return fmt.Errorf("compose plugin prompt: %w", err)
	}
	if !install {
		return fmt.Errorf("the docker compose plugin is required — install it (see https://docs.docker.com/compose/install/linux/) and re-run nullwatch")
	}

	arch, ok := unameArch()
	if !ok {
		return fmt.Errorf("unsupported architecture %s for the compose plugin — install it manually: https://docs.docker.com/compose/install/linux/", runtime.GOARCH)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	pluginDir := filepath.Join(home, ".docker", "cli-plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", pluginDir, err)
	}

	url := fmt.Sprintf("https://github.com/docker/compose/releases/latest/download/docker-compose-linux-%s", arch)
	fmt.Printf("==> downloading compose plugin from %s\n", url)
	if err := downloadFile(url, filepath.Join(pluginDir, "docker-compose"), 0o755); err != nil {
		return fmt.Errorf("download compose plugin: %w", err)
	}

	if !composeAvailable() {
		return fmt.Errorf("installed the compose plugin to %s but `docker compose version` still fails — check the download and re-run nullwatch", pluginDir)
	}
	return nil
}

func downloadFile(url, dest string, perm os.FileMode) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func offerDaemonFix() error {
	fix := false
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Docker is installed, but the daemon isn't reachable.").
			Description("Try starting it now with `sudo systemctl enable --now docker`?").
			Affirmative("Start it").
			Negative("Skip").
			Value(&fix),
	)).Run()
	if err != nil {
		return fmt.Errorf("docker daemon prompt: %w", err)
	}
	if !fix {
		return fmt.Errorf("docker daemon is not reachable — start it (or check permissions) and re-run nullwatch")
	}

	cmd := exec.Command("sudo", "systemctl", "enable", "--now", "docker")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("starting docker failed: %w", err)
	}

	if !daemonReachable() {
		return fmt.Errorf("docker daemon still not reachable after starting it — check `docker info` and your user's permissions")
	}
	return nil
}
