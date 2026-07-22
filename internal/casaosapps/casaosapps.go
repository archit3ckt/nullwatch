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
// port (e.g. "8181"), not the container's internal port (8080 there) — so
// CasaOS itself already writes the real, conflict-resolved port back into
// this file, and reading it directly is both simpler and more reliable than
// cross-referencing a running container via `docker ps`/`docker port`,
// which an earlier version of this package did before being checked against
// a real app's file.
package casaosapps

import (
	"fmt"
	"os"
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
	TargetURL string // e.g. "http://172.30.0.1:8181"
}

type composeFile struct {
	XCasaOS *xCasaOS `yaml:"x-casaos"`
}

type xCasaOS struct {
	Title   flexString `yaml:"title"`
	Scheme  string     `yaml:"scheme"`
	PortMap string     `yaml:"port_map"`
}

// flexString handles x-casaos.title, which CasaOS-AppStore apps write as
// either a plain string or a map of language codes to strings (e.g.
// {custom: "", en_US: "qBittorrent"} — confirmed against a real installed
// app). en_US is preferred; empty values (like an unset "custom" override)
// are skipped even as a last-resort fallback, since a blank title is never
// useful as a hostname.
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
// with a discoverable web UI. Apps with no x-casaos.port_map (background
// services with nothing to browse to) are silently skipped rather than
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

// parsed is what's extractable from an app's compose file — split out from
// scanOne so the YAML-handling half is testable without a real CasaOS
// install on disk.
type parsed struct {
	Title   string
	Scheme  string
	PortMap string
}

// parseComposeFile extracts the x-casaos fields Scan needs. Returns
// ok=false for anything with no port_map — not a web app, or a background
// service with nothing to route to.
func parseComposeFile(data []byte) (parsed, bool) {
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return parsed{}, false // malformed — skip rather than fail the whole scan
	}
	if cf.XCasaOS == nil || cf.XCasaOS.PortMap == "" {
		return parsed{}, false
	}

	scheme := cf.XCasaOS.Scheme
	if scheme == "" {
		scheme = "http"
	}

	return parsed{
		Title:   string(cf.XCasaOS.Title),
		Scheme:  scheme,
		PortMap: cf.XCasaOS.PortMap,
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

	title := p.Title
	if title == "" {
		title = appDir
	}

	return App{
		Slug:      slugify(title),
		TargetURL: fmt.Sprintf("%s://%s:%s", p.Scheme, casaos.GatewayIP, p.PortMap),
	}, true
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
