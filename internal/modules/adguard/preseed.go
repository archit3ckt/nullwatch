package adguard

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"github.com/archit3ckt/nullwatch/internal/config"
)

// adguardHomeConfig mirrors the subset of AdGuardHome.yaml's schema needed
// to preseed admin credentials, DNS, and filters. Preseeding this file lets
// the container skip its interactive first-run setup wizard entirely, which
// is what makes the DNS rewrite REST API usable immediately after `up -d`.
//
// schema_version is coupled to the pinned image tag in template data
// (see adguard.go); bump it if the image is upgraded and AdGuard refuses to
// start with a schema mismatch it can't auto-migrate.
type adguardHomeConfig struct {
	SchemaVersion int          `yaml:"schema_version"`
	HTTP          adgHTTP      `yaml:"http"`
	Users         []adgUser    `yaml:"users"`
	DNS           adgDNS       `yaml:"dns"`
	Filtering     adgFiltering `yaml:"filtering"`
	Filters       []adgFilter  `yaml:"filters"`
}

type adgHTTP struct {
	Address string `yaml:"address"`
}

type adgUser struct {
	Name         string `yaml:"name"`
	PasswordHash string `yaml:"password"`
}

type adgDNS struct {
	BindHosts    []string `yaml:"bind_hosts"`
	Port         int      `yaml:"port"`
	UpstreamDNS  []string `yaml:"upstream_dns"`
	BootstrapDNS []string `yaml:"bootstrap_dns"`
}

type adgFiltering struct {
	FilteringEnabled bool `yaml:"filtering_enabled"`
}

type adgFilter struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	Name    string `yaml:"name"`
	ID      int    `yaml:"id"`
}

// schemaVersion targets the AdGuardHome.yaml format used by the adguard
// image tag pinned in the compose template.
const schemaVersion = 28

// generatePreseed builds the AdGuardHome.yaml bytes to bind-mount into the
// container's config volume so it boots pre-configured.
func generatePreseed(cfg *config.AdGuardConfig) ([]byte, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash admin password: %w", err)
	}

	filters := make([]adgFilter, 0, len(cfg.Blocklists))
	for i, url := range cfg.Blocklists {
		filters = append(filters, adgFilter{
			Enabled: true,
			URL:     url,
			Name:    fmt.Sprintf("blocklist-%d", i+1),
			ID:      i + 1,
		})
	}

	preseed := adguardHomeConfig{
		SchemaVersion: schemaVersion,
		HTTP: adgHTTP{
			Address: fmt.Sprintf("0.0.0.0:%d", cfg.HTTPPort),
		},
		Users: []adgUser{
			{Name: cfg.AdminUser, PasswordHash: string(hash)},
		},
		DNS: adgDNS{
			BindHosts:   []string{"0.0.0.0"},
			Port:        cfg.DNSPort,
			UpstreamDNS: []string{"https://dns.quad9.net/dns-query"},
			// Quad9's own resolvers, used only to bootstrap the DoH upstream
			// above — never queried directly for user lookups.
			BootstrapDNS: []string{"9.9.9.10", "149.112.112.10"},
		},
		Filtering: adgFiltering{FilteringEnabled: true},
		Filters:   filters,
	}

	return yaml.Marshal(preseed)
}
