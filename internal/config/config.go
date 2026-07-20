// Package config defines the declarative schema for ~/.nullwatch/config.yaml
// and provides load/save helpers. This file is the single source of truth
// for which modules are enabled and how they're parameterized; it is meant
// to be human-readable and safe to hand-edit.
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is bumped whenever the on-disk config shape changes in a way
// that requires migration.
const SchemaVersion = 1

// Config is the root document written to config.yaml.
type Config struct {
	Version   int              `yaml:"version"`
	Global    GlobalConfig     `yaml:"global"`
	AdGuard   *AdGuardConfig   `yaml:"adguard,omitempty"`
	WireGuard *WireGuardConfig `yaml:"wireguard,omitempty"`
	Traefik   *TraefikConfig   `yaml:"traefik,omitempty"`
}

// GlobalConfig holds settings shared across modules.
type GlobalConfig struct {
	// Domain is the base domain used for Traefik routing (*.Domain) and the
	// matching AdGuard DNS rewrite. Required if traefik is enabled.
	Domain string `yaml:"domain,omitempty"`
}

// AdGuardConfig configures the AdGuard Home container, the stack's DNS
// resolver and blocklist-based tracker/ad filtering layer.
type AdGuardConfig struct {
	Enabled bool `yaml:"enabled"`

	HTTPPort int `yaml:"http_port"` // AdGuard web UI
	DNSPort  int `yaml:"dns_port"`  // DNS listener (53)

	AdminUser     string `yaml:"admin_user"`
	AdminPassword string `yaml:"admin_password"`

	// Blocklists are filter list URLs applied on top of AdGuard's defaults.
	// Should include tracker/analytics/telemetry lists, not just ad lists,
	// since DNS-level blocking is part of this project's privacy story.
	Blocklists []string `yaml:"blocklists"`
}

// WireGuardConfig configures wg-easy as a full-tunnel VPN server. Once
// configured, this is meant to be the only way in/out for client devices,
// so client DNS is pushed through AdGuard to avoid leaking lookups.
type WireGuardConfig struct {
	Enabled bool `yaml:"enabled"`

	// Host is the public IP or DNS name clients connect to.
	Host string `yaml:"host"`
	Port int    `yaml:"port"` // UDP listen port, default 51820

	Subnet string `yaml:"subnet"` // e.g. 10.8.0.0/24

	WebUIPort     int    `yaml:"webui_port"`
	WebUIPassword string `yaml:"webui_password"`

	// DNS is the resolver pushed to clients. Auto-populated with AdGuard's
	// in-network address when both modules are enabled; otherwise left to
	// the user (or blank to fall back to the container image's default).
	DNS string `yaml:"dns,omitempty"`
}

// TraefikConfig configures Traefik as the reverse proxy for *.Domain,
// routing to backend containers via Docker label discovery.
type TraefikConfig struct {
	Enabled bool `yaml:"enabled"`

	ACMEEmail string `yaml:"acme_email"`

	HTTPPort  int `yaml:"http_port"`  // 80, used for ACME HTTP-01 + redirect
	HTTPSPort int `yaml:"https_port"` // 443

	DashboardEnabled bool `yaml:"dashboard_enabled"`
	DashboardPort    int  `yaml:"dashboard_port"`
}

// Default returns a fresh Config with the core infrastructure stack
// enabled — AdGuard, WireGuard, and Traefik are mandatory, not optional
// picks, so Default() reflects that baseline rather than an empty install.
func Default() *Config {
	return &Config{
		Version: SchemaVersion,
		AdGuard: &AdGuardConfig{
			Enabled:  true,
			HTTPPort: 3000,
			DNSPort:  53,
			Blocklists: []string{
				"https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt", // AdGuard DNS filter
				"https://adguardteam.github.io/HostlistsRegistry/assets/filter_3.txt", // AdGuard Tracking Protection filter
				"https://big.oisd.nl/domainswild",                                     // OISD Big
			},
		},
		WireGuard: &WireGuardConfig{
			Enabled:   true,
			Port:      51820,
			Subnet:    "10.8.0.0/24",
			WebUIPort: 51821,
		},
		Traefik: &TraefikConfig{
			Enabled:       true,
			HTTPPort:      80,
			HTTPSPort:     443,
			DashboardPort: 8080,
		},
	}
}

// Load reads config.yaml from disk. If the file doesn't exist yet, it
// returns Default() with no error so callers can treat "no config" as the
// first-run case uniformly.
func Load() (*Config, error) {
	path, err := FilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes cfg to config.yaml, creating ~/.nullwatch if necessary.
// The file is kept human-readable and world-unreadable (0600) since it may
// contain admin/WebUI passwords.
func Save(cfg *Config) error {
	if err := EnsureBaseDirs(); err != nil {
		return err
	}
	path, err := FilePath()
	if err != nil {
		return err
	}

	cfg.Version = SchemaVersion

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Clone returns a deep copy of cfg so callers (the wizard) can mutate a
// working copy while the original is kept around as the "previous" state
// for diffing.
func (c *Config) Clone() *Config {
	clone := *c
	if c.AdGuard != nil {
		ag := *c.AdGuard
		ag.Blocklists = append([]string(nil), c.AdGuard.Blocklists...)
		clone.AdGuard = &ag
	}
	if c.WireGuard != nil {
		wg := *c.WireGuard
		clone.WireGuard = &wg
	}
	if c.Traefik != nil {
		tk := *c.Traefik
		clone.Traefik = &tk
	}
	return &clone
}

// EnabledSet returns the set of module names currently enabled in cfg.
func (c *Config) EnabledSet() map[string]bool {
	set := map[string]bool{}
	if c.AdGuard != nil && c.AdGuard.Enabled {
		set["adguard"] = true
	}
	if c.WireGuard != nil && c.WireGuard.Enabled {
		set["wireguard"] = true
	}
	if c.Traefik != nil && c.Traefik.Enabled {
		set["traefik"] = true
	}
	return set
}
