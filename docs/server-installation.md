# Server installation

Use the README Docker or native instructions. A non-loopback bind requires both
`tls_cert`/`tls_key` unless `allow_insecure_http` is explicitly set for a trusted
reverse-proxy network. Allow TCP 8443 and UDP 51830 only,
create the first admin through stdin, back up `/var/lib/tyxnet`, and review systemd
hardening. `uninstall` removes binary/unit/config but deliberately preserves data.

The server enables its virtual adapter by default. Windows uses a same-name
Wintun adapter, Linux opens the configured TUN name, and macOS receives an
available `utunN` name from the kernel. Repeated starts reuse the Windows adapter
and idempotently replace Linux addressing; macOS utun devices are process-scoped
and receive a kernel-selected number each time. Set `tunnel_enabled: false` only
for control-plane-only development. Adapters exchange virtual-IP traffic through
the central encrypted UDP data plane when HTTPS control and UDP 51830 are
reachable.

For Docker on amd64 or arm64, download `docker-compose.yml` and `.env.example`,
save the latter as `.env`, set either `TYXNET_DOMAIN` or `TYXNET_PUBLIC_IP`, and
run `docker compose up -d`. The single legacy-Compose-compatible stack pulls the
published GHCR image, publishes TCP 8443 for trusted-LAN setup and UDP 51830 for
the tunnel, and starts Caddy for public HTTPS. Caddy generates its configuration
from the selected address and automatically renews domain certificates or
Let's Encrypt short-lived public-IP certificates.

The default host ports are TCP 18080 for ACME HTTP validation and TCP/UDP 18443
for HTTPS. Forward WAN TCP 443 to host TCP 18443 for TLS-ALPN-01 validation, or
WAN TCP 80 to host TCP 18080 for HTTP-01. Also forward WAN TCP 18443 for user
access and WAN UDP 51830 for tunneled traffic. Compose restart policies start
both services after host reboots, so the web console hides **Run at startup** in
containers and does not invoke systemd. Keep the Compose project directory
stable and never use `down -v` during updates because the volumes contain the
database and certificate state.

For a headless host on a trusted LAN, `tyxnet-server run` listens on
`0.0.0.0:8443` by default and enables remote first-admin setup. Open
`http://<SERVER-LAN-IP>:8443` from an administrator workstation. This shortcut
explicitly permits plaintext HTTP and should never be used on an untrusted or
public network; configure certificates for those deployments. Pass
`--local-web` to bind the console to `127.0.0.1` and disable remote setup.

The administrator-only **Run at startup** switch uses an elevated Scheduled Task
on Windows, systemd on Linux, and a LaunchDaemon plus menu-bar LaunchAgent on
macOS. Registration occurs while the current process is already privileged, so
Windows does not display a UAC prompt on later sign-ins. Clearing the switch
removes the registration without stopping the current process.

`ping_interval` controls the default device heartbeat frequency and accepts
durations such as `25s` or `1m`. Administrators can change the live value from
the Overview page; web changes are stored in SQLite and take precedence after a
restart.

Administrators can assign a persistent virtual IPv4 address from the Devices
page. The address must be inside the configured TyxNet network, cannot use the
network, server, or broadcast address, and cannot already belong to another
device. A connected client saves the new address when it next establishes its
control connection.
