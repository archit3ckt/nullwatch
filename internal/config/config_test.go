package config_test

import (
	"os"
	"testing"

	"github.com/archit3ckt/nullwatch/internal/config"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Default()
	cfg.Global.Domain = "example.com"
	cfg.AdGuard.AdminUser = "admin"
	cfg.AdGuard.AdminPassword = "s3cret"

	if err := config.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	path, err := config.FilePath()
	if err != nil {
		t.Fatalf("file path: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat config file: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 perms, got %o", info.Mode().Perm())
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Global.Domain != "example.com" {
		t.Errorf("domain = %q, want example.com", loaded.Global.Domain)
	}
	if !loaded.AdGuard.Enabled || loaded.AdGuard.AdminUser != "admin" {
		t.Errorf("adguard config not round-tripped: %+v", loaded.AdGuard)
	}
	if !loaded.WireGuard.Enabled || !loaded.Traefik.Enabled {
		t.Errorf("wireguard and traefik are mandatory and should stay enabled: %+v / %+v", loaded.WireGuard, loaded.Traefik)
	}
}

func TestLoadWithNoExistingFileReturnsMandatoryDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.EnabledSet(); len(got) != 3 {
		t.Errorf("expected all 3 core modules enabled on fresh install, got %v", got)
	}
}

func TestDiffEnabled(t *testing.T) {
	previous := config.Default()
	previous.WireGuard.Enabled = false // simulates a config from before this module was applied

	desired := config.Default()

	diff := config.DiffEnabled(previous, desired)

	if len(diff.ToStart) != 1 || diff.ToStart[0] != "wireguard" {
		t.Errorf("ToStart = %v, want [wireguard]", diff.ToStart)
	}
	if len(diff.ToStop) != 0 {
		t.Errorf("ToStop = %v, want []", diff.ToStop)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	original := config.Default()
	original.AdGuard.Enabled = false
	original.AdGuard.Blocklists = []string{"a", "b"}

	clone := original.Clone()
	clone.AdGuard.Enabled = true
	clone.AdGuard.Blocklists[0] = "mutated"

	if original.AdGuard.Enabled {
		t.Error("mutating clone's Enabled affected original")
	}
	if original.AdGuard.Blocklists[0] != "a" {
		t.Error("mutating clone's Blocklists slice affected original")
	}
}
