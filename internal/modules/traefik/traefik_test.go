package traefik_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/modules/traefik"
)

func TestPreApplyWritesCasaOSRoute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Default()
	cfg.Global.Domain = "home.arpa"
	cfg.Traefik.Enabled = true

	if err := traefik.New().PreApply(cfg); err != nil {
		t.Fatalf("PreApply: %v", err)
	}

	dataDir, err := config.DataDir("traefik")
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "dynamic", "casaos.yml"))
	if err != nil {
		t.Fatalf("read generated route: %v", err)
	}

	for _, want := range []string{
		"Host(`casaos.home.arpa`)",
		"Host(`home.arpa`)",
		"replacement: \"https://casaos.home.arpa${1}\"",
		"- websecure",
		"tls: {}",
		"http://172.30.0.1:81",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("generated route missing %q, got:\n%s", want, data)
		}
	}
}

func TestPreApplySkipsCasaOSRouteWithoutDomain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Default()
	cfg.Traefik.Enabled = true

	if err := traefik.New().PreApply(cfg); err != nil {
		t.Fatalf("PreApply: %v", err)
	}

	dataDir, err := config.DataDir("traefik")
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "dynamic", "casaos.yml")); !os.IsNotExist(err) {
		t.Errorf("expected no casaos.yml without a configured domain, stat err = %v", err)
	}
}
