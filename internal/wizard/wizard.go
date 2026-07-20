// Package wizard implements the interactive huh-based flow used on every
// run: a parameter group per core module, pre-filled from existing
// config.yaml state. It only builds a desired config.Config in memory —
// applying it (compose files, docker, wiring) is the orchestrator's job.
package wizard

import (
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// Run walks the user through configuring the core infrastructure stack —
// AdGuard, WireGuard, and Traefik are mandatory and always enabled; this
// tool exists specifically to provision them, unlike the optional apps
// CasaOS's app store already covers. Starting from current's state
// (pre-filled fields), it returns the desired config to apply. current is
// not mutated.
func Run(current *config.Config) (*config.Config, error) {
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
		return nil, fmt.Errorf("module configuration: %w", err)
	}
	commitIntFields()

	return desired, nil
}
