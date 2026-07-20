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
- **Nothing but the WireGuard tunnel is exposed to the internet.** AdGuard,
  Traefik, Traefik's proxied sites, WireGuard's own admin UI, and CasaOS are
  all firewalled to the VPN subnet and localhost — connect to the VPN first,
  then everything else. DNS is pushed through AdGuard too, so lookups can't
  silently leak to your ISP or a third party either.
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
| **adguard** | `adguard/adguardhome` | DNS resolver for the stack, with curated tracker/analytics/ad blocklists. Configured on first boot via AdGuard's own install API (admin credentials, blocklists) so it skips the interactive setup wizard and its REST API is usable immediately — idempotent, so re-running just confirms it's already configured. |
| **wireguard** | `ghcr.io/wg-easy/wg-easy` | Full-tunnel WireGuard VPN server with a small web UI for managing peers and generating QR codes / client configs. |
| **traefik** | `traefik` | Reverse proxy for `*.yourdomain.com`, routing to backend containers via Docker label discovery. Uses its own self-signed TLS cert — see [Security posture](#security-posture) for why there's no Let's Encrypt here. |

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

## Security posture

The only thing meant to be reachable from the public internet is the
WireGuard tunnel itself. Everything else on this host is firewalled to the
WireGuard client subnet and localhost. Connect to the VPN first; that's the
only door in — and this applies to whatever is running, not a fixed list of
services.

That "whatever is running" part is deliberate: this isn't a per-port
allowlist naming AdGuard, Traefik, CasaOS, and so on. A hand-maintained port
list can never keep up with CasaOS's whole reason for existing — one-click
installs of apps (Nextcloud, Jellyfin, whatever you add next) that each open
their own ports nullwatch has no way to know about in advance. So instead of
allowlisting ports, the entire WireGuard subnet is trusted for every port.
Connected to the VPN, you can reach anything on the box; not connected, nothing but SSH and
the tunnel itself exists as far as the internet is concerned.

This talks to `iptables-legacy`/`ip6tables-legacy` directly rather than
going through `ufw`. `ufw` was the first approach here, but on at least one
real deployment (Debian trixie, `ufw` 0.36.2) it failed — a bare `"ERROR:
problem running"`, no further detail, no log output — on *every* new rule
addition, confirmed by comparing it directly against raw `iptables-legacy`
performing the identical operation, which worked. Since the layer
underneath `ufw` is what actually works, this talks to it directly instead
of fighting `ufw`'s own bug.

Two separate chains need rules, covering both IPv4 and IPv6 (Docker
publishes container ports on both by default) — from the menu's "Lock down
firewall" action (also offered automatically as part of full setup):

- **`INPUT`**, governing native host processes (sshd, CasaOS's own gateway
  service): SSH is allowed from anywhere, always, before anything else is
  touched — it's the one rule applied first, specifically so a
  misconfiguration can't lock you out of the box. The WireGuard UDP port is
  allowed from anywhere too, since it has to be for clients to connect in
  the first place (WireGuard silently drops unauthenticated packets rather
  than responding to them, so this doesn't expand the attack surface the
  way an open TCP port would). Every other port is allowed from the
  WireGuard subnet and localhost, then the default policy is set to `DROP`.
- **Docker's `DOCKER-USER` chain**, governing every Docker-published port
  (AdGuard, Traefik, WireGuard's admin UI, and anything installed later via
  CasaOS). Docker manipulates iptables' nat/FORWARD chains directly to
  expose published ports, and that happens *before* `INPUT`-chain rules
  ever see the traffic — so a restrictive `INPUT` policy has no effect on a
  container's published port, full stop. This is a well-documented
  Docker/`ufw` interaction (same root cause, whether you're using `ufw` or
  plain `iptables`), not a nullwatch-specific quirk. `DOCKER-USER` is the
  chain Docker deliberately leaves empty for exactly this purpose, evaluated
  before its own permissive rules. nullwatch inserts rules there — scoped to
  the public network interface, so container-to-container traffic on the
  internal Docker network is untouched — allowing only the WireGuard subnet
  and the tunnel port, and dropping everything else.

Rules are applied idempotently (checked before adding, so re-running
doesn't duplicate them) and persisted across reboots via
`iptables-persistent`/`netfilter-persistent`, installed automatically if
missing. Skipping the `DOCKER-USER` half and relying on `INPUT`-chain rules
alone (what `ufw` does by default) would leave every container's published
port reachable from the internet regardless of what the firewall's status
output claims — worth knowing if you ever adapt this for your own setup.

One consequence: since Traefik's ports are never reachable from the public
internet, Let's Encrypt's HTTP-01 challenge can't complete (it requires port
80 to be reachable from Let's Encrypt's own servers). Traefik falls back to
its own self-signed certificate instead — your browser will warn once until
you trust it, but no ACME flow or third party is involved in getting HTTPS
working internally.

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
  CasaOS:            http://203.0.113.5:81

What do you want to do?
> Full setup (AdGuard + WireGuard + Traefik + CasaOS)
  Reconfigure AdGuard
  Reconfigure WireGuard
  Reconfigure Traefik
  Install/check CasaOS
  Lock down firewall (VPN-only access)
  Show status & URLs
  Uninstall
  Exit
```

- **Full setup** — fill in parameters for AdGuard, WireGuard, and Traefik
  (domain, VPN subnet, admin credentials, etc.), then writes
  `~/.nullwatch/config.yaml`, generates compose files, brings the stack up,
  applies wiring, installs CasaOS if it isn't already there, and offers to
  lock down the firewall (see [Security posture](#security-posture)). This
  is what you pick the first time; picking it again reconfigures everything
  at once with your current values pre-filled.
- **Reconfigure AdGuard / WireGuard / Traefik** — edit just that module's
  parameters and re-applies only that one container. `docker compose up -d`
  is idempotent, so nothing restarts unless something actually changed.
- **Lock down firewall** — re-run the `ufw` lockdown any time on its own,
  e.g. after changing a port via a reconfigure action. Shows the exact rules
  before applying and asks for confirmation.
- **Uninstall** — a series of separate confirmations, each more destructive
  than the last, so declining one doesn't cascade into the next: stop and
  remove the AdGuard/WireGuard/Traefik containers and the shared Docker
  network; optionally uninstall CasaOS and every app it manages (via its own
  uninstaller); optionally delete `~/.nullwatch` (config and all persisted
  data — cannot be undone); optionally remove the `nullwatch` binary itself.
  It doesn't touch the firewall rules — `ufw disable` if you want those gone
  too.
- **Manual edits:** `~/.nullwatch/config.yaml` is the source of truth and
  is meant to be hand-editable. Edit it directly, then re-run `nullwatch` —
  the next action you take reconciles against whatever's in the file rather
  than overwriting your changes.
- After any setup or reconfigure action, it prints the same quick links plus
  next steps (connect to the VPN, log into the admin UIs, lock down the
  firewall if you haven't yet) before dropping you back at the menu.

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
    └── traefik/           # dynamic file config (self-signed TLS, no acme.json)
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
internal/casaos/         installs/uninstalls CasaOS via its official scripts
internal/status/         computes service URLs shown in the menu
internal/preflight/      checks/installs docker + compose plugin at startup
internal/firewall/       locks the host to VPN-only access via ufw
templates/                embedded docker-compose templates
```
