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

Turn a bare Linux VPS into your own private cloud and internet gateway — your
DNS lookups, your VPN traffic, and your data never have to touch a big tech
company's infrastructure to work.

nullwatch is a single Go binary with an interactive menu. Run it, answer a
few questions, and it stands up **AdGuard Home** (your own DNS resolver with
tracker/ad blocking), **WireGuard** (your own VPN), and **Traefik** (reverse
proxy) on your server — wired together automatically, locked down so nothing
but the VPN is reachable from the internet, and left running as plain Docker
containers you can inspect and manage by hand at any time.

## Contents

- [Quick start](#quick-start)
- [What you get](#what-you-get)
- [Using nullwatch](#using-nullwatch)
- [FAQ](#faq)
- [Security posture](#security-posture)
- [Advanced / reference](#advanced--reference)

## Quick start

All you need is a Linux VPS — nullwatch installs Docker and Go itself if
they're missing.

```bash
curl -fsSL https://raw.githubusercontent.com/archit3ckt/nullwatch/main/install.sh | sh
```

This is a shell script that installs system packages — read it first, same
as you'd do with any curl-installer before piping it into a shell. It builds
`nullwatch`, installs it to `/usr/local/bin`, and drops you straight into the
setup menu.

From there:

1. Pick **Full setup** and answer the prompts (your domain, a VPN subnet,
   admin passwords). A couple of minutes later you'll have AdGuard,
   WireGuard, and Traefik running, plus CasaOS installed as your day-to-day
   dashboard.
2. Pick **Add WireGuard peer** to create your first VPN client — scan the QR
   code it prints with the [WireGuard app](https://www.wireguard.com/install/)
   and connect.
3. Pick **Lock down firewall** to make sure nothing but the VPN tunnel is
   reachable from the public internet.

Prefer to build it yourself instead of the install script?

```bash
git clone https://github.com/archit3ckt/nullwatch.git
cd nullwatch
go build -o nullwatch ./cmd/nullwatch
sudo mv nullwatch /usr/local/bin/
```

## What you get

| | |
|---|---|
| **AdGuard Home** | Your own DNS resolver, with tracker/analytics/ad blocklists — DNS-level privacy, not just ad-blocking. |
| **WireGuard** (via wg-easy) | A full-tunnel VPN. Once it's set up, it's the only way in or out — your traffic and DNS lookups never leak to your ISP or anyone else. |
| **Traefik** | A reverse proxy for anything you host at `*.yourdomain.com`. |
| **CasaOS** | The dashboard and one-click app store for everything else you'd actually use day to day — Nextcloud, Jellyfin, whatever you want next. |

AdGuard, WireGuard, and Traefik are always installed together — that's the
whole point of this tool, and there's no picker asking which you want.
CasaOS is where you add optional apps afterward; nullwatch installs it and
then gets out of the way, never touching its config, its app store, or its
containers again.

Nothing here needs a third-party account, a hosted control plane, or any
telemetry to work. The config file and every generated Docker Compose file
are plain, human-readable text you can inspect or hand-edit — nothing about
this stack is hidden from you.

## Using nullwatch

Run it any time you want to check on or change something:

```bash
nullwatch
```

Every run shows the banner, live links to each service (once you've set up
at least once), and the same menu:

```
Quick links (only reachable once connected to the VPN):
  AdGuard Home:      http://203.0.113.5:3000
  WireGuard admin:   http://203.0.113.5:51821
  CasaOS:            http://203.0.113.5:81

What do you want to do?
> Full setup (AdGuard + WireGuard + Traefik + CasaOS)
  Reconfigure AdGuard
  Reconfigure WireGuard
  Reconfigure Traefik
  Install/check CasaOS
  Add WireGuard peer
  Lock down firewall (VPN-only access)
  Show status & URLs
  Uninstall
  Exit
```

- **Full setup** — configure and (re)apply everything at once. Safe to
  re-run any time; already-correct values just get reconfirmed.
- **Reconfigure AdGuard / WireGuard / Traefik** — change just that module's
  settings (a port, a password, the domain) and re-apply only that one
  container.
- **Add WireGuard peer** — creates a new VPN client and prints its config as
  both text and a scannable QR code, saved under
  `~/.nullwatch/data/wireguard/peers/`. This talks to wg-easy over
  `localhost` (nullwatch runs on the same server), so it works even before
  you're connected to the VPN — you don't need to reach wg-easy's own web UI
  at all.
- **Lock down firewall** — applies (or re-applies) the VPN-only lockdown
  described below. Shows you the exact rules and asks for confirmation
  first.
- **Uninstall** — walks through separate confirmations for stopping the
  containers, removing CasaOS, deleting your config/data, and removing the
  binary itself — declining one doesn't cascade into the next.
- **Manual edits** — `~/.nullwatch/config.yaml` is the source of truth and
  is meant to be hand-edited. Change it directly, then re-run `nullwatch`;
  it reconciles against whatever's in the file instead of overwriting your
  changes.

## FAQ

**What if I lose access or get locked out of something?**
SSH is always allowed through the firewall, no matter what — that's by
design. SSH in and run `nullwatch` again to fix or reconfigure anything.

**How do I add apps like Nextcloud or Jellyfin?**
Through CasaOS, once you're connected to the VPN, at `http://<your
server>:81`. That's CasaOS's job — nullwatch just makes sure the
infrastructure underneath it exists and stays locked down.

**Why does my browser warn me about an invalid certificate?**
Traefik uses a self-signed certificate. Nothing here is reachable from the
public internet, so Let's Encrypt has no way to validate a certificate for
it (that requires a publicly-reachable server to answer its challenge).
Trust the certificate once in your browser and you're set — see
[Security posture](#security-posture) for why this is the deliberate
tradeoff.

**Can I change settings after the initial setup?**
Yes — re-run `nullwatch`, pick the relevant "Reconfigure" option, and it
updates just that one container.

**Is it safe to run nullwatch again after everything's already set up?**
Yes. Every action is idempotent — re-running with nothing changed doesn't
restart or duplicate anything.

## Security posture

The only thing reachable from the public internet is the WireGuard tunnel
itself. Everything else — AdGuard, Traefik, WireGuard's own admin UI,
CasaOS, and anything you install through it later — is locked to the VPN
subnet and localhost. Connect to the VPN first; that's the only door in,
and it applies to whatever's running, not a fixed list of services. A
hand-maintained port list could never keep up with CasaOS's whole reason
for existing (one-click installs of apps that each open their own ports),
so instead of allowlisting ports, the entire VPN subnet is trusted for
every port.

This is enforced directly via `iptables`/`ip6tables` (not `ufw` — it failed
outright on at least one real deployment) across two separate chains,
because Docker deliberately bypasses a host's normal firewall rules for its
own published ports:

- **`INPUT`** governs native processes (sshd, CasaOS's own gateway
  service). SSH and the WireGuard port are allowed from anywhere; every
  other port is allowed only from the VPN subnet and localhost, then the
  default policy is set to deny.
- **`DOCKER-USER`** governs every Docker-published port (AdGuard, Traefik,
  WireGuard's admin UI, CasaOS's apps). Docker exposes published ports via
  its own NAT/forwarding rules *before* `INPUT` ever sees that traffic, so
  a restrictive `INPUT` policy alone has no effect on a container's port —
  full stop. `DOCKER-USER` is the chain Docker deliberately leaves empty
  for exactly this override.

Rules are applied idempotently and persisted across reboots (`INPUT` via
`iptables-persistent`; `DOCKER-USER` via a small systemd unit that
reapplies it right after Docker starts, since Docker recreates that chain
empty on every boot). `ufw`, if present, is purged first — leaving it
installed but unused can silently reset your `INPUT` policy back to
`ACCEPT`.

One consequence: since Traefik is never reachable from the public internet,
Let's Encrypt's HTTP-01 challenge can't complete, so Traefik uses its own
self-signed certificate instead. No ACME flow or third party is involved in
getting HTTPS working internally — just a one-time browser warning.

**References**, if you're adapting this for your own setup:
[Docker's docs on packet filtering and firewalls](https://docs.docker.com/engine/network/packet-filtering-firewalls/)
and [ufw-docker](https://github.com/chaifeng/ufw-docker), the reference
community tool this technique is based on (adapted here to trust the whole
VPN subnet rather than per-port rules).

## Advanced / reference

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
    ├── wireguard/         # wg-easy keys; peers/ has each client's saved .conf
    └── traefik/           # dynamic file config (self-signed TLS, no acme.json)
```

Every generated compose file is runnable by hand:

```bash
docker compose -f ~/.nullwatch/compose/adguard.yml -p nullwatch-adguard up -d
```

### Repo structure

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
internal/firewall/       locks the host to VPN-only access via iptables/ip6tables
templates/                embedded docker-compose templates
```
