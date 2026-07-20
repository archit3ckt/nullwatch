// Package wizard implements the interactive huh-based forms used by the
// menu: a full-stack setup pass and per-module reconfiguration, both
// pre-filled from existing config.yaml state. It only builds a desired
// config.Config in memory — applying it (compose files, docker, wiring) is
// the orchestrator's job.
package wizard

import (
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// RunFull walks the user through configuring the whole core stack —
// AdGuard, WireGuard, and Traefik are mandatory and always enabled; this
// tool exists specifically to provision them, unlike the optional apps
// CasaOS's app store already covers. Starting from current's state
// (pre-filled fields), it returns the desired config to apply. current is
// not mutated.
func RunFull(current *config.Config) (*config.Config, error) {
	pendingIntFields = nil
	desired := current.Clone()

	desired.AdGuard.Enabled = true
	desired.WireGuard.Enabled = true
	desired.Traefik.Enabled = true

	groups := []*huh.Group{
		domainGroup(desired),
		adguardGroup(desired.AdGuard),
		wireguardGroup(desired.WireGuard),
		traefikGroup(desired.Traefik),
	}

	if err := huh.NewForm(groups...).Run(); err != nil {
		return nil, fmt.Errorf("full setup: %w", err)
	}
	commitIntFields()

	return desired, nil
}

// RunModule walks the user through reconfiguring a single module's
// parameters (domain is included alongside traefik, since it's what the
// domain is for). current is not mutated.
func RunModule(current *config.Config, module string) (*config.Config, error) {
	pendingIntFields = nil
	desired := current.Clone()

	var groups []*huh.Group
	switch module {
	case "adguard":
		groups = []*huh.Group{adguardGroup(desired.AdGuard)}
	case "wireguard":
		groups = []*huh.Group{wireguardGroup(desired.WireGuard)}
	case "traefik":
		groups = []*huh.Group{domainGroup(desired), traefikGroup(desired.Traefik)}
	default:
		return nil, fmt.Errorf("unknown module %q", module)
	}

	if err := huh.NewForm(groups...).Run(); err != nil {
		return nil, fmt.Errorf("reconfigure %s: %w", module, err)
	}
	commitIntFields()

	return desired, nil
}
