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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/archit3ckt/nullwatch/internal/casaos"
	"github.com/archit3ckt/nullwatch/internal/compose"
	"github.com/archit3ckt/nullwatch/internal/config"
)

// StaticIP is Traefik's fixed address on compose.NetworkName. AdGuard's
// wildcard DNS rewrite for the configured domain points here.
const StaticIP = "172.30.0.3"

const templateName = "traefik-compose.yml.tmpl"
const casaosDynamicTemplateName = "traefik-dynamic-casaos.yml.tmpl"
const image = "traefik:v3.2"
const containerName = "nullwatch-traefik" // matches container_name in traefik-compose.yml.tmpl
const bridgeNetwork = "bridge"

type Traefik struct{}

func New() *Traefik { return &Traefik{} }

func (t *Traefik) Name() string { return "traefik" }

func (t *Traefik) Enabled(cfg *config.Config) bool {
	return cfg.Traefik != nil && cfg.Traefik.Enabled
}

func (t *Traefik) StaticIP() string { return StaticIP }

// PreApply ensures the dynamic file-provider directory exists and writes
// the CasaOS route into it — CasaOS is a native process, not a container
// nullwatch can label for Docker-provider auto-discovery, so its route has
// to be written by hand instead. Re-written every run, so a later domain
// change is picked up rather than leaving a route for the old hostname.
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

	if cfg.Global.Domain == "" {
		return nil
	}
	casaosRoute, err := compose.Render(casaosDynamicTemplateName, struct {
		Domain          string
		CasaOSGatewayIP string
		CasaOSPort      int
	}{cfg.Global.Domain, casaos.GatewayIP, casaos.Port})
	if err != nil {
		return fmt.Errorf("render casaos route: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dynamicDir, "casaos.yml"), casaosRoute, 0o600); err != nil {
		return fmt.Errorf("write casaos route: %w", err)
	}
	return nil
}

// PostApply connects the running container to Docker's default "bridge"
// network — deliberately not declared in the compose file itself, since
// compose always tries to register container_name as a network-scoped
// alias on every network a service joins, and the classic bridge network
// can't support aliases at all (confirmed on a real deployment: "invalid
// endpoint settings: network-scoped aliases are only supported for
// user-defined networks", which `docker compose up` failed outright on). A
// plain `docker network connect` afterward doesn't go through that alias
// logic. This is what gives Traefik a direct path to CasaOS apps on the
// default bridge network (see internal/casaosapps) without hairpin NAT
// through the host. Idempotent: connecting to a network the container is
// already on just errors "already exists", which is treated as success.
func (t *Traefik) PostApply(cfg *config.Config) error {
	if !t.Enabled(cfg) {
		return nil
	}
	out, err := exec.Command("docker", "network", "connect", bridgeNetwork, containerName).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "already exists") {
		return fmt.Errorf("connect %s to %s network: %w\n%s", containerName, bridgeNetwork, err, out)
	}
	return nil
}

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
