package status_test

import (
	"testing"

	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/status"
)

func TestLinksEmptyBeforeSetup(t *testing.T) {
	cfg := config.Default()
	if links := status.Links(cfg); links != nil {
		t.Errorf("expected no links before WireGuard.Host is set, got %v", links)
	}
}

func TestLinksAfterSetup(t *testing.T) {
	cfg := config.Default()
	cfg.WireGuard.Host = "203.0.113.5"
	cfg.Traefik.DashboardEnabled = true

	links := status.Links(cfg)

	want := map[string]string{
		"AdGuard Home":      "http://172.30.0.2:3000",
		"WireGuard admin":   "http://172.30.0.4:51821",
		"Traefik dashboard": "http://172.30.0.3:8080",
		"CasaOS":            "http://172.30.0.1:81",
	}
	if len(links) != len(want) {
		t.Fatalf("got %d links, want %d: %+v", len(links), len(want), links)
	}
	for _, l := range links {
		if want[l.Name] != l.URL {
			t.Errorf("%s = %s, want %s", l.Name, l.URL, want[l.Name])
		}
	}
}

func TestLinksCasaOSUsesFriendlyHostnameWhenWired(t *testing.T) {
	cfg := config.Default()
	cfg.WireGuard.Host = "203.0.113.5"
	cfg.Global.Domain = "home.arpa"

	links := status.Links(cfg)
	for _, l := range links {
		if l.Name == "CasaOS" {
			if l.URL != "http://casaos.home.arpa" {
				t.Errorf("CasaOS = %s, want http://casaos.home.arpa", l.URL)
			}
			return
		}
	}
	t.Fatal("no CasaOS link returned")
}

func TestLinksOmitsDisabledTraefikDashboard(t *testing.T) {
	cfg := config.Default()
	cfg.WireGuard.Host = "203.0.113.5"
	cfg.Traefik.DashboardEnabled = false

	for _, l := range status.Links(cfg) {
		if l.Name == "Traefik dashboard" {
			t.Error("dashboard link should be omitted when DashboardEnabled is false")
		}
	}
}
