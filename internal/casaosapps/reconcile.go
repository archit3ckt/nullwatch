package casaosapps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/archit3ckt/nullwatch/internal/compose"
)

const dynamicTemplateName = "traefik-dynamic-app.yml.tmpl"

// routeFilePrefix namespaces the route files this package manages within
// Traefik's dynamic directory, so ReconcileTraefikRoutes can tell "a
// previous run's now-stale app route" apart from CasaOS's own hand-written
// casaos.yml or anything else placed in that directory, and only ever
// removes files it recognizes as its own.
const routeFilePrefix = "casaos-app-"

// ReconcileTraefikRoutes writes one Traefik dynamic route file per app,
// and removes route files (matching routeFilePrefix) for apps no longer
// present — so uninstalling an app through CasaOS also removes its route.
func ReconcileTraefikRoutes(domain, dynamicDir string, apps []App) error {
	want := map[string]bool{}
	for _, app := range apps {
		filename := routeFilePrefix + app.Slug + ".yml"
		want[filename] = true

		out, err := compose.Render(dynamicTemplateName, struct {
			Slug      string
			Domain    string
			TargetURL string
		}{app.Slug, domain, app.TargetURL})
		if err != nil {
			return fmt.Errorf("render route for %s: %w", app.Slug, err)
		}
		if err := os.WriteFile(filepath.Join(dynamicDir, filename), out, 0o600); err != nil {
			return fmt.Errorf("write route for %s: %w", app.Slug, err)
		}
	}

	entries, err := os.ReadDir(dynamicDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dynamicDir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, routeFilePrefix) || want[name] {
			continue
		}
		if err := os.Remove(filepath.Join(dynamicDir, name)); err != nil {
			return fmt.Errorf("remove stale route %s: %w", name, err)
		}
	}
	return nil
}
