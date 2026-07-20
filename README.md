```
$$\   $$\ $$\   $$\ $$\       $$\       $$\      $$\  $$$$$$\ $$$$$$$$\  $$$$$$\  $$\   $$\ 
$$$\  $$ |$$ |  $$ |$$ |      $$ |      $$ | $\  $$ |$$  __$$\\__$$  __|$$  __$$\ $$ |  $$ |
$$$$\ $$ |$$ |  $$ |$$ |      $$ |      $$ |$$$\ $$ |$$ /  $$ |  $$ |   $$ /  \__|$$ |  $$ |
$$ $$\$$ |$$ |  $$ |$$ |      $$ |      $$ $$ $$\$$ |$$$$$$$$ |  $$ |   $$ |      $$$$$$$$ |
$$ \$$$$ |$$ |  $$ |$$ |      $$ |      $$$$  _$$$$ |$$  __$$ |  $$ |   $$ |      $$  __$$ |
$$ |\$$$ |$$ |  $$ |$$ |      $$ |      $$$  / \$$$ |$$ |  $$ |  $$ |   $$ |  $$\ $$ |  $$ |
$$ | \$$ |\$$$$$$  |$$$$$$$$\ $$$$$$$$\ $$  /   \$$ |$$ |  $$ |  $$ |   \$$$$$$  |$$ |  $$ |
\__|  \__| \______/ \________|\________|\__/     \__|\__|  \__|  \__|    \______/ \__|  \__|
```

# nullwatch

Turn a bare VPS into your own private cloud and internet gateway — so your
DNS lookups, your VPN traffic, and your data don't have to pass through a
big tech company's infrastructure to work.

nullwatch is the interactive CLI that provisions the pieces that make that
possible: **AdGuard Home** (your own DNS resolver, with tracker/ad
blocklists), **WireGuard** (a full-tunnel VPN — the only way in or out once
it's set up), and **Traefik** (reverse proxy for whatever you host) — wired
together automatically, on infrastructure you actually control, with no
third-party accounts, relays, or telemetry involved.

It's a single static Go binary. No background service — you run it, pick
something from a menu, and it exits when you're done. Everything it
generates (config, compose files) is plain text you can read, edit by hand,
and version-control yourself.

## Why

The point is to let you run your own private cloud: full control over your
data, no dependence on big tech infrastructure, and no data collection by
third parties. That shapes some concrete defaults:

- **No telemetry in the CLI itself**, and container configs default to
  disabling whatever built-in telemetry/usage-reporting the upstream images
  ship with.
- **AdGuard's blocklists include tracker/analytics/telemetry lists**, not
  just ad-blocking lists — DNS-level blocking is part of the privacy story.
- **WireGuard is the only way in/out** for client devices once configured,
  with DNS pushed through AdGuard so lookups can't silently leak to your
  ISP or a third party.
- **No component requires a third-party cloud account or hosted control
  plane.** Everything here is fully self-hostable end to end.
- **Config and compose files stay human-readable** — no obscured state, no
  proprietary formats, nothing you can't inspect or hand-edit.

## What it does and doesn't do

nullwatch provisions three things — AdGuard, WireGuard, and Traefik — as
plain Docker Compose services on a Linux host. All three are always
deployed together, since that's the core infrastructure layer this tool
exists to stand up. What you configure is how they're set up: domain, VPN
subnet, admin credentials, ports.

[CasaOS](https://casaos.io) — the dashboard and one-click app store for
everything else you'd actually use day to day (Nextcloud, Jellyfin, and so
on) — gets installed alongside the infrastructure layer as part of setup.
Past installing it, nullwatch doesn't touch it: no managing its config, its
app store, or its containers. CasaOS auto-detects the containers nullwatch
runs on the same Docker daemon and surfaces them on its own dashboard with
no integration step needed.

## Modules

| Module | Image | Purpose |
|---|---|---|
| **adguard** | `adguard/adguardhome` | DNS resolver for the stack, with curated tracker/analytics/ad blocklists. Preseeded on first boot (admin credentials, filters) so it skips AdGuard's interactive setup wizard and its REST API is usable immediately. |
| **wireguard** | `ghcr.io/wg-easy/wg-easy` | Full-tunnel WireGuard VPN server with a small web UI for managing peers and generating QR codes / client configs. |
| **traefik** | `traefik` | Reverse proxy for `*.yourdomain.com`, routing to backend containers via Docker label discovery, with Let's Encrypt handled automatically. |

### Cross-module wiring

All three modules are wired together automatically:

- **adguard → traefik** — AdGuard gets a wildcard DNS rewrite
  (`*.yourdomain.com` and `yourdomain.com`) pointing at Traefik, registered
  via AdGuard's REST API.
- **adguard → wireguard** — wg-easy's pushed client DNS is set to AdGuard's
  address automatically, so VPN clients resolve through your own blocklists
  instead of leaking DNS elsewhere.

All three share a fixed Docker network (`nullwatch-net`) with static IPs per
container, so this wiring doesn't depend on inspecting containers at
runtime — it's deterministic and works the same on every run.

## Installation

### Prerequisites

Just a Linux host (VPS or otherwise). You don't need to install Docker or Go
yourself first — see below.

### Quick install

```bash
curl -fsSL https://raw.githubusercontent.com/archit3ckt/nullwatch/main/install.sh | sh
```

This bootstraps everything needed to build and run nullwatch:

- Installs Docker (via the official `get.docker.com` convenience script) if
  it's not already present, and adds your user to the `docker` group.
- Downloads a local Go toolchain into `~/go-sdk` (no sudo, no system changes)
  if Go isn't already installed — only needed to build the binary.
- Builds `nullwatch` and installs it to `/usr/local/bin` (requires sudo for
  that last step only), then launches it straight into the wizard.

It's a shell script that installs system packages and touches your home
directory — read it before piping it into a shell, same as you would with
any curl-installer. `sudo apt-get install golang-go` and Docker's own docs
are equally valid ways to get the prerequisites in place if you'd rather do
it by hand.

If you already have Docker but nullwatch still can't find it at runtime
(e.g. you're not on the machine `install.sh` ran on), it'll detect that on
startup and offer to install/start Docker for you interactively, with your
confirmation before anything runs as root.

### Build from source manually

```bash
git clone https://github.com/archit3ckt/nullwatch.git
cd nullwatch
go build -o nullwatch ./cmd/nullwatch
sudo mv nullwatch /usr/local/bin/
```

## Usage

```bash
nullwatch
```

Every run opens the same menu — the banner and, once you've set up at least
once, the live URLs for each service, right at the top:

```
Quick links:
  AdGuard Home:      http://203.0.113.5:3000
  WireGuard admin:   http://203.0.113.5:51821
  CasaOS:            http://203.0.113.5

What do you want to do?
> Full setup (AdGuard + WireGuard + Traefik + CasaOS)
  Reconfigure AdGuard
  Reconfigure WireGuard
  Reconfigure Traefik
  Install/check CasaOS
  Show status & URLs
  Exit
```

- **Full setup** — fill in parameters for AdGuard, WireGuard, and Traefik
  (domain, VPN subnet, admin credentials, etc.), then writes
  `~/.nullwatch/config.yaml`, generates compose files, brings the stack up,
  applies wiring, and installs CasaOS if it isn't already there. This is
  what you pick the first time; picking it again reconfigures everything at
  once with your current values pre-filled.
- **Reconfigure AdGuard / WireGuard / Traefik** — edit just that module's
  parameters and re-applies only that one container. `docker compose up -d`
  is idempotent, so nothing restarts unless something actually changed.
- **Manual edits:** `~/.nullwatch/config.yaml` is the source of truth and
  is meant to be hand-editable. Edit it directly, then re-run `nullwatch` —
  the next action you take reconciles against whatever's in the file rather
  than overwriting your changes.
- After any setup or reconfigure action, it prints the same quick links plus
  next steps (point your domain's DNS at the server, open the right
  firewall ports, log into AdGuard/WireGuard, add a VPN client) before
  dropping you back at the menu.

### On-disk layout

```
~/.nullwatch/
├── config.yaml           # source of truth — safe to hand-edit
├── compose/
│   ├── adguard.yml        # generated, human-readable
│   ├── wireguard.yml
│   └── traefik.yml
└── data/
    ├── adguard/           # AdGuardHome.yaml, work dir (persistent)
    ├── wireguard/         # wg-easy keys and peer configs
    └── traefik/           # acme.json, dynamic file config
```

Every generated compose file is runnable by hand:

```bash
docker compose -f ~/.nullwatch/compose/adguard.yml -p nullwatch-adguard up -d
```

## Repo structure

```
cmd/nullwatch/           entrypoint — the menu loop
internal/wizard/         huh forms — full-stack setup and per-module reconfigure
internal/config/         config.yaml schema, load/save, diff
internal/modules/        one package per module (adguard, wireguard, traefik)
internal/compose/        template rendering + docker compose shell-out
internal/wiring/         cross-module automation (DNS rewrites, DNS push)
internal/orchestrator/   applies desired config — full stack or a single module
internal/casaos/         installs CasaOS via its official script
internal/status/         computes service URLs shown in the menu
internal/preflight/      checks/installs docker + compose plugin at startup
templates/                embedded docker-compose templates
```
