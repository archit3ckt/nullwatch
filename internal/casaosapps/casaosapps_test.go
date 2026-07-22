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
// fields) — confirmed to differ from the nested "webui: {port_map, scheme}"
// shape this package originally assumed from public AppStore examples:
// port_map/scheme/title live directly under x-casaos, and port_map is
// already the resolved host-published port (8181), not the container's
// internal port (8080, visible in the real file's own ports: block).
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
	if p.PortMap != "8181" {
		t.Errorf("PortMap = %q, want %q (the published port, not the container port 8080)", p.PortMap, "8181")
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
x-casaos:
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

func TestParseComposeFileSimpleStringTitle(t *testing.T) {
	data := []byte(`
x-casaos:
  title: Stable Diffusion
  scheme: http
  port_map: "7860"
`)
	p, ok := parseComposeFile(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.Title != "Stable Diffusion" || p.PortMap != "7860" || p.Scheme != "http" {
		t.Errorf("got %+v", p)
	}
}

func TestParseComposeFileDefaultsSchemeToHTTP(t *testing.T) {
	data := []byte(`
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
x-casaos:
  title: Background Worker
`)
	if _, ok := parseComposeFile(data); ok {
		t.Error("expected ok=false for an app with no x-casaos.port_map")
	}
}

func TestParseComposeFileSkipsMalformedYAML(t *testing.T) {
	if _, ok := parseComposeFile([]byte("not: valid: yaml: [")); ok {
		t.Error("expected ok=false for malformed YAML")
	}
}
