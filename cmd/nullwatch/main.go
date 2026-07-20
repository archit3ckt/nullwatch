// Command nullwatch is an interactive provisioner and reconfiguration menu
// for a self-hosted infrastructure stack (AdGuard, WireGuard, Traefik,
// CasaOS). It's re-run whenever you want to change something; each action
// writes ~/.nullwatch/config.yaml, generates docker-compose files, and
// reconciles the running containers to match, then returns to the menu.
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"

	"github.com/archit3ckt/nullwatch/internal/casaos"
	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/orchestrator"
	"github.com/archit3ckt/nullwatch/internal/preflight"
	"github.com/archit3ckt/nullwatch/internal/status"
	"github.com/archit3ckt/nullwatch/internal/wizard"
)

const banner = `
$$\   $$\ $$\   $$\ $$\       $$\       $$\      $$\  $$$$$$\ $$$$$$$$\  $$$$$$\  $$\   $$\
$$$\  $$ |$$ |  $$ |$$ |      $$ |      $$ | $\  $$ |$$  __$$\\__$$  __|$$  __$$\ $$ |  $$ |
$$$$\ $$ |$$ |  $$ |$$ |      $$ |      $$ |$$$\ $$ |$$ /  $$ |  $$ |   $$ /  \__|$$ |  $$ |
$$ $$\$$ |$$ |  $$ |$$ |      $$ |      $$ $$ $$\$$ |$$$$$$$$ |  $$ |   $$ |      $$$$$$$$ |
$$ \$$$$ |$$ |  $$ |$$ |      $$ |      $$$$  _$$$$ |$$  __$$ |  $$ |   $$ |      $$  __$$ |
$$ |\$$$ |$$ |  $$ |$$ |      $$ |      $$$  / \$$$ |$$ |  $$ |  $$ |   $$ |  $$\ $$ |  $$ |
$$ | \$$ |\$$$$$$  |$$$$$$$$\ $$$$$$$$\ $$  /   \$$ |$$ |  $$ |  $$ |   \$$$$$$  |$$ |  $$ |
\__|  \__| \______/ \________|\________|\__/     \__|\__|  \__|  \__|    \______/ \__|  \__|
`

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

	for {
		fmt.Print(banner)
		printLinks(previous)

		var choice string
		err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Options(
					huh.NewOption("Full setup (AdGuard + WireGuard + Traefik + CasaOS)", "full"),
					huh.NewOption("Reconfigure AdGuard", "adguard"),
					huh.NewOption("Reconfigure WireGuard", "wireguard"),
					huh.NewOption("Reconfigure Traefik", "traefik"),
					huh.NewOption("Install/check CasaOS", "casaos"),
					huh.NewOption("Show status & URLs", "status"),
					huh.NewOption("Exit", "exit"),
				).
				Value(&choice),
		)).Run()
		if err != nil {
			return fmt.Errorf("menu: %w", err)
		}

		switch choice {
		case "exit":
			return nil
		case "full":
			desired, err := wizard.RunFull(previous)
			if err != nil {
				return err
			}
			if err := applyAndSave(previous, desired); err != nil {
				return err
			}
			if err := casaos.EnsureInstalled(); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
			previous = desired
			printNextSteps(previous)
		case "casaos":
			if err := casaos.EnsureInstalled(); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
		case "status":
			printNextSteps(previous)
		case "adguard", "wireguard", "traefik":
			desired, err := wizard.RunModule(previous, choice)
			if err != nil {
				return err
			}
			if err := config.Save(desired); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			if err := orchestrator.ApplyOne(desired, choice); err != nil {
				return fmt.Errorf("apply %s: %w", choice, err)
			}
			previous = desired
			printNextSteps(previous)
		}
	}
}

func applyAndSave(previous, desired *config.Config) error {
	if err := config.Save(desired); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := orchestrator.Apply(previous, desired); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	return nil
}

func printLinks(cfg *config.Config) {
	links := status.Links(cfg)
	if len(links) == 0 {
		fmt.Println("Not set up yet — pick \"Full setup\" below to get started.")
		fmt.Println()
		return
	}
	fmt.Println("Quick links:")
	for _, l := range links {
		fmt.Printf("  %-18s %s\n", l.Name+":", l.URL)
	}
	fmt.Println()
}

func printNextSteps(cfg *config.Config) {
	fmt.Println("\nDone. Config: ~/.nullwatch/config.yaml, compose files: ~/.nullwatch/compose/")
	printLinks(cfg)
	fmt.Println("Next steps:")
	fmt.Println("  - Point your domain's DNS A record at this server's public IP.")
	fmt.Println("  - Open ports 80/443 (Traefik) and your WireGuard UDP port in the firewall.")
	fmt.Println("  - Log into AdGuard and WireGuard above with the credentials you just set.")
	fmt.Println("  - Add a WireGuard client and confirm its DNS resolves through AdGuard.")
	fmt.Println()
}
