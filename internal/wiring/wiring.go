// Package wiring implements the cross-module automation described in the
// project spec: when adguard + traefik are both enabled, register a
// wildcard DNS rewrite for the configured domain; when adguard + wireguard
// are both enabled, push AdGuard as the DNS server to VPN clients. Every
// step is conditional on the modules actually being enabled.
package wiring

import (
	"fmt"
	"time"

	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/modules/adguard"
	"github.com/archit3ckt/nullwatch/internal/modules/traefik"
)

// PrepareConfig mutates cfg in place to apply wiring decisions that must be
// known before any compose file is rendered — currently just WireGuard's
// pushed DNS server. Call this before modules.WriteCompose.
func PrepareConfig(cfg *config.Config) {
	adguardOn := cfg.AdGuard != nil && cfg.AdGuard.Enabled
	if !adguardOn {
		return
	}

	if cfg.WireGuard != nil && cfg.WireGuard.Enabled {
		cfg.WireGuard.DNS = adguard.StaticIP
	}
}

// RegisterDNS applies wiring that requires AdGuard's API to be reachable —
// currently the wildcard rewrite pointing the configured domain at Traefik.
// Call this after AdGuard's container is confirmed up.
func RegisterDNS(cfg *config.Config) error {
	adguardOn := cfg.AdGuard != nil && cfg.AdGuard.Enabled
	traefikOn := cfg.Traefik != nil && cfg.Traefik.Enabled
	if !adguardOn || !traefikOn {
		return nil
	}
	if cfg.Global.Domain == "" {
		return fmt.Errorf("adguard + traefik are both enabled but global.domain is not set")
	}

	client := adguard.NewClient(cfg.AdGuard)
	if err := client.WaitReady(20 * time.Second); err != nil {
		return fmt.Errorf("adguard API not reachable: %w", err)
	}

	domain := cfg.Global.Domain
	wildcard := "*." + domain

	if err := client.EnsureRewrite(domain, traefik.StaticIP); err != nil {
		return fmt.Errorf("register rewrite for %s: %w", domain, err)
	}
	if err := client.EnsureRewrite(wildcard, traefik.StaticIP); err != nil {
		return fmt.Errorf("register rewrite for %s: %w", wildcard, err)
	}
	return nil
}
