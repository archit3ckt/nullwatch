// Package wireguard implements the Module contract for wg-easy, a
// full-tunnel WireGuard VPN server with a small web UI for managing peers.
package wireguard

import (
	"fmt"

	"github.com/archit3ckt/nullwatch/internal/compose"
	"github.com/archit3ckt/nullwatch/internal/config"
)

// StaticIP is wg-easy's fixed address on compose.NetworkName.
const StaticIP = "172.30.0.4"

const templateName = "wireguard-compose.yml.tmpl"

// image is pinned to a wg-easy major version whose env vars (PASSWORD,
// WG_DEFAULT_DNS, etc.) match templateData below. wg-easy has changed its
// env var names across major versions — re-check its docs before bumping.
const image = "ghcr.io/wg-easy/wg-easy:11"

type WireGuard struct{}

func New() *WireGuard { return &WireGuard{} }

func (w *WireGuard) Name() string { return "wireguard" }

func (w *WireGuard) Enabled(cfg *config.Config) bool {
	return cfg.WireGuard != nil && cfg.WireGuard.Enabled
}

func (w *WireGuard) StaticIP() string { return StaticIP }

// PreApply has nothing to preseed: wg-easy generates its own server keys
// and peer configs on first boot and persists them in its data volume.
func (w *WireGuard) PreApply(cfg *config.Config) error { return nil }

// PostApply has nothing to do after the container is up: wg-easy's web UI
// needs no first-run API setup.
func (w *WireGuard) PostApply(cfg *config.Config) error { return nil }

type templateData struct {
	Image       string
	Host        string
	Port        int
	WebUIPort   int
	Password    string
	DefaultAddr string
	DNS         string
	DataDir     string
	NetworkName string
	StaticIP    string
}

func (w *WireGuard) WriteCompose(cfg *config.Config) (string, error) {
	dataDir, err := config.DataDir(w.Name())
	if err != nil {
		return "", err
	}

	dns := cfg.WireGuard.DNS
	if dns == "" {
		dns = "1.1.1.1"
	}

	data := templateData{
		Image:       image,
		Host:        cfg.WireGuard.Host,
		Port:        cfg.WireGuard.Port,
		WebUIPort:   cfg.WireGuard.WebUIPort,
		Password:    cfg.WireGuard.WebUIPassword,
		DefaultAddr: subnetToDefaultAddress(cfg.WireGuard.Subnet),
		DNS:         dns,
		DataDir:     dataDir,
		NetworkName: compose.NetworkName,
		StaticIP:    StaticIP,
	}

	path, err := compose.Write(w.Name(), templateName, data)
	if err != nil {
		return "", fmt.Errorf("write wireguard compose: %w", err)
	}
	return path, nil
}

// subnetToDefaultAddress converts a CIDR like "10.8.0.0/24" into wg-easy's
// WG_DEFAULT_ADDRESS template form "10.8.0.x" (peers get .2, .3, ... — .1 is
// the server itself).
func subnetToDefaultAddress(subnet string) string {
	var a, b, c int
	if _, err := fmt.Sscanf(subnet, "%d.%d.%d.0/24", &a, &b, &c); err != nil {
		return "10.8.0.x" // fall back to the conventional default
	}
	return fmt.Sprintf("%d.%d.%d.x", a, b, c)
}
