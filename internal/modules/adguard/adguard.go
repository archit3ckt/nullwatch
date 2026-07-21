// Package adguard implements the Module contract for AdGuard Home, the
// stack's DNS resolver and tracker/ad blocklist layer.
package adguard

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/archit3ckt/nullwatch/internal/compose"
	"github.com/archit3ckt/nullwatch/internal/config"
)

// StaticIP is AdGuard's fixed address on compose.NetworkName, used by other
// modules (WireGuard's pushed DNS, Traefik's rewrite target lookups) to
// reach it without depending on Docker's embedded DNS.
const StaticIP = "172.30.0.2"

const templateName = "adguard-compose.yml.tmpl"

type Adguard struct{}

func New() *Adguard { return &Adguard{} }

func (a *Adguard) Name() string { return "adguard" }

func (a *Adguard) Enabled(cfg *config.Config) bool {
	return cfg.AdGuard != nil && cfg.AdGuard.Enabled
}

func (a *Adguard) StaticIP() string { return StaticIP }

// PreApply ensures the bind-mount directories exist before the container
// starts. AdGuard owns everything under them once it boots.
func (a *Adguard) PreApply(cfg *config.Config) error {
	if !a.Enabled(cfg) {
		return nil
	}

	dataDir, err := config.DataDir(a.Name())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "conf"), 0o700); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(dataDir, "work"), 0o700)
}

// PostApply completes AdGuard's first-run setup via its own API, once the
// container is up and reachable. See Client.Bootstrap. Deliberately doesn't
// register blocklists here — callers should call RegisterBlocklists
// separately, and last, after anything else that depends on AdGuard's API
// (like DNS rewiring) has already happened.
func (a *Adguard) PostApply(cfg *config.Config) error {
	if !a.Enabled(cfg) {
		return nil
	}
	client := NewClient(cfg.AdGuard)
	return client.Bootstrap(cfg.AdGuard.HTTPPort, cfg.AdGuard.DNSPort)
}

// RegisterBlocklists registers the configured blocklist URLs via AdGuard's
// API. Split out from PostApply and meant to be called last in the overall
// apply sequence: a slow or stuck fetch here can tie up AdGuard's whole API,
// so anything that depends on it responding quickly (DNS rewiring) needs to
// happen first. No-ops if AdGuard isn't enabled.
func RegisterBlocklists(cfg *config.Config) error {
	if cfg.AdGuard == nil || !cfg.AdGuard.Enabled {
		return nil
	}
	client := NewClient(cfg.AdGuard)
	return client.EnsureFilters(cfg.AdGuard.Blocklists)
}

type templateData struct {
	Image       string
	HTTPPort    int
	DNSPort     int
	ConfDir     string
	WorkDir     string
	NetworkName string
	StaticIP    string
}

func (a *Adguard) WriteCompose(cfg *config.Config) (string, error) {
	dataDir, err := config.DataDir(a.Name())
	if err != nil {
		return "", err
	}

	data := templateData{
		Image:       "adguard/adguardhome:v0.107.52",
		HTTPPort:    cfg.AdGuard.HTTPPort,
		DNSPort:     cfg.AdGuard.DNSPort,
		ConfDir:     filepath.Join(dataDir, "conf"),
		WorkDir:     filepath.Join(dataDir, "work"),
		NetworkName: compose.NetworkName,
		StaticIP:    StaticIP,
	}

	path, err := compose.Write(a.Name(), templateName, data)
	if err != nil {
		return "", fmt.Errorf("write adguard compose: %w", err)
	}
	return path, nil
}
