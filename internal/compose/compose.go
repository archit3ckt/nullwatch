// Package compose renders per-module docker-compose files from embedded
// templates and shells out to the `docker compose` CLI to bring modules up
// or down. Deliberately thin: a human should be able to read the generated
// files in ~/.nullwatch/compose and run the same `docker compose` commands
// by hand.
package compose

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/templates"
)

// ProjectPrefix namespaces docker compose projects so they don't collide
// with CasaOS or other stacks on the same host.
const ProjectPrefix = "nullwatch"

// Render executes the named embedded template with data and returns the
// resulting compose YAML.
func Render(templateName string, data any) ([]byte, error) {
	raw, err := templates.FS.ReadFile(templateName)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", templateName, err)
	}

	tmpl, err := template.New(templateName).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", templateName, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", templateName, err)
	}
	return buf.Bytes(), nil
}

// FilePath returns the on-disk path for a module's generated compose file.
func FilePath(module string) (string, error) {
	dir, err := config.ComposeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, module+".yml"), nil
}

// Write renders and saves a module's compose file under
// ~/.nullwatch/compose/<module>.yml.
func Write(module, templateName string, data any) (string, error) {
	if err := config.EnsureBaseDirs(); err != nil {
		return "", err
	}

	out, err := Render(templateName, data)
	if err != nil {
		return "", err
	}

	path, err := FilePath(module)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return "", fmt.Errorf("write compose file %s: %w", path, err)
	}
	return path, nil
}

// Up brings a module's compose stack up, creating or updating containers as
// needed. Idempotent: re-running with no changes to the rendered compose
// file restarts nothing.
func Up(module string) error {
	return run(module, "up", "-d", "--remove-orphans")
}

// Down stops and removes a module's containers. Named volumes and bind
// mounts under ~/.nullwatch/data are left untouched.
func Down(module string) error {
	return run(module, "down")
}

// Remove deletes a module's generated compose file after it has been
// brought down. User data under ~/.nullwatch/data is left in place.
func Remove(module string) error {
	path, err := FilePath(module)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Exists reports whether a module currently has a generated compose file.
func Exists(module string) (bool, error) {
	path, err := FilePath(module)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func run(module string, args ...string) error {
	path, err := FilePath(module)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("compose file for %s not found at %s: %w", module, path, err)
	}

	full := append([]string{"compose", "-f", path, "-p", ProjectPrefix + "-" + module}, args...)
	cmd := exec.Command("docker", full...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %v: %w", full, err)
	}
	return nil
}
