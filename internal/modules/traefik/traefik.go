// Package traefik implements the Module contract for Traefik, the reverse
// proxy routing *.Domain to backend containers via Docker label discovery,
// with a file provider directory for static/dynamic routes too.
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

// PreApply ensures the dynamic file-provider directory and acme.json exist.
// acme.json must be 0600 or Traefik refuses to use it for ACME storage.
func (t *Traefik) PreApply(cfg *config.Config) error {
	if !t.Enabled(cfg) {
		return nil
	}

	dataDir, err := config.DataDir(t.Name())
	if err != nil {
		return err
	}
	dynamicDir := filepath.Join(dataDir, "dynamic")
	if err := os.MkdirAll(dynamicDir, 0o700); err != nil {
		return err
	}

	acmePath := filepath.Join(dataDir, "acme.json")
	if _, err := os.Stat(acmePath); os.IsNotExist(err) {
		if err := os.WriteFile(acmePath, []byte("{}"), 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return os.Chmod(acmePath, 0o600)
}

type templateData struct {
	Image            string
	HTTPPort         int
	HTTPSPort        int
	DashboardEnabled bool
	DashboardPort    int
	ACMEEmail        string
	DataDir          string
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
		ACMEEmail:        cfg.Traefik.ACMEEmail,
		DataDir:          dataDir,
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
