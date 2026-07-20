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

// PreApply generates AdGuardHome.yaml (admin credentials, DNS settings,
// blocklists) so the container boots pre-configured instead of requiring
// AdGuard's interactive first-run setup wizard.
func (a *Adguard) PreApply(cfg *config.Config) error {
	if !a.Enabled(cfg) {
		return nil
	}

	dataDir, err := config.DataDir(a.Name())
	if err != nil {
		return err
	}
	confDir := filepath.Join(dataDir, "conf")
	workDir := filepath.Join(dataDir, "work")
	if err := os.MkdirAll(confDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return err
	}

	confPath := filepath.Join(confDir, "AdGuardHome.yaml")
	// Only preseed on first setup: once AdGuard has booted it owns this
	// file (persists filter state, stats, etc.) and we must not clobber it
	// on every re-run.
	if _, err := os.Stat(confPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	data, err := generatePreseed(cfg.AdGuard)
	if err != nil {
		return err
	}
	return os.WriteFile(confPath, data, 0o600)
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
