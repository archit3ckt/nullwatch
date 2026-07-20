package compose_test

import (
	"strings"
	"testing"

	"github.com/archit3ckt/nullwatch/internal/compose"
)

func TestRenderAllTemplates(t *testing.T) {
	cases := []struct {
		name     string
		template string
		data     any
	}{
		{
			"adguard",
			"adguard-compose.yml.tmpl",
			struct {
				Image, NetworkName, StaticIP, ConfDir, WorkDir string
				HTTPPort, DNSPort                              int
			}{"adguard/adguardhome:v0.107.52", compose.NetworkName, "172.30.0.2", "/data/conf", "/data/work", 3000, 53},
		},
		{
			"wireguard",
			"wireguard-compose.yml.tmpl",
			struct {
				Image, Host, DefaultAddr, DNS, Password, DataDir, NetworkName, StaticIP string
				Port, WebUIPort                                                         int
			}{"ghcr.io/wg-easy/wg-easy:11", "vpn.example.com", "10.8.0.x", "172.30.0.2", "hunter2", "/data/wg", compose.NetworkName, "172.30.0.4", 51820, 51821},
		},
		{
			"traefik-nodash",
			"traefik-compose.yml.tmpl",
			struct {
				Image, ACMEEmail, DataDir, DynamicDir, NetworkName, StaticIP string
				HTTPPort, HTTPSPort, DashboardPort                           int
				DashboardEnabled                                             bool
			}{"traefik:v3.2", "me@example.com", "/data/traefik", "/data/traefik/dynamic", compose.NetworkName, "172.30.0.3", 80, 443, 8080, false},
		},
		{
			"traefik-dashboard",
			"traefik-compose.yml.tmpl",
			struct {
				Image, ACMEEmail, DataDir, DynamicDir, NetworkName, StaticIP string
				HTTPPort, HTTPSPort, DashboardPort                           int
				DashboardEnabled                                             bool
			}{"traefik:v3.2", "me@example.com", "/data/traefik", "/data/traefik/dynamic", compose.NetworkName, "172.30.0.3", 80, 443, 8080, true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := compose.Render(tc.template, tc.data)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if len(out) == 0 {
				t.Fatal("empty output")
			}
			if !strings.Contains(string(out), "external: true") {
				t.Error("expected generated compose to reference the external shared network")
			}
			t.Logf("%s output:\n%s", tc.name, out)
		})
	}
}
