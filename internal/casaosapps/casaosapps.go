// Package casaosapps auto-generates Traefik routes for apps installed
// through CasaOS's App Store, so installing something gets it a friendly
// "<app>.<domain>" URL without any manual Traefik configuration — the same
// mechanism nullwatch already uses for CasaOS itself, just driven by
// CasaOS's own per-app metadata instead of hand-written.
//
// CasaOS stores each installed app's compose file at
// /var/lib/casaos/apps/<app>/docker-compose.yml, carrying an "x-casaos"
// extension block CasaOS itself uses to build its own dashboard links
// (title, and a webui.port_map naming the container's web port). Reading
// that instead of guessing a hostname from the container name, or a port
// from `docker ps` output, reuses data CasaOS already curated rather than
// re-deriving it heuristically.
//
// The actual host-published port is looked up from the running container
// rather than trusted from the compose file's text: CasaOS resolves a
// ${WEBUI_PORT}-style placeholder to whatever free port it picked at
// install time to dodge conflicts, so the file alone doesn't reliably say
// what's actually bound.
package casaosapps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/archit3ckt/nullwatch/internal/casaos"
)

// AppsDir is where CasaOS stores each installed app's compose file.
const AppsDir = "/var/lib/casaos/apps"

// App is one CasaOS app with a discovered web UI, ready to route.
type App struct {
	Slug      string // derived from the app's title, used as both the Traefik router/service name and the hostname label
	TargetURL string // e.g. "http://172.30.0.1:38384"
}

type composeFile struct {
	Services map[string]any `yaml:"services"`
	XCasaOS  *xCasaOS       `yaml:"x-casaos"`
}

type xCasaOS struct {
	Title flexString `yaml:"title"`
	Main  string     `yaml:"main"`
	WebUI *webUI     `yaml:"webui"`
}

type webUI struct {
	Scheme  string `yaml:"scheme"`
	PortMap string `yaml:"port_map"`
}

// flexString handles x-casaos.title, which CasaOS-AppStore apps write as
// either a plain string or a map of language codes to strings (e.g.
// {en_us: "...", zh_cn: "..."}).
type flexString string

func (f *flexString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*f = flexString(node.Value)
		return nil
	}
	if node.Kind == yaml.MappingNode {
		var m map[string]string
		if err := node.Decode(&m); err != nil {
			return err
		}
		if v, ok := m["en_us"]; ok {
			*f = flexString(v)
			return nil
		}
		for _, v := range m {
			*f = flexString(v)
			return nil
		}
	}
	return nil
}

// Scan reads every installed CasaOS app's compose file and returns the ones
// with a discoverable, currently-running web UI. Apps with no x-casaos
// webui block (background services with nothing to browse to), or whose
// container isn't currently running, are silently skipped rather than
// treated as errors — a partial result is more useful here than failing the
// whole scan over one app.
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

// parsed is what's extractable from an app's compose file alone, before any
// live Docker lookup — split out from scanOne so the YAML-handling half is
// testable without a running CasaOS/Docker environment.
type parsed struct {
	Title         string
	MainService   string
	ContainerPort string
	Scheme        string
}

// parseComposeFile extracts the x-casaos fields Scan needs. Returns ok=false
// for anything that isn't a routable web app: no x-casaos.webui block at
// all (background services with nothing to browse to), or no service to
// attribute the port to.
func parseComposeFile(data []byte) (parsed, bool) {
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return parsed{}, false // malformed — skip rather than fail the whole scan
	}
	if cf.XCasaOS == nil || cf.XCasaOS.WebUI == nil || cf.XCasaOS.WebUI.PortMap == "" {
		return parsed{}, false
	}

	mainService := cf.XCasaOS.Main
	if mainService == "" {
		for name := range cf.Services {
			mainService = name
			break
		}
	}
	if mainService == "" {
		return parsed{}, false
	}

	scheme := cf.XCasaOS.WebUI.Scheme
	if scheme == "" {
		scheme = "http"
	}

	return parsed{
		Title:         string(cf.XCasaOS.Title),
		MainService:   mainService,
		ContainerPort: cf.XCasaOS.WebUI.PortMap,
		Scheme:        scheme,
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

	hostPort, err := lookupHostPort(appDir, p.MainService, p.ContainerPort)
	if err != nil {
		return App{}, false // container not running — nothing to route to yet
	}

	title := p.Title
	if title == "" {
		title = appDir
	}

	return App{
		Slug:      slugify(title),
		TargetURL: fmt.Sprintf("%s://%s:%s", p.Scheme, casaos.GatewayIP, hostPort),
	}, true
}

// lookupHostPort finds the host port CasaOS actually bound a running app's
// web UI container port to. Matched via the standard "docker compose"
// labels (project working dir + service name) rather than assuming a
// project-naming convention, since that's derived from something already
// known for certain — the compose file's own path.
func lookupHostPort(appDir, service, containerPort string) (string, error) {
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

	portOut, err := exec.Command("docker", "port", id, containerPort+"/tcp").Output()
	if err != nil {
		return "", fmt.Errorf("docker port: %w", err)
	}
	line := strings.TrimSpace(strings.SplitN(string(portOut), "\n", 2)[0])
	idx := strings.LastIndex(line, ":")
	if idx == -1 {
		return "", fmt.Errorf("unexpected docker port output: %q", line)
	}
	return line[idx+1:], nil
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
