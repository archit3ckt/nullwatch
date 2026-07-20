// Package modules defines the common Module contract implemented by each
// infrastructure service (adguard, wireguard, traefik) and the registry the
// orchestrator and wizard iterate over.
package modules

import (
	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/modules/adguard"
	"github.com/archit3ckt/nullwatch/internal/modules/traefik"
	"github.com/archit3ckt/nullwatch/internal/modules/wireguard"
)

// Module is implemented by each infrastructure service package. Config
// mutation (PreApply) and compose rendering are split so the orchestrator
// can finish all cross-module wiring before any file hits disk.
type Module interface {
	// Name is the module's identifier: used as the compose project suffix,
	// the generated file name, and the config.yaml key.
	Name() string

	// Enabled reports whether this module is turned on in cfg.
	Enabled(cfg *config.Config) bool

	// StaticIP is this module's fixed address on compose.NetworkName.
	StaticIP() string

	// PreApply runs before compose files are rendered, for work like
	// preseeding a container's own config so it doesn't need an interactive
	// first-run wizard (e.g. AdGuard's AdGuardHome.yaml). No-op for modules
	// that don't need it.
	PreApply(cfg *config.Config) error

	// WriteCompose renders this module's docker-compose file to
	// ~/.nullwatch/compose/<name>.yml and returns the path written.
	WriteCompose(cfg *config.Config) (string, error)
}

// All returns every module in the fixed order they should be applied:
// AdGuard first (so its DNS API is up before wiring runs), then Traefik,
// then WireGuard.
func All() []Module {
	return []Module{
		adguard.New(),
		traefik.New(),
		wireguard.New(),
	}
}

// ByName returns the module with the given Name(), if any.
func ByName(name string) Module {
	for _, m := range All() {
		if m.Name() == name {
			return m
		}
	}
	return nil
}
