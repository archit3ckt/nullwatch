// Package orchestrator reconciles a desired config.Config against what's
// currently running: it starts newly- or still-enabled modules (a no-op for
// unchanged ones, since `docker compose up -d` idempotently diffs itself),
// stops modules the user just disabled, and applies cross-module wiring.
package orchestrator

import (
	"fmt"

	"github.com/archit3ckt/nullwatch/internal/compose"
	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/modules"
	"github.com/archit3ckt/nullwatch/internal/wiring"
)

// Apply reconciles the running stack to match desired. previous is the
// last-applied config (nil on first run); it's only used to know which
// modules to tear down.
func Apply(previous, desired *config.Config) error {
	wiring.PrepareConfig(desired)

	if err := compose.EnsureNetwork(); err != nil {
		return fmt.Errorf("ensure docker network: %w", err)
	}

	diff := config.DiffEnabled(previous, desired)

	for _, m := range modules.All() {
		if !m.Enabled(desired) {
			continue
		}

		fmt.Printf("==> %s: applying\n", m.Name())

		if err := m.PreApply(desired); err != nil {
			return fmt.Errorf("%s: preapply: %w", m.Name(), err)
		}
		if _, err := m.WriteCompose(desired); err != nil {
			return fmt.Errorf("%s: write compose: %w", m.Name(), err)
		}
		if err := compose.Up(m.Name()); err != nil {
			return fmt.Errorf("%s: up: %w", m.Name(), err)
		}
	}

	for _, name := range diff.ToStop {
		fmt.Printf("==> %s: disabled, tearing down\n", name)
		if err := compose.Down(name); err != nil {
			return fmt.Errorf("%s: down: %w", name, err)
		}
		if err := compose.Remove(name); err != nil {
			return fmt.Errorf("%s: remove compose file: %w", name, err)
		}
	}

	if err := wiring.RegisterDNS(desired); err != nil {
		return fmt.Errorf("register DNS wiring: %w", err)
	}

	return nil
}
