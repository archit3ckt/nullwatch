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

// ApplyOne re-applies a single module — used by the menu's per-module
// reconfigure actions, so editing e.g. WireGuard's params doesn't touch the
// other two containers. Still re-runs wiring (cheap and idempotent) since a
// domain or DNS change elsewhere can affect it.
func ApplyOne(desired *config.Config, name string) error {
	wiring.PrepareConfig(desired)

	if err := compose.EnsureNetwork(); err != nil {
		return fmt.Errorf("ensure docker network: %w", err)
	}

	m := modules.ByName(name)
	if m == nil {
		return fmt.Errorf("unknown module %q", name)
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

	if err := wiring.RegisterDNS(desired); err != nil {
		return fmt.Errorf("register DNS wiring: %w", err)
	}
	return nil
}

// Teardown stops and removes every module's containers and compose file,
// then the shared Docker network. It never touches CasaOS or
// ~/.nullwatch/config.yaml and data — callers decide separately whether to
// delete those.
func Teardown() error {
	for _, m := range modules.All() {
		exists, err := compose.Exists(m.Name())
		if err != nil {
			return fmt.Errorf("%s: check compose file: %w", m.Name(), err)
		}
		if !exists {
			continue
		}

		fmt.Printf("==> %s: tearing down\n", m.Name())
		if err := compose.Down(m.Name()); err != nil {
			return fmt.Errorf("%s: down: %w", m.Name(), err)
		}
		if err := compose.Remove(m.Name()); err != nil {
			return fmt.Errorf("%s: remove compose file: %w", m.Name(), err)
		}
	}

	if err := compose.RemoveNetwork(); err != nil {
		return fmt.Errorf("remove docker network: %w", err)
	}
	return nil
}
