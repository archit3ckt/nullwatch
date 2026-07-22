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

func TestParseComposeFileSimpleTitle(t *testing.T) {
	data := []byte(`
services:
  stable-diffusion-webui:
    image: example/sdwebui

x-casaos:
  title: Stable Diffusion
  main: stable-diffusion-webui
  webui:
    scheme: http
    index: /
    port_map: "7860"
`)
	p, ok := parseComposeFile(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.Title != "Stable Diffusion" || p.MainService != "stable-diffusion-webui" ||
		p.ContainerPort != "7860" || p.Scheme != "http" {
		t.Errorf("got %+v", p)
	}
}

func TestParseComposeFileBilingualTitle(t *testing.T) {
	data := []byte(`
services:
  app:
    image: example/app

x-casaos:
  title:
    en_us: My App
    zh_cn: 我的应用
  main: app
  webui:
    port_map: "8080"
`)
	p, ok := parseComposeFile(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.Title != "My App" {
		t.Errorf("Title = %q, want %q", p.Title, "My App")
	}
	if p.Scheme != "http" {
		t.Errorf("Scheme = %q, want default %q", p.Scheme, "http")
	}
}

func TestParseComposeFileNoMainFallsBackToOnlyService(t *testing.T) {
	data := []byte(`
services:
  onlyservice:
    image: example/thing

x-casaos:
  title: Thing
  webui:
    port_map: "9000"
`)
	p, ok := parseComposeFile(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.MainService != "onlyservice" {
		t.Errorf("MainService = %q, want %q", p.MainService, "onlyservice")
	}
}

func TestParseComposeFileSkipsAppsWithoutWebUI(t *testing.T) {
	data := []byte(`
services:
  worker:
    image: example/worker

x-casaos:
  title: Background Worker
`)
	if _, ok := parseComposeFile(data); ok {
		t.Error("expected ok=false for an app with no x-casaos.webui block")
	}
}

func TestParseComposeFileSkipsMalformedYAML(t *testing.T) {
	if _, ok := parseComposeFile([]byte("not: valid: yaml: [")); ok {
		t.Error("expected ok=false for malformed YAML")
	}
}
