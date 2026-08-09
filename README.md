<div align="center">

# TyxNet

**Self-hosted device enrollment, management, and experimental private networking.**

[![Release](https://img.shields.io/github/v/release/fbeser/tyxnet?display_name=tag&sort=semver)](https://github.com/fbeser/tyxnet/releases/latest)
[![CI](https://github.com/fbeser/tyxnet/actions/workflows/ci.yml/badge.svg)](https://github.com/fbeser/tyxnet/actions/workflows/ci.yml)
[![Container](https://img.shields.io/badge/GHCR-amd64%20%7C%20arm64-2496ED?logo=docker&logoColor=white)](https://github.com/fbeser/tyxnet/pkgs/container/tyxnet)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Platforms](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-555)](#platform-support)
[![License](https://img.shields.io/github/license/fbeser/tyxnet)](LICENSE)

[Quick Start](#quick-start) · [Downloads](https://github.com/fbeser/tyxnet/releases/latest) · [Documentation](#documentation) · [Security](SECURITY.md) · [Roadmap](docs/roadmap.md)

</div>

TyxNet is an open-source control plane for enrolling and managing devices behind
NAT or CGNAT. Devices initiate outbound connections to a central server, receive
private IPv4 identities, and appear in a role-aware web console.

> [!IMPORTANT]
> Encrypted central UDP forwarding is implemented for experimental virtual-IP
> traffic. TyxNet is still an experimental development release, not a
> production-ready or independently audited VPN replacement.

> [!WARNING]
> The protocol has not undergone an independent security audit. Do not expose a
> plaintext deployment to the public internet. Use TLS or a trusted HTTPS reverse
> proxy and obtain an independent review before production use.

## Quick start

### Raspberry Pi or Linux with Docker

This is the easiest server installation. One Compose service and one container
run TyxNet and Caddy together, provide trusted-LAN access on TCP 8443, and enable
public HTTPS with either a domain or a static public IP. Raspberry Pi does
**not** compile the Go project locally.

Requirements: 64-bit Linux, Docker Engine, Docker Compose v2 or legacy Compose
v1.29, and `/dev/net/tun`. Raspberry Pi OS should report `aarch64` from
`uname -m`.

Download the Compose file and environment template:

```bash
mkdir -p tyxnet && cd tyxnet
curl -fLO https://raw.githubusercontent.com/fbeser/tyxnet/main/docker-compose.yml
curl -fLo .env https://raw.githubusercontent.com/fbeser/tyxnet/main/.env.example
nano .env
```

For HTTPS, set exactly one public address in `.env`:

```dotenv
# Public IP without a domain
TYXNET_PUBLIC_IP=203.0.113.10
TYXNET_DOMAIN=

# Or a domain whose A/AAAA record points to this server
TYXNET_PUBLIC_IP=
TYXNET_DOMAIN=vpn.example.com
```

For a trusted-LAN-only installation without HTTPS, leave both values empty:

```dotenv
TYXNET_PUBLIC_IP=
TYXNET_DOMAIN=
```

The server remains available at `http://RASPBERRY_PI_OR_SERVER_IP:8443` and
Caddy starts with a private no-op configuration instead of entering a restart
loop. Do not forward TCP 8443, 18080, or 18443 from the modem in this mode.

Leave the remaining port values unchanged unless they conflict with another
service. `TYXNET_VERSION=latest` follows new releases; use a version such as
`0.3.14` to pin the deployment.

Start everything with current Compose:

```bash
sudo docker compose pull
sudo docker compose up -d
sudo docker compose ps
```

On older Raspberry Pi installations, the same file works with legacy Compose:

```bash
sudo docker-compose pull
sudo docker-compose up -d
sudo docker-compose ps
```

If `docker compose version` fails but `docker-compose --version` succeeds, use
the second command form consistently. Compose stores the database and Caddy
certificates in persistent Docker volumes and starts the container after a host
reboot through `restart: unless-stopped`.

For initial setup on the trusted LAN, open:

```text
http://RASPBERRY_PI_OR_SERVER_IP:8443
```

The first page creates the initial administrator. The dashboard hides **Run at
startup** inside containers because Compose already handles startup; systemd is
neither installed nor required there.

The container entrypoint supervises both processes. It forwards Docker stop
signals to TyxNet and Caddy and exits unsuccessfully if either process stops
unexpectedly, allowing `restart: unless-stopped` to recover the complete stack.

### Public HTTPS and modem forwarding

Caddy automatically obtains and renews the certificate. For a domain, forward
the standard public ports to the configured host ports. With the default `.env`:

```text
WAN TCP 80    -> RASPBERRY_PI_LAN_IP TCP 18080
WAN TCP 443   -> RASPBERRY_PI_LAN_IP TCP 18443
WAN TCP 18443 -> RASPBERRY_PI_LAN_IP TCP 18443
WAN UDP 51830 -> RASPBERRY_PI_LAN_IP UDP 51830
```

Public-IP certificates are short-lived Let's Encrypt certificates. At least one
standard ACME validation route, WAN TCP 443 or WAN TCP 80, must reach Caddy.
The TCP 18443 rule is the user-facing HTTPS service and UDP 51830 carries the
encrypted TyxNet tunnel.

Use one of these URLs for the dashboard and every client:

```text
https://PUBLIC_IP:18443
https://vpn.example.com:18443
```

If the domain is served directly on host TCP 443, set `TYXNET_HTTPS_PORT=443`
and use `https://vpn.example.com` without a port suffix. Public-IP HTTPS was
verified on Raspberry Pi 5 with Debian 12, Docker 20.10, and legacy Compose
1.29. Its certificate is valid for about six days and Caddy renews it
automatically.

Check the server, proxy, certificate, and tunnel:

```bash
sudo docker-compose ps
sudo docker-compose logs -f tyxnet-server
openssl s_client -connect 127.0.0.1:18443 -servername PUBLIC_IP -verify_return_error </dev/null
curl --resolve PUBLIC_IP:18443:127.0.0.1 https://PUBLIC_IP:18443/
```

If these checks pass but the public URL does not open, test from mobile data
with Wi-Fi disabled. Many home routers do not support NAT loopback. Confirm that
the modem forwards TCP `18443` to the Raspberry Pi's active Ethernet address,
not a second Wi-Fi address. During the external test,
`sudo tcpdump -ni any 'tcp port 18443'` shows whether the connection reaches the
host: no incoming SYN means the problem is still in the modem, ISP firewall or
CGNAT path.

#### CasaOS: import as one application

CasaOS must receive the complete `docker-compose.yml` in one import. Open
**App Store → Custom Install → Import**, paste or upload the whole Compose file,
and verify that the preview contains only the `tyxnet-server` service before
installing. The single container starts TyxNet first, waits for its local HTTP
listener, and then starts Caddy.

In the imported service environment, set one address for HTTPS:

```text
TYXNET_PUBLIC_IP=203.0.113.10
TYXNET_DOMAIN=
```

For a domain, leave `TYXNET_PUBLIC_IP` empty and set `TYXNET_DOMAIN` instead.
For LAN-only HTTP, leave both values empty; the application remains healthy and
the dashboard is available on the configured `TYXNET_LAN_PORT`.
The supervisor entrypoint is stored inside the TyxNet image, so CasaOS cannot
expand its shell variables while parsing Compose. Caddy proxies to
`127.0.0.1:8443` inside the same container; Docker DNS and container names are
not involved.

CasaOS can customize `TYXNET_VERSION`, LAN/tunnel/HTTPS host ports, and volume
names while importing. Only `TYXNET_DOMAIN` and `TYXNET_PUBLIC_IP` are passed to
the container. Unrelated host variables such as `OPENAI_API_KEY` are not copied
into it.

The default volume names deliberately match earlier installs created with
project name `tyxnet`:

```text
tyxnet_tyxnet-data
tyxnet_caddy-data
tyxnet_caddy-config
```

Before migration, identify the actual names with
`docker volume ls --format '{{.Name}}'`. If they differ, set
`TYXNET_DATA_VOLUME`, `TYXNET_CADDY_DATA_VOLUME`, and
`TYXNET_CADDY_CONFIG_VOLUME` to those existing names during import. Stop the old
application only after confirming the names. Never use `down -v`, delete a
volume, or create an empty replacement volume during migration.

When upgrading from the previous two-container Compose layout, use the old
Compose file once to stop its containers without deleting volumes, then replace
the file and start the new layout:

```bash
sudo docker-compose -p tyxnet down
curl -fLo docker-compose.yml \
  https://raw.githubusercontent.com/fbeser/tyxnet/main/docker-compose.yml
sudo docker-compose -p tyxnet pull
sudo docker-compose -p tyxnet up -d
```

Do not add `-v`. For a CasaOS migration, stop the old application without its
**Delete data** option, confirm the three named volumes still exist, and import
the new Compose file as one application. After startup, `docker-compose ps`
must show one container and `docker-compose logs -f tyxnet-server` must show both
TyxNet and Caddy startup messages.

Already enrolled clients keep their device identity; only change `server:` to
the new HTTPS URL and restart TyxNet. Windows stores this file at
`C:\ProgramData\TyxNet\client.yaml`; macOS stores it under
`~/Library/Application Support/TyxNet/client.yaml`. No new enrollment token is
required.

Update or stop the stack without deleting its data:

```bash
sudo docker-compose pull && sudo docker-compose up -d
sudo docker-compose down
```

Never add `-v` to `down` during updates or HTTPS migration. It deletes the
TyxNet database and Caddy certificate state. Compose v2 users can replace
`docker-compose` with `docker compose`. The published multiarch image is
`ghcr.io/fbeser/tyxnet:latest` for `linux/amd64` and `linux/arm64`.

### macOS

1. Open the [latest release](https://github.com/fbeser/tyxnet/releases/latest).
2. Download `TyxNet-<version>-macos-universal.dmg`.
3. Drag **TyxNet Client** or **TyxNet Server** into Applications.
4. On first launch, right-click the app and select **Open**.
5. Approve the administrator prompt required for the native tunnel process.

The universal DMG supports Intel and Apple Silicon. Client and Server run as
menu-bar apps; configuration is stored under
`~/Library/Application Support/TyxNet` and logs under `~/Library/Logs/TyxNet`.

The current apps are ad-hoc signed but not Apple-notarized. Gatekeeper may show
an unsigned developer warning. Disable **Run at startup** before deleting the app
to remove its LaunchDaemon and LaunchAgent registrations.

### Windows

1. Open the [latest release](https://github.com/fbeser/tyxnet/releases/latest).
2. Download `TyxNet-<version>-windows-amd64.msi`.
3. Run the installer and approve the UAC prompt.
4. Launch **TyxNet Client** or **TyxNet Server** from the Start menu.

The x64 MSI installs the server, client, tray companions, `tyxnetctl`, and the
verified Wintun 0.14.1 runtime under `Program Files\TyxNet`. Mutable data and logs
are stored under `ProgramData\TyxNet`.

The MSI is not Authenticode-signed, so Windows SmartScreen may warn on first
launch. Windows ARM64 binaries are available as standalone `.exe` files in the
release; an ARM64 MSI is not available yet.

### Native Linux server

Download the binary matching the host architecture from the
[latest release](https://github.com/fbeser/tyxnet/releases/latest):

```bash
# Raspberry Pi 5 / Linux ARM64
curl -fLO https://github.com/fbeser/tyxnet/releases/download/v0.3.14/tyxnet-server-linux-arm64
curl -fLO https://github.com/fbeser/tyxnet/releases/download/v0.3.14/checksums.txt
grep 'tyxnet-server-linux-arm64$' checksums.txt | sha256sum -c -
chmod +x tyxnet-server-linux-arm64

sudo ./tyxnet-server-linux-arm64 install \
  --listen-address 0.0.0.0 \
  --api-port 8443 \
  --tunnel-port 51830 \
  --network 10.90.0.0/24
```

For x64 Linux, replace `arm64` with `amd64`. The install command copies the
binary to `/usr/local/bin/tyxnet-server`, writes `/etc/tyxnet/server.yaml`,
creates `/var/lib/tyxnet`, installs a systemd unit, and starts it.

```bash
sudo systemctl status tyxnet-server
sudo journalctl -u tyxnet-server -f
```

## What works today

| Area | Status |
|---|---|
| First-admin setup and authenticated web console | Working |
| Users, roles, sessions, enrollment tokens, devices, and audit events | Working |
| Ed25519 enrollment and challenge-authenticated control connection | Working |
| Reconnect, heartbeat, device presence, and role-scoped views | Working |
| Network-flow topology and 60-second traffic dashboard | Working |
| Linux TUN, Windows Wintun, and macOS utun adapter creation | Experimental |
| Windows tray and macOS menu-bar companions | Experimental |
| Native Linux systemd installation | Working |
| Multiarch Docker image and Compose deployment | Working |
| Encrypted UDP packet data plane and virtual-IP traffic | Experimental |
| Authenticated remote reconnect, restart, shutdown, and result reporting | Working |
| Signed/notarized desktop distribution | Not complete |
| Mobile clients, P2P, STUN, and hole punching | Planned |

With HTTPS control and UDP `51830` reachable, traffic such as
`ping 10.90.0.1` or `ssh user@10.90.0.4` crosses the central encrypted data
plane. Host firewalls and the destination service must still allow that traffic.

## Features

- Central self-hosted server with SQLite persistence and append-only migrations
- Browser-based server management and client enrollment/status consoles
- Users, RBAC roles, enrollment tokens, sessions, commands, and audit records
- Ed25519 device identity and challenge authentication
- HKDF-SHA256, ChaCha20-Poly1305, directional keys, and replay protection
- Argon2id passwords and one-way hashes for random tokens
- Virtual source-IP validation and central routing policy
- Live network topology, stable/filterable protocol and port flows, per-flow Mbps, packet totals, and 60-second throughput charts
- Windows Wintun, Linux TUN, and macOS utun platform adapters
- Windows notification-area and macOS menu-bar companions
- Strict enum-based remote command allowlist with no server-provided shell text
- Linux systemd, Docker Compose, Windows MSI, and universal macOS DMG packaging
- Cross-builds and release checksums for Linux, Windows, and macOS

## Architecture

```mermaid
flowchart LR
  LC[Linux client] -->|TLS control| S[TyxNet Server]
  WC[Windows client] -->|TLS control| S
  MC[macOS client] -->|TLS control| S
  UI[Web console / tyxnetctl] -->|HTTPS API| S
  S --> DB[(SQLite)]
  LC -->|Encrypted UDP packets| S
  WC -->|Encrypted UDP packets| S
  MC -->|Encrypted UDP packets| S
```

TyxNet uses a central star topology. Clients connect outbound through NAT or
CGNAT; no inbound client port forwarding is required. TyxNet does not use
WireGuard and does not route general internet traffic through the tunnel by
default. See [Architecture](docs/architecture.md) and
[Protocol](docs/protocol.md).

## Platform support

| Platform | Architectures | Server adapter | Client adapter | Desktop UI | Package |
|---|---|---:|---:|---:|---|
| Linux | amd64, arm64 | TUN | TUN | Web | Binary / Docker |
| macOS | Intel, Apple Silicon | `utunN` | `utunN` | Web + menu bar | Universal DMG |
| Windows | amd64, arm64 | Wintun | Wintun | Web + tray | x64 MSI / binaries |
| Android / iOS | Planned | — | — | — | — |

## Server setup

### First administrator

For trusted-LAN installs, open `http://SERVER_IP:8443` and complete the setup
form. To create the administrator from the CLI without placing the password in
process arguments:

The setup card, login card, and management sidebar display the running TyxNet
version so operators can confirm the active build before creating the first
administrator.

```bash
printf '%s\n' 'a-strong-password' | sudo tyxnet-server admin create \
  --config /etc/tyxnet/server.yaml \
  --username admin \
  --password-stdin
```

### Enrollment token

```bash
sudo tyxnet-server token create \
  --config /etc/tyxnet/server.yaml \
  --user admin \
  --expires 24h \
  --max-uses 1
```

The web console can create and revoke enrollment tokens without using the CLI.

## Client setup

### Linux client

Download `tyxnet-client-linux-amd64` or `tyxnet-client-linux-arm64` from the
release, make it executable, then enroll and install it:

```bash
chmod +x tyxnet-client-linux-arm64
sudo ./tyxnet-client-linux-arm64 install \
  --server https://vpn.example.com \
  --token TYX-EXAMPLE \
  --name workshop-pc
```

The command stores the client identity with owner-only permissions and creates a
systemd service.

### macOS client

Install **TyxNet Client.app** from the universal DMG, open the menu-bar icon, and
select **Open Web Console**. Enter the server URL, device name, and enrollment
token. The local UI is available at `http://127.0.0.1:9070`. **Leave server**
removes the saved server URL, enrollment identity, active tunnel, and local
management session so the client can enroll again. It is restricted to a
loopback browser request and does not delete the server-side device record;
revoke that old record separately from the server console.

macOS packet routing remains experimental and a production Network Extension is
not implemented. See [macOS client notes](docs/macos-client.md).

### Windows client

Install the x64 MSI, launch **TyxNet Client**, and use its tray menu to open the
local enrollment console. The launcher requests administrator access and uses
the bundled Wintun runtime. The local UI is available at
`http://127.0.0.1:9070` and provides the same local-only **Leave server** reset.

See [Windows client notes](docs/windows-client.md).

## Web console and roles

The server dashboard provides setup, login, overview counters, device rename and
revoke, persistent virtual-IP assignment, command actions, user and role
management, administrator password resets, token management, command history,
server settings, network-flow telemetry, and audit logs. The flow panel shows
device-to-device direction, TCP/UDP ports or ICMP metadata, stable sorting,
filters, expandable details, current Mbps, packet totals, and a 60-second chart
from successfully routed virtual-IP packets. Administrators can optionally
enable persistent flow history and set a 1–10240 MB logical metadata budget;
recording is off by default, the setting survives restart, and the oldest saved
rows are removed when the selected budget is reached. Saved history has endpoint,
protocol, date/time, transfer, and packet filters plus an administrator-only
delete-all action. For example, the additive live flow API
metadata includes:

```json
{"source":"10.90.0.2","destination":"10.90.0.3","protocol":"tcp","protocol_number":6,"source_port":52000,"destination_port":22}
```

The authenticated API can enable a 100 MB history budget and query a UTC time
range without exposing packet contents:

```bash
curl -X PATCH https://vpn.example.com/api/v1/server/settings \
  -H "Authorization: Bearer $TYXNET_ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"flow_history_enabled":true,"flow_history_limit_mb":100}'

curl -G https://vpn.example.com/api/v1/network/flows/history \
  -H "Authorization: Bearer $TYXNET_ACCESS_TOKEN" \
  --data-urlencode 'protocol=tcp' \
  --data-urlencode 'from=2026-08-09T09:00:00Z' \
  --data-urlencode 'to=2026-08-09T10:00:00Z'
```

Resetting a password signs out
every existing session for that user; the new password must contain at least 12
characters. **Remember me** creates a 30-day session only over HTTPS.

| Role | Devices visible to clients | Client controls |
|---|---|---|
| Admin | All devices | Reconnect, restart, shutdown |
| Operator | All devices | Reconnect, restart, shutdown |
| Viewer | All devices | View only |
| Member | Devices owned by that user | View only |

Admins, operators, and viewers can view live and saved flow metadata. Members
cannot access the cross-device flow panel. Traffic payloads, DNS names, and
application-protocol contents are not stored. Live IP protocol and port/ICMP
metadata remains in memory for 60 seconds. When an administrator enables flow
history, one-second aggregates of that same metadata are persisted to SQLite
until deleted or aged out by the configured logical metadata budget.

Reconnect, restart, and shutdown commands are delivered over the authenticated
control stream and tracked as queued, delivered, accepted, succeeded, failed, or
expired. Clients accept only fixed allowlisted actions: Windows uses
`shutdown.exe`, Linux uses `systemctl`, and macOS uses `/sbin/shutdown`. The
client service must run with the operating-system privileges required by those
commands. No server-provided text is passed to a shell.

## CLI

```bash
tyxnetctl --server https://vpn.example.com login \
  --username admin --password '...'

export TYXNET_SERVER=https://vpn.example.com
export TYXNET_ACCESS_TOKEN=TYX-...

tyxnetctl devices list
tyxnetctl users list
tyxnetctl tokens list
```

A password supplied on the command line may be visible in the process list. Use
a secure secret source or the API for automation.

## Configuration

Example YAML files are under [`configs/`](configs/). The server validates
addresses, ports, CIDRs, TLS paths, and plaintext-bind opt-in at startup.

Important locations:

| Installation | Configuration | Data / identity | Logs |
|---|---|---|---|
| Docker server | Image default or mounted YAML | `tyxnet-data` volume | `docker compose logs` |
| Native Linux server | `/etc/tyxnet/server.yaml` | `/var/lib/tyxnet` | systemd journal |
| Windows MSI | `ProgramData\TyxNet` | `ProgramData\TyxNet` | `ProgramData\TyxNet\logs` |
| macOS app | `~/Library/Application Support/TyxNet` | Same directory | `~/Library/Logs/TyxNet` |

Private keys and tokens are never written into server configuration YAML.

## Ports

| Port | Protocol | Purpose |
|---:|---|---|
| 443 | TCP/UDP | Public HTTPS or ACME validation through Caddy |
| 18443 | TCP | Optional public-IP HTTPS endpoint mapped to Caddy |
| 8443 | TCP | Direct/LAN dashboard, enrollment, and control APIs |
| 51830 | UDP | Encrypted virtual-IP packet tunnel |
| 9070 | TCP | Local client enrollment and status UI |

Allow only the required ports from intended networks. Never expose TCP 9070 to
the public internet. The default Docker Compose file publishes TCP 8443 to the
LAN for first setup and should be used only on a trusted network until TLS is
configured.

Clients initiate UDP keepalives, so no client-side port forwarding is needed.
The server-side modem/firewall must forward UDP `51830` to the TyxNet server.

## Operations

### Docker

```bash
sudo docker compose ps
sudo docker compose logs -f tyxnet-server
sudo docker compose restart tyxnet-server
sudo docker compose pull && sudo docker compose up -d
```

### Native Linux

```bash
sudo systemctl status tyxnet-server
sudo systemctl restart tyxnet-server
sudo journalctl -u tyxnet-server -f
```

Uninstall removes the native binary, unit, and configuration but intentionally
preserves server data for recovery.

## Build from source

Requirements: Go 1.25 or newer. Desktop tray builds also require the native
Windows or macOS toolchain.

```bash
git clone https://github.com/fbeser/tyxnet.git
cd tyxnet
make build
make test
```

Useful targets:

| Command | Purpose |
|---|---|
| `make build` | Build server, client, trays, and CLI for the host |
| `make test` | Run unit and integration tests |
| `make test-race` | Run the Go race detector |
| `make vet` | Run `go vet` |
| `make lint` | Run `golangci-lint` |
| `make release` | Cross-build release binaries and checksums |
| `make release-full` | Build all release binaries plus macOS DMG and Windows MSI |
| `make package-macos` | Build the universal DMG on macOS |
| `make package-windows` | Build the x64 MSI with `wixl` |

Release targets accept `VERSION`, for example:

```bash
make release-full VERSION=0.3.14
```

## Release verification

Every GitHub release includes `checksums.txt`:

```bash
grep 'tyxnet-server-linux-arm64$' checksums.txt | sha256sum -c -
```

Release binaries and GHCR images are built and published from the local release
machine; GitHub Actions is not used for release publishing. Versioned and
`latest` container tags are published for `linux/amd64` and `linux/arm64`.

## Documentation

| Topic | Document |
|---|---|
| Architecture | [docs/architecture.md](docs/architecture.md) |
| Protocol | [docs/protocol.md](docs/protocol.md) |
| Security model | [docs/security-model.md](docs/security-model.md) |
| Server installation | [docs/server-installation.md](docs/server-installation.md) |
| Linux client | [docs/linux-client.md](docs/linux-client.md) |
| macOS client | [docs/macos-client.md](docs/macos-client.md) |
| Windows client | [docs/windows-client.md](docs/windows-client.md) |
| OpenAPI | [docs/openapi.yaml](docs/openapi.yaml) |
| Troubleshooting | [docs/troubleshooting.md](docs/troubleshooting.md) |
| Roadmap | [docs/roadmap.md](docs/roadmap.md) |
| Development | [docs/development.md](docs/development.md) |

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). Security
issues must be reported privately according to [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
