# Roadmap

## Phase 1 — foundation

Repository, server/client/config/SQLite models, enrollment, basic API/CLI and
in-memory tunnel testing. Mostly implemented.

## Phase 2 — Linux data plane

Complete Linux TUN configuration, central UDP handshake/tunnel, virtual-IP
routing, encrypted sessions, keepalive/rekey/reconnect and native installers.

## Phase 3 — Windows

Wintun integration, Windows Service, route/firewall management and signed setup.

## Phase 3b — macOS

Darwin virtual-adapter integration, Keychain-backed identity, LaunchDaemon,
route management, signed and notarized packaging, and an optional menu-bar UI.

## Phase 4 — management

Full server/client web panels, richer RBAC/audit, safe command delivery/results.

## Phase 5 — scale

Updates, ACLs, subnet router, exit node, multiple networks, HA, relay scaling and
Prometheus metrics.

## Phase 6 — mobile

Android app/VpnService, token or QR enrollment, device/admin views, notifications,
then iOS evaluation. macOS networking work is tracked separately in Phase 3b.
P2P, STUN and UDP hole punching may be evaluated later and
are not part of the first version.
