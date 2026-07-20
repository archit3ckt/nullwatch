package config

import (
	"os"
	"path/filepath"
)

// BaseDir returns the root state directory for nullwatch, ~/.nullwatch.
func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nullwatch"), nil
}

// FilePath returns the path to config.yaml inside BaseDir.
func FilePath() (string, error) {
	dir, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// ComposeDir returns the directory where generated docker-compose files live.
func ComposeDir() (string, error) {
	dir, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "compose"), nil
}

// DataDir returns the directory for a module's persistent bind-mounted data
// (e.g. AdGuard config, WireGuard peer configs).
func DataDir(module string) (string, error) {
	dir, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "data", module), nil
}

// EnsureBaseDirs creates ~/.nullwatch and its compose subdirectory if missing.
func EnsureBaseDirs() error {
	dir, err := BaseDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	composeDir, err := ComposeDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(composeDir, 0o700)
}
