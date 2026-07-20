// Package status computes the URLs for services nullwatch manages, for
// display in the menu and after a setup/reconfigure action. It's read-only
// — no network calls, just formatting from config.
package status

import (
	"fmt"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// Link is a named service URL.
type Link struct {
	Name string
	URL  string
}

// Links returns the URLs for every enabled, configured service. Empty until
// WireGuard's host has been set by a setup run — before that there's
// nothing meaningful to link to yet.
func Links(cfg *config.Config) []Link {
	if cfg.WireGuard == nil || cfg.WireGuard.Host == "" {
		return nil
	}
	host := cfg.WireGuard.Host

	var links []Link
	if cfg.AdGuard != nil && cfg.AdGuard.Enabled {
		links = append(links, Link{"AdGuard Home", fmt.Sprintf("http://%s:%d", host, cfg.AdGuard.HTTPPort)})
	}
	if cfg.WireGuard != nil && cfg.WireGuard.Enabled {
		links = append(links, Link{"WireGuard admin", fmt.Sprintf("http://%s:%d", host, cfg.WireGuard.WebUIPort)})
	}
	if cfg.Traefik != nil && cfg.Traefik.Enabled && cfg.Traefik.DashboardEnabled {
		links = append(links, Link{"Traefik dashboard", fmt.Sprintf("http://%s:%d", host, cfg.Traefik.DashboardPort)})
	}
	links = append(links, Link{"CasaOS", fmt.Sprintf("http://%s:81", host)})

	return links
}
