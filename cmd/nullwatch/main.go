// Command nullwatch is an interactive provisioner and reconfiguration menu
// for a self-hosted infrastructure stack (AdGuard, WireGuard, Traefik,
// CasaOS). It's re-run whenever you want to change something; each action
// writes ~/.nullwatch/config.yaml, generates docker-compose files, and
// reconciles the running containers to match, then returns to the menu.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/mdp/qrterminal/v3"

	"github.com/archit3ckt/nullwatch/internal/casaos"
	"github.com/archit3ckt/nullwatch/internal/casaosapps"
	"github.com/archit3ckt/nullwatch/internal/config"
	"github.com/archit3ckt/nullwatch/internal/firewall"
	"github.com/archit3ckt/nullwatch/internal/modules/wireguard"
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
	if len(os.Args) > 1 && os.Args[1] == "--casaos-watch" {
		if err := runCasaOSWatch(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
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
					huh.NewOption("Add WireGuard peer", "wg-peer"),
					huh.NewOption("Lock down firewall (VPN-only access)", "firewall"),
					huh.NewOption("Show status & URLs", "status"),
					huh.NewOption("Uninstall", "uninstall"),
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

			// config.Save happens before docker apply, so even if apply
			// fails partway through, desired is now the source of truth —
			// and CasaOS/firewall still run below regardless, since a
			// partially-applied stack left with no firewall lockdown is
			// worse than one that's still locked down while broken.
			applyErr := applyAndSave(previous, desired)
			if applyErr != nil {
				fmt.Fprintln(os.Stderr, "warning: apply failed, continuing to CasaOS/firewall so the stack isn't left unprotected:", applyErr)
			}
			previous = desired

			if err := casaos.EnsureInstalled(); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
			if err := ensureCasaOSWatcher(previous); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
			if err := firewall.Apply(previous); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
			printNextSteps(previous)

			if applyErr != nil {
				return applyErr
			}
		case "casaos":
			if err := casaos.EnsureInstalled(); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
			if err := ensureCasaOSWatcher(previous); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
		case "wg-peer":
			if err := addWireGuardPeer(previous); err != nil {
				fmt.Fprintln(os.Stderr, "warning:", err)
			}
		case "firewall":
			if err := firewall.Apply(previous); err != nil {
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
			if choice == "traefik" {
				if err := ensureCasaOSWatcher(previous); err != nil {
					fmt.Fprintln(os.Stderr, "warning:", err)
				}
			}
			printNextSteps(previous)
		case "uninstall":
			updated, err := runUninstall(previous)
			if err != nil {
				return err
			}
			previous = updated
		}
	}
}

// runUninstall walks through three separate, increasingly destructive
// confirmations — containers, then config/data, then the binary itself —
// so declining the first doesn't skip straight past the others.
func runUninstall(previous *config.Config) (*config.Config, error) {
	teardown := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Stop and remove AdGuard, WireGuard, and Traefik?").
			Description("Removes their containers and the shared Docker network. CasaOS and its apps are not touched.").
			Affirmative("Uninstall").
			Negative("Cancel").
			Value(&teardown),
	)).Run(); err != nil {
		return previous, fmt.Errorf("uninstall prompt: %w", err)
	}
	if !teardown {
		fmt.Println("Cancelled.")
		return previous, nil
	}

	if err := orchestrator.Teardown(); err != nil {
		return previous, fmt.Errorf("teardown: %w", err)
	}
	fmt.Println("==> containers and network removed")

	if casaos.Installed() {
		removeCasaOS := false
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title("Also uninstall CasaOS?").
				Description("Removes CasaOS and every app it manages (Nextcloud, Jellyfin, etc.) via its own uninstaller. Cannot be undone.").
				Affirmative("Uninstall it").
				Negative("Keep it").
				Value(&removeCasaOS),
		)).Run(); err != nil {
			return previous, fmt.Errorf("casaos uninstall prompt: %w", err)
		}
		if removeCasaOS {
			if err := casaos.Uninstall(); err != nil {
				return previous, fmt.Errorf("uninstall casaos: %w", err)
			}
			fmt.Println("==> CasaOS removed")
		}
	}

	deleteData := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Also delete ~/.nullwatch?").
			Description("Removes config.yaml and all persisted data — AdGuard settings, WireGuard peer configs, Traefik certs. Cannot be undone.").
			Affirmative("Delete it").
			Negative("Keep it").
			Value(&deleteData),
	)).Run(); err != nil {
		return previous, fmt.Errorf("delete data prompt: %w", err)
	}
	if deleteData {
		dir, err := config.BaseDir()
		if err != nil {
			return previous, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return previous, fmt.Errorf("remove %s: %w", dir, err)
		}
		fmt.Printf("==> removed %s\n", dir)
		previous = config.Default()
	}

	removeBinary := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Also remove the nullwatch binary itself?").
			Value(&removeBinary),
	)).Run(); err != nil {
		return previous, fmt.Errorf("remove binary prompt: %w", err)
	}
	if removeBinary {
		exe, err := os.Executable()
		if err != nil {
			return previous, fmt.Errorf("find nullwatch binary: %w", err)
		}
		if err := os.Remove(exe); err != nil {
			return previous, fmt.Errorf("remove %s: %w", exe, err)
		}
		fmt.Printf("==> removed %s. Goodbye.\n", exe)
		os.Exit(0)
	}

	return previous, nil
}

// addWireGuardPeer creates a new peer via wg-easy's API over localhost —
// nullwatch runs on the VPS itself, so this never touches the public
// interface and structurally never needs the firewall opened, unlike
// visiting wg-easy's own web UI directly. Prints the config and a
// scannable QR code, and saves the config to disk.
func addWireGuardPeer(cfg *config.Config) error {
	if cfg.WireGuard == nil || !cfg.WireGuard.Enabled {
		return fmt.Errorf("wireguard isn't enabled — run full setup first")
	}

	name := ""
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Peer name").
			Description("e.g. \"phone\" or \"laptop\" — used to identify this client in wg-easy.").
			Value(&name).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("name is required")
				}
				return nil
			}),
	)).Run(); err != nil {
		return fmt.Errorf("peer name prompt: %w", err)
	}

	client := wireguard.NewClient(cfg.WireGuard)
	_, conf, err := client.CreatePeer(name)
	if err != nil {
		return fmt.Errorf("create wireguard peer: %w", err)
	}

	dataDir, err := config.DataDir("wireguard")
	if err != nil {
		return err
	}
	peerDir := filepath.Join(dataDir, "peers")
	if err := os.MkdirAll(peerDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", peerDir, err)
	}
	peerPath := filepath.Join(peerDir, name+".conf")
	if err := os.WriteFile(peerPath, []byte(conf), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", peerPath, err)
	}

	fmt.Printf("\n==> peer %q created, saved to %s\n\n", name, peerPath)
	fmt.Println(conf)
	fmt.Println("Scan this with the WireGuard app:")
	qrterminal.GenerateHalfBlock(conf, qrterminal.L, os.Stdout)
	fmt.Println()
	return nil
}

// ensureCasaOSWatcher enables the automatic CasaOS-app routing service if
// its preconditions are met, and quietly does nothing otherwise (no
// domain/Traefik configured yet, or CasaOS not installed — the latter
// checked inside EnsureWatcherService itself) rather than erroring, since
// this is a nice-to-have that most menu paths shouldn't block on.
func ensureCasaOSWatcher(cfg *config.Config) error {
	if cfg.Traefik == nil || !cfg.Traefik.Enabled || cfg.Global.Domain == "" {
		return nil
	}
	return casaosapps.EnsureWatcherService()
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
	fmt.Println("Quick links (only reachable once connected to the VPN):")
	for _, l := range links {
		fmt.Printf("  %-18s %s\n", l.Name+":", l.URL)
	}
	fmt.Println("  (all private addresses on the VPN's own network — reachable over the tunnel")
	fmt.Println("   like any other machine on a LAN. CasaOS isn't containerized by nullwatch, but")
	fmt.Println("   it listens on every interface the host has, including this network's gateway,")
	fmt.Println("   so it's reached the same way. Apps installed through CasaOS get their own")
	fmt.Println("   <app>.<domain> URL automatically once the app-route watcher is enabled.)")
	fmt.Println()
}

func printNextSteps(cfg *config.Config) {
	fmt.Println("\nDone. Config: ~/.nullwatch/config.yaml, compose files: ~/.nullwatch/compose/")
	printLinks(cfg)
	fmt.Println("Next steps:")
	fmt.Println("  - If you haven't yet, run \"Lock down firewall\" from the menu — nothing but")
	fmt.Println("    the WireGuard tunnel should be reachable from the public internet, including")
	fmt.Println("    the WireGuard admin UI above (it's a Docker-published port like everything else).")
	fmt.Println("  - Run \"Add WireGuard peer\" from the menu to create your first client — it talks")
	fmt.Println("    to wg-easy over localhost, so it works even before you're connected to the VPN.")
	fmt.Println("    Scan the printed QR code, connect, and only then the URLs above will load.")
	fmt.Println("  - Traefik uses a self-signed cert (nothing here is publicly reachable for")
	fmt.Println("    Let's Encrypt to validate), so your browser will warn once — that's expected.")
	fmt.Println()
}
