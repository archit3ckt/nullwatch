package casaosapps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileTraefikRoutesWritesAndRemoves(t *testing.T) {
	dir := t.TempDir()

	// A route that isn't ours — must survive reconcile untouched.
	handWritten := filepath.Join(dir, "casaos.yml")
	if err := os.WriteFile(handWritten, []byte("hand-written"), 0o600); err != nil {
		t.Fatal(err)
	}

	apps := []App{
		{Slug: "jellyfin", TargetURL: "http://172.30.0.1:8097"},
		{Slug: "n8n", TargetURL: "http://172.30.0.1:5678"},
	}
	if err := ReconcileTraefikRoutes("home.arpa", dir, apps); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, app := range apps {
		path := filepath.Join(dir, routeFilePrefix+app.Slug+".yml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), "Host(`"+app.Slug+".home.arpa`)") {
			t.Errorf("%s missing expected Host rule, got:\n%s", path, data)
		}
		if !strings.Contains(string(data), app.TargetURL) {
			t.Errorf("%s missing target URL %s", path, app.TargetURL)
		}
	}

	if _, err := os.ReadFile(handWritten); err != nil {
		t.Errorf("hand-written route file should survive reconcile: %v", err)
	}

	// Now drop jellyfin — its route file should be removed, n8n's kept.
	if err := ReconcileTraefikRoutes("home.arpa", dir, apps[1:]); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, routeFilePrefix+"jellyfin.yml")); !os.IsNotExist(err) {
		t.Errorf("expected jellyfin route to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, routeFilePrefix+"n8n.yml")); err != nil {
		t.Errorf("expected n8n route to remain: %v", err)
	}
	if _, err := os.ReadFile(handWritten); err != nil {
		t.Errorf("hand-written route file should still survive: %v", err)
	}
}
