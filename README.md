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

An interactive CLI that provisions the infrastructure layer of a self-hosted
private cloud: **AdGuard Home** (DNS + tracker/ad blocking), **WireGuard**
(full-tunnel VPN), and **Traefik** (reverse proxy) — wired together
automatically, on your own VPS, with no third-party accounts, relays, or
telemetry involved.

It's a single static Go binary. No dashboard, no daemon — you run it, answer
a few prompts, and it exits. Everything it generates (config, compose files)
is plain text you can read, edit by hand, and version-control yourself.

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

nullwatch provisions exactly three things — AdGuard, WireGuard, and Traefik —
as plain Docker Compose services on a Linux host. They're mandatory, not a
menu: this tool exists specifically to stand up that infrastructure layer,
so there's no picker asking whether you want DNS or a VPN. What you do
configure is how they're set up — domain, VPN subnet, admin credentials,
ports.

Anything beyond that infrastructure layer (Nextcloud, Jellyfin, and every
other app you'd actually use day to day) is a one-click install away in
[CasaOS](https://casaos.io)'s app store — nullwatch doesn't duplicate that,
and isn't a dashboard or day-to-day management UI either. CasaOS
auto-detects running containers on the same Docker daemon and surfaces them
on its dashboard with no explicit integration step needed.

## Modules

| Module | Image | Purpose |
|---|---|---|
| **adguard** | `adguard/adguardhome` | DNS resolver for the stack, with curated tracker/analytics/ad blocklists. Preseeded on first boot (admin credentials, filters) so it skips AdGuard's interactive setup wizard and its REST API is usable immediately. |
| **wireguard** | `ghcr.io/wg-easy/wg-easy` | Full-tunnel WireGuard VPN server with a small web UI for managing peers and generating QR codes / client configs. |
| **traefik** | `traefik` | Reverse proxy for `*.yourdomain.com`, routing to backend containers via Docker label discovery, with Let's Encrypt handled automatically. |

### Cross-module wiring

Since all three are always present, their wiring is always applied too, no
conditionals to think about:

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

Run it and answer the prompts:

```bash
nullwatch
```

- **First run:** fill in parameters for AdGuard, WireGuard, and Traefik
  (domain, VPN subnet, admin credentials, etc.) — all three are enabled by
  default, no picker involved. nullwatch writes `~/.nullwatch/config.yaml`,
  generates compose files, brings the stack up, and applies wiring.
- **Later runs:** the same wizard opens with your current values pre-filled.
  Change a parameter and nullwatch reconciles: `docker compose up -d` is
  idempotent, so re-running with no changes restarts nothing.
- **Manual edits:** `~/.nullwatch/config.yaml` is the source of truth and
  is meant to be hand-editable. Edit it directly, then re-run `nullwatch` —
  it reconciles against whatever's in the file rather than overwriting your
  changes.

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
cmd/nullwatch/           entrypoint
internal/wizard/         huh forms — per-module config (adguard/wireguard/traefik are mandatory)
internal/config/         config.yaml schema, load/save, diff
internal/modules/        one package per module (adguard, wireguard, traefik)
internal/compose/        template rendering + docker compose shell-out
internal/wiring/         cross-module automation (DNS rewrites, DNS push)
internal/orchestrator/   reconciles desired config against running state
templates/                embedded docker-compose templates
```
