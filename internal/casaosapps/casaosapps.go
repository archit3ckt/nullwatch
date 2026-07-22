// Package casaosapps auto-generates Traefik routes for apps installed
// through CasaOS's App Store, so installing something gets it a friendly
// "<app>.<domain>" URL without any manual Traefik configuration — the same
// mechanism nullwatch already uses for CasaOS itself, just driven by
// CasaOS's own per-app metadata instead of hand-written.
//
// CasaOS stores each installed app's compose file at
// /var/lib/casaos/apps/<app>/docker-compose.yml, carrying a top-level
// "x-casaos" extension block CasaOS itself uses to build its own dashboard
// links: title, scheme, and port_map. Confirmed against a real installed
// app (qBittorrent) that port_map holds the actual resolved host-published
// port (e.g. "8181"), matching one of the service's own ports[].published
// entries — whose sibling "target" field is the container's internal port
// (8080 there).
//
// Routes point at the app's own container IP + internal port, not the
// host's published port: confirmed on a real deployment that Traefik (or
// any container on nullwatch-net) reaching a docker-proxy-published port
// via the host's bridge gateway address times out — a hairpin-NAT gap
// between bridges, not something fixable from here. Traefik instead also
// joins Docker's default "bridge" network directly (see
// traefik-compose.yml.tmpl), which is where CasaOS apps using the common
// `network_mode: bridge` convention land, giving it a direct L3 path to
// the app's container with no NAT involved.
package casaosapps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppsDir is where CasaOS stores each installed app's compose file.
const AppsDir = "/var/lib/casaos/apps"

// App is one CasaOS app with a discovered web UI, ready to route.
type App struct {
	Slug      string // derived from the app's title, used as both the Traefik router/service name and the hostname label
	TargetURL string // e.g. "http://172.17.0.5:8080" — the container's own bridge IP and internal port
}

type composeFile struct {
	Services map[string]serviceSpec `yaml:"services"`
	XCasaOS  *xCasaOS               `yaml:"x-casaos"`
}

type serviceSpec struct {
	Ports []portSpec `yaml:"ports"`
}

type portSpec struct {
	Target    flexString `yaml:"target"`
	Published flexString `yaml:"published"`
}

type xCasaOS struct {
	Title   flexString `yaml:"title"`
	Main    string     `yaml:"main"`
	Scheme  string     `yaml:"scheme"`
	PortMap string     `yaml:"port_map"`
}

// flexString handles two different YAML shapes with the same Go type:
//
//   - x-casaos.title, which CasaOS-AppStore apps write as either a plain
//     string or a map of language codes (e.g. {custom: "", en_US:
//     "qBittorrent"} — confirmed against a real installed app). en_US is
//     preferred; empty values (like an unset "custom" override) are
//     skipped even as a last-resort fallback, since a blank title is never
//     useful as a hostname.
//   - ports[].target/published, which compose allows as either a quoted
//     string ("8181") or a bare int (8080) depending on the app — read via
//     the YAML node's raw scalar value either way, sidestepping needing to
//     know which form a given app used.
type flexString string

func (f *flexString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*f = flexString(node.Value)
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}

	var m map[string]string
	if err := node.Decode(&m); err != nil {
		return err
	}
	for _, key := range []string{"en_US", "en_us", "custom"} {
		if v, ok := m[key]; ok && v != "" {
			*f = flexString(v)
			return nil
		}
	}
	for _, v := range m {
		if v != "" {
			*f = flexString(v)
			return nil
		}
	}
	return nil
}

// Scan reads every installed CasaOS app's compose file and returns the ones
// with a discoverable, currently-reachable web UI. Apps with no
// x-casaos.port_map (background services with nothing to browse to), or
// whose container isn't running / isn't on the default bridge network, are
// silently skipped rather than treated as errors — a partial result is more
// useful here than failing the whole scan over one app.
func Scan() ([]App, error) {
	entries, err := os.ReadDir(AppsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", AppsDir, err)
	}

	var apps []App
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		app, ok := scanOne(e.Name())
		if ok {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

// parsed is what's extractable from an app's compose file alone — split out
// from scanOne so the YAML-handling half is testable without a real CasaOS
// install or running Docker daemon.
type parsed struct {
	Title         string
	Scheme        string
	MainService   string
	ContainerPort string
}

// parseComposeFile extracts the x-casaos fields Scan needs, resolving
// port_map (the host-published port CasaOS's own dashboard uses) back to
// the matching container port via the main service's own ports list.
// Returns ok=false for anything that isn't a routable web app: no
// x-casaos.port_map at all, no service to attribute it to, or no ports
// entry whose "published" matches port_map (so the container port can't be
// determined).
func parseComposeFile(data []byte) (parsed, bool) {
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return parsed{}, false // malformed — skip rather than fail the whole scan
	}
	if cf.XCasaOS == nil || cf.XCasaOS.PortMap == "" {
		return parsed{}, false
	}

	mainService := cf.XCasaOS.Main
	if mainService == "" {
		for name := range cf.Services {
			mainService = name
			break
		}
	}
	svc, ok := cf.Services[mainService]
	if !ok {
		return parsed{}, false
	}

	var containerPort string
	for _, p := range svc.Ports {
		if string(p.Published) == cf.XCasaOS.PortMap {
			containerPort = string(p.Target)
			break
		}
	}
	if containerPort == "" {
		return parsed{}, false
	}

	scheme := cf.XCasaOS.Scheme
	if scheme == "" {
		scheme = "http"
	}

	return parsed{
		Title:         string(cf.XCasaOS.Title),
		Scheme:        scheme,
		MainService:   mainService,
		ContainerPort: containerPort,
	}, true
}

func scanOne(appDir string) (App, bool) {
	composePath := filepath.Join(AppsDir, appDir, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		return App{}, false
	}

	p, ok := parseComposeFile(data)
	if !ok {
		return App{}, false
	}

	containerIP, err := lookupContainerIP(appDir, p.MainService)
	if err != nil {
		return App{}, false // not running, or not on the default bridge network
	}

	title := p.Title
	if title == "" {
		title = appDir
	}

	return App{
		Slug:      slugify(title),
		TargetURL: fmt.Sprintf("%s://%s:%s", p.Scheme, containerIP, p.ContainerPort),
	}, true
}

// lookupContainerIP finds a running app's own IP address on Docker's
// default "bridge" network — matched via the standard "docker compose"
// labels (project working dir + service name) rather than assuming a
// project-naming convention, since that's derived from something already
// known for certain: the compose file's own path.
func lookupContainerIP(appDir, service string) (string, error) {
	workingDir := filepath.Join(AppsDir, appDir)
	out, err := exec.Command("docker", "ps",
		"--filter", "label=com.docker.compose.project.working_dir="+workingDir,
		"--filter", "label=com.docker.compose.service="+service,
		"--format", "{{.ID}}",
	).Output()
	if err != nil {
		return "", fmt.Errorf("docker ps: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("no running container for %s/%s", appDir, service)
	}

	ipOut, err := exec.Command("docker", "inspect", "-f",
		"{{.NetworkSettings.Networks.bridge.IPAddress}}", id,
	).Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w", err)
	}
	ip := strings.TrimSpace(string(ipOut))
	if ip == "" {
		return "", fmt.Errorf("%s/%s isn't on the default bridge network", appDir, service)
	}
	return ip, nil
}

// slugify turns an app title into a hostname-safe, lowercase label.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := true // avoid a leading dash
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}
