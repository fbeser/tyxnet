# Roadmap

## Phase 1 — foundation

Repository, server/client/config/SQLite models, enrollment, basic API/CLI and
in-memory tunnel testing. Mostly implemented.

## Phase 2 — Linux data plane

Connect the configured Linux TUN to the central UDP handshake/tunnel, virtual-IP
routing, encrypted sessions, keepalive/rekey/reconnect and native installers.

## Phase 3 — Windows

Connect the existing server Wintun adapter to the data plane, then add client
integration, Windows Service, route/firewall management and signed setup. The
system-tray companion and stable Wintun/NLA identity are implemented.

## Phase 3b — macOS

Connect the server utun adapter to the data plane, then add a production Network
Extension client, Keychain-backed identity, LaunchDaemon,
route management, and signed/notarized packaging. The development menu-bar
companion is implemented.

## Phase 4 — management

Server management console, client role-scoped device panel, and bootstrap are
implemented. Remaining work: richer RBAC/audit search and safe command
delivery/results.

## Phase 5 — scale

Updates, ACLs, subnet router, exit node, multiple networks, HA, relay scaling and
Prometheus metrics.

## Phase 6 — mobile

Android app/VpnService, token or QR enrollment, device/admin views, notifications,
then iOS evaluation. macOS networking work is tracked separately in Phase 3b.
P2P, STUN and UDP hole punching may be evaluated later and
are not part of the first version.
