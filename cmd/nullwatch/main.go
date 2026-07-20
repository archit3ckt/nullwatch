// Command nullwatch is an interactive provisioner for a self-hosted
// infrastructure stack (AdGuard, WireGuard, Traefik). It runs a wizard,
// writes ~/.nullwatch/config.yaml, generates docker-compose files, and
// reconciles the running containers to match — then exits. It is not a
// dashboard or a long-running service; CasaOS covers that layer.
package main

import (
	"fmt"
	"os"

	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/orchestrator"
	"github.com/archit3ckt/nullwatch/internal/preflight"
	"github.com/archit3ckt/nullwatch/internal/wizard"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := preflight.Ensure(); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}

	previous, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	desired, err := wizard.Run(previous)
	if err != nil {
		return fmt.Errorf("wizard: %w", err)
	}

	if err := config.Save(desired); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	if err := orchestrator.Apply(previous, desired); err != nil {
		return fmt.Errorf("apply: %w", err)
	}

	fmt.Println("\nDone. Config: ~/.nullwatch/config.yaml, compose files: ~/.nullwatch/compose/")
	return nil
}
