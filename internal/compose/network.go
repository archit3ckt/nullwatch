package compose

import (
	"fmt"
	"os/exec"
	"strings"
)

// NetworkName is the shared Docker network all nullwatch-managed containers
// join. Modules are assigned static IPs on this network (see each module's
// StaticIP constant) so cross-module wiring — e.g. AdGuard DNS rewrites
// pointing at Traefik — doesn't depend on Docker's embedded DNS or on
// inspecting containers at runtime.
const NetworkName = "nullwatch-net"

// NetworkSubnet is the fixed subnet backing NetworkName.
const NetworkSubnet = "172.30.0.0/24"

// EnsureNetwork creates the shared Docker network if it doesn't already
// exist. Safe to call on every run.
func EnsureNetwork() error {
	check := exec.Command("docker", "network", "inspect", NetworkName)
	if out, err := check.CombinedOutput(); err == nil {
		return nil // already exists
	} else if !strings.Contains(string(out), "No such network") {
		// inspect failed for some other reason than "doesn't exist" — still
		// fall through and let `network create` surface the real error.
		_ = err
	}

	create := exec.Command("docker", "network", "create",
		"--subnet", NetworkSubnet,
		NetworkName,
	)
	if out, err := create.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("docker network create %s: %w\n%s", NetworkName, err, out)
	}
	return nil
}

// RemoveNetwork deletes the shared Docker network. Safe to call even if it
// doesn't exist (e.g. nothing was ever brought up).
func RemoveNetwork() error {
	out, err := exec.Command("docker", "network", "rm", NetworkName).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "not found") {
		return fmt.Errorf("docker network rm %s: %w\n%s", NetworkName, err, out)
	}
	return nil
}
