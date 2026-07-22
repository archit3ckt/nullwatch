package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/archit3ckt/nullwatch/internal/casaos"
	"github.com/archit3ckt/nullwatch/internal/casaosapps"
	"github.com/archit3ckt/nullwatch/internal/config"
)

// runCasaOSWatch is the entrypoint for the nullwatch-casaos-watcher systemd
// service (see internal/casaosapps.EnsureWatcherService) — invoked as
// `nullwatch --casaos-watch` rather than a separate binary, so there's
// nothing extra to build or install. Does one reconcile immediately, then
// blocks reacting to `docker events` rather than polling: a container
// starting or dying is checked as it happens, not up to a poll interval
// later.
func runCasaOSWatch() error {
	if !casaos.Installed() {
		fmt.Println("CasaOS isn't installed — nothing to watch, exiting.")
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Traefik == nil || !cfg.Traefik.Enabled || cfg.Global.Domain == "" {
		fmt.Println("Traefik isn't enabled or no domain is configured — nothing to route, exiting.")
		return nil
	}

	dataDir, err := config.DataDir("traefik")
	if err != nil {
		return err
	}
	dynamicDir := filepath.Join(dataDir, "dynamic")

	reconcile := func() {
		apps, err := casaosapps.Scan()
		if err != nil {
			fmt.Fprintln(os.Stderr, "casaos-watch: scan:", err)
			return
		}
		if err := casaosapps.ReconcileTraefikRoutes(cfg.Global.Domain, dynamicDir, apps); err != nil {
			fmt.Fprintln(os.Stderr, "casaos-watch: reconcile:", err)
			return
		}
		fmt.Printf("casaos-watch: reconciled %d app route(s)\n", len(apps))
	}

	reconcile()

	// No --format: its Go-template fields (Status, Actor, ...) come from
	// Docker's internal event struct, which has changed shape across
	// versions — confirmed to break here ("can't evaluate field Status")
	// on a real deployment. The content of each line is never parsed below
	// anyway (any event triggers a full reconcile), so the default
	// plain-text output, guaranteed stable across versions, is all that's
	// needed.
	cmd := exec.Command("docker", "events",
		"--filter", "type=container",
		"--filter", "event=start",
		"--filter", "event=die",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("docker events pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start docker events: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		fmt.Println("casaos-watch: event:", scanner.Text())
		reconcile()
	}
	return cmd.Wait()
}
