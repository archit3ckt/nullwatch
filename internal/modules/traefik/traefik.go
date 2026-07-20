// Package traefik implements the Module contract for Traefik, the reverse
// proxy routing *.Domain to backend containers via Docker label discovery,
// with a file provider directory for static/dynamic routes too.
//
// Everything this stack runs is meant to be VPN-only (see internal/firewall)
// rather than publicly reachable, so there's no way to complete Let's
// Encrypt's HTTP-01 challenge — port 80 is never reachable from Let's
// Encrypt's validation servers. Traefik falls back to its own self-signed
// certificate for TLS instead; browsers will warn once until you trust it,
// but no ACME/third party is involved.
package traefik

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/archit3ckt/nullwatch/internal/compose"
	"github.com/archit3ckt/nullwatch/internal/config"
)

// StaticIP is Traefik's fixed address on compose.NetworkName. AdGuard's
// wildcard DNS rewrite for the configured domain points here.
const StaticIP = "172.30.0.3"

const templateName = "traefik-compose.yml.tmpl"
const image = "traefik:v3.2"

type Traefik struct{}

func New() *Traefik { return &Traefik{} }

func (t *Traefik) Name() string { return "traefik" }

func (t *Traefik) Enabled(cfg *config.Config) bool {
	return cfg.Traefik != nil && cfg.Traefik.Enabled
}

func (t *Traefik) StaticIP() string { return StaticIP }

// PreApply ensures the dynamic file-provider directory exists.
func (t *Traefik) PreApply(cfg *config.Config) error {
	if !t.Enabled(cfg) {
		return nil
	}

	dataDir, err := config.DataDir(t.Name())
	if err != nil {
		return err
	}
	dynamicDir := filepath.Join(dataDir, "dynamic")
	return os.MkdirAll(dynamicDir, 0o700)
}

// PostApply has nothing to do after the container is up: Traefik needs no
// first-run API setup.
func (t *Traefik) PostApply(cfg *config.Config) error { return nil }

type templateData struct {
	Image            string
	HTTPPort         int
	HTTPSPort        int
	DashboardEnabled bool
	DashboardPort    int
	DynamicDir       string
	NetworkName      string
	StaticIP         string
}

func (t *Traefik) WriteCompose(cfg *config.Config) (string, error) {
	dataDir, err := config.DataDir(t.Name())
	if err != nil {
		return "", err
	}

	data := templateData{
		Image:            image,
		HTTPPort:         cfg.Traefik.HTTPPort,
		HTTPSPort:        cfg.Traefik.HTTPSPort,
		DashboardEnabled: cfg.Traefik.DashboardEnabled,
		DashboardPort:    cfg.Traefik.DashboardPort,
		DynamicDir:       filepath.Join(dataDir, "dynamic"),
		NetworkName:      compose.NetworkName,
		StaticIP:         StaticIP,
	}

	path, err := compose.Write(t.Name(), templateName, data)
	if err != nil {
		return "", fmt.Errorf("write traefik compose: %w", err)
	}
	return path, nil
}
