package casaosapps

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Stable Diffusion":  "stable-diffusion",
		"N8n":               "n8n",
		"  Jellyfin!! ":     "jellyfin",
		"Home Assistant OS": "home-assistant-os",
		"already-slug":      "already-slug",
		"":                  "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseComposeFileRealQBittorrentSchema uses the actual on-disk
// x-casaos shape from a real CasaOS install (trimmed to the relevant
// fields): port_map ("8181") is the host-published port, resolved back to
// the container's internal port (8080) via the matching ports[] entry —
// confirmed necessary since routing needs the container port, not the host
// one, to reach the app directly by its own container IP.
func TestParseComposeFileRealQBittorrentSchema(t *testing.T) {
	data := []byte(`
services:
  qbittorrent:
    image: ghcr.io/hotio/qbittorrent:release-5.0.4
    ports:
      - target: 8080
        published: "8181"
        protocol: tcp

x-casaos:
  main: qbittorrent
  port_map: "8181"
  scheme: http
  title:
    custom: ""
    en_US: qBittorrent
`)
	p, ok := parseComposeFile(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.Title != "qBittorrent" {
		t.Errorf("Title = %q, want %q", p.Title, "qBittorrent")
	}
	if p.MainService != "qbittorrent" {
		t.Errorf("MainService = %q, want %q", p.MainService, "qbittorrent")
	}
	if p.ContainerPort != "8080" {
		t.Errorf("ContainerPort = %q, want %q (resolved from port_map=8181 via the matching ports[] entry)", p.ContainerPort, "8080")
	}
	if p.Scheme != "http" {
		t.Errorf("Scheme = %q, want %q", p.Scheme, "http")
	}
}

func TestParseComposeFileTitlePrefersEnUSOverEmptyCustom(t *testing.T) {
	// Map iteration order is random in Go — this specifically guards against
	// picking "custom" (present but blank) over "en_US" nondeterministically.
	for i := 0; i < 20; i++ {
		data := []byte(`
services:
  app:
    ports:
      - target: 8080
        published: "8080"

x-casaos:
  main: app
  port_map: "8080"
  title:
    custom: ""
    en_US: My App
`)
		p, ok := parseComposeFile(data)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if p.Title != "My App" {
			t.Fatalf("Title = %q, want %q (iteration %d)", p.Title, "My App", i)
		}
	}
}

func TestParseComposeFileSimpleStringTitleNoExplicitMain(t *testing.T) {
	data := []byte(`
services:
  stable-diffusion-webui:
    ports:
      - target: 7860
        published: "7860"

x-casaos:
  title: Stable Diffusion
  scheme: http
  port_map: "7860"
`)
	p, ok := parseComposeFile(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.Title != "Stable Diffusion" || p.MainService != "stable-diffusion-webui" || p.ContainerPort != "7860" {
		t.Errorf("got %+v", p)
	}
}

func TestParseComposeFileDefaultsSchemeToHTTP(t *testing.T) {
	data := []byte(`
services:
  thing:
    ports:
      - target: 9000
        published: "9000"

x-casaos:
  title: Thing
  port_map: "9000"
`)
	p, ok := parseComposeFile(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.Scheme != "http" {
		t.Errorf("Scheme = %q, want default %q", p.Scheme, "http")
	}
}

func TestParseComposeFileSkipsAppsWithoutPortMap(t *testing.T) {
	data := []byte(`
services:
  worker:
    image: example/worker

x-casaos:
  title: Background Worker
`)
	if _, ok := parseComposeFile(data); ok {
		t.Error("expected ok=false for an app with no x-casaos.port_map")
	}
}

func TestParseComposeFileSkipsWhenNoPortsEntryMatchesPortMap(t *testing.T) {
	data := []byte(`
services:
  app:
    ports:
      - target: 8080
        published: "9999"

x-casaos:
  main: app
  port_map: "8080"
  title: App
`)
	if _, ok := parseComposeFile(data); ok {
		t.Error("expected ok=false when no ports[] entry's published value matches port_map")
	}
}

func TestParseComposeFileSkipsMalformedYAML(t *testing.T) {
	if _, ok := parseComposeFile([]byte("not: valid: yaml: [")); ok {
		t.Error("expected ok=false for malformed YAML")
	}
}
