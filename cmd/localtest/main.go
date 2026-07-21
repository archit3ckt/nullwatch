// Command localtest exercises the real nullwatch code (the actual
// orchestrator, plus the AdGuard and wg-easy API clients) against
// actually-running local Docker containers, instead of guessing at API
// shapes and burning a round-trip to a real VPS per mistake. Kept as a
// permanent dev tool, not a throwaway — this is the fast local iteration
// loop for anything touching module bootstrap, wiring, or the module APIs.
//
// Usage:
//
//	go build -o /tmp/localtest ./cmd/localtest
//	HOME=/tmp/nullwatch-test /tmp/localtest
//
// Always override HOME to an isolated scratch directory — config.BaseDir()
// resolves under $HOME/.nullwatch, and this must never touch a real
// deployment's config/data. If HOME is overridden, first copy
// ~/.docker/cli-plugins into the scratch HOME too, or `docker compose`
// won't be found (Docker's own plugin discovery also keys off HOME).
//
// AdGuard's DNS port is deliberately non-standard (5300) here to avoid
// colliding with anything already on :53 on a dev machine — a real
// deployment uses the standard 53, so don't read anything into needing to
// specify a custom port when testing DNS resolution manually against this.
//
// For an even deeper check — an actual WireGuard tunnel, not just the API
// calls — after this passes, bring up a client container on the same
// nullwatch-net network using the printed peer config (set its Endpoint to
// WireGuard's static IP, 172.30.0.4:<port>, instead of the peer config's
// default host/public endpoint, since a container on the same Docker
// network reaches it directly):
//
//	docker run --rm -d --name wg-client-test --network nullwatch-net \
//	  --cap-add NET_ADMIN --cap-add SYS_MODULE \
//	  --sysctl net.ipv4.conf.all.src_valid_mark=1 \
//	  --device /dev/net/tun \
//	  -v /path/to/peer.conf:/config/wg_confs/wg0.conf:ro \
//	  linuxserver/wireguard:latest
//	docker exec wg-client-test wget -qO- --timeout=10 https://1.1.1.1/cdn-cgi/trace
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/modules/adguard"
	"github.com/archit3ckt/nullwatch/internal/modules/wireguard"
	"github.com/archit3ckt/nullwatch/internal/orchestrator"
)

func check(step string, err error) {
	if err != nil {
		fmt.Printf("FAIL [%s]: %v\n", step, err)
		os.Exit(1)
	}
	fmt.Printf("ok   [%s]\n", step)
}

func main() {
	cfg := config.Default()
	cfg.Global.Domain = "gateway.internal"
	cfg.AdGuard.Enabled = true
	cfg.AdGuard.HTTPPort = 3000
	cfg.AdGuard.DNSPort = 5300 // non-standard on purpose — see package doc
	cfg.AdGuard.AdminUser = "admin"
	cfg.AdGuard.AdminPassword = "test1234"
	cfg.WireGuard.Enabled = true
	cfg.WireGuard.Host = "127.0.0.1"
	cfg.WireGuard.Port = 51820
	cfg.WireGuard.Subnet = "10.9.0.0/24"
	cfg.WireGuard.WebUIPort = 51821
	cfg.WireGuard.WebUIPassword = "test1234"
	cfg.Traefik.Enabled = false // not needed for this pass

	// This is the exact function the real CLI calls for "Full setup" — no
	// manual re-implementation of its steps, so nothing here can drift from
	// what actually ships.
	check("orchestrator.Apply (adguard+wireguard, full sequence incl. wiring)", orchestrator.Apply(nil, cfg))

	// wg.DNS should now be AdGuard's static IP — wiring.PrepareConfig runs
	// as part of Apply above and mutates cfg in place.
	if cfg.WireGuard.DNS != adguard.StaticIP {
		fmt.Printf("FAIL [dns wiring]: WireGuard.DNS = %q, want %q (AdGuard's static IP)\n", cfg.WireGuard.DNS, adguard.StaticIP)
		os.Exit(1)
	}
	fmt.Printf("ok   [dns wiring] WireGuard.DNS = %s\n", cfg.WireGuard.DNS)

	// "Add WireGuard peer" is a separate menu action, not part of Apply.
	wgClient := wireguard.NewClient(cfg.WireGuard)
	id, peerConf, err := wgClient.CreatePeer("test-phone")
	check("wireguard create peer", err)
	fmt.Printf("     peer id=%s conf=%d bytes\n", id, len(peerConf))
	if !strings.Contains(peerConf, "DNS = "+adguard.StaticIP) {
		fmt.Printf("FAIL [peer config dns]: expected \"DNS = %s\" in peer config, got:\n%s\n", adguard.StaticIP, peerConf)
		os.Exit(1)
	}
	fmt.Println("--- peer config ---")
	fmt.Println(peerConf)
	fmt.Println("--- end peer config ---")

	fmt.Println("\nALL CHECKS PASSED")
}
