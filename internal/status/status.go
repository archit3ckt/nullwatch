// Package status computes the URLs for services nullwatch manages, for
// display in the menu and after a setup/reconfigure action. It's read-only
// — no network calls, just formatting from config.
package status

import (
	"fmt"

	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/modules/adguard"
	"github.com/archit3ckt/nullwatch/internal/modules/traefik"
	"github.com/archit3ckt/nullwatch/internal/modules/wireguard"
)

// Link is a named service URL.
type Link struct {
	Name string
	URL  string
}

// Links returns the URLs for every enabled, configured service. Empty until
// WireGuard's host has been set by a setup run — before that there's
// nothing meaningful to link to yet.
//
// AdGuard, Traefik, and WireGuard's admin UI are addressed by their static
// IP on nullwatch-net (172.30.0.x), not the public domain/IP. A VPN client
// reaching those addresses is normal routing to a private subnet — no
// different from reaching any other machine on a LAN. Reaching the public
// domain/IP instead would mean the client's own traffic, after already
// entering through the tunnel, has to loop back out through the server's
// public interface and back in ("hairpin NAT") — something that isn't
// guaranteed to work without explicit configuration this stack doesn't set
// up, since it's not needed for anything else here.
//
// CasaOS is a native process, not a container on nullwatch-net, so it has
// no static IP of its own — but it does listen on all of the host's
// interfaces, which includes nullwatch-net's own Docker-assigned bridge
// gateway address. That address is reachable the same way the other
// services' static IPs are (ordinary routing to a private subnet, no
// hairpin NAT), so it's used here instead of the public domain.
const casaosGatewayIP = "172.30.0.1"
func Links(cfg *config.Config) []Link {
	if cfg.WireGuard == nil || cfg.WireGuard.Host == "" {
		return nil
	}

	var links []Link
	if cfg.AdGuard != nil && cfg.AdGuard.Enabled {
		links = append(links, Link{"AdGuard Home", fmt.Sprintf("http://%s:%d", adguard.StaticIP, cfg.AdGuard.HTTPPort)})
	}
	if cfg.WireGuard != nil && cfg.WireGuard.Enabled {
		links = append(links, Link{"WireGuard admin", fmt.Sprintf("http://%s:%d", wireguard.StaticIP, cfg.WireGuard.WebUIPort)})
	}
	if cfg.Traefik != nil && cfg.Traefik.Enabled && cfg.Traefik.DashboardEnabled {
		links = append(links, Link{"Traefik dashboard", fmt.Sprintf("http://%s:%d", traefik.StaticIP, cfg.Traefik.DashboardPort)})
	}
	links = append(links, Link{"CasaOS", fmt.Sprintf("http://%s:81", casaosGatewayIP)})

	return links
}
