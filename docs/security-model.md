# Security model

Assets are device private keys, admin credentials, enrollment/session tokens,
session keys and routed packet confidentiality/integrity. Attackers may control
the network, possess expired tokens, replay packets, spoof virtual source IPs or
send malformed API/UDP input. The host administrator and server are trusted.

Controls include TLS certificate validation, Ed25519 device proof, Argon2id
password hashing, SHA-256 random-token hashing, short sessions, RBAC, rate limits,
audit events, AEAD, replay window, source-IP validation, input limits and private
key file mode 0600. The server can observe traffic metadata and, in the central
design, is a high-value trusted relay.

Known gaps: unaudited protocol; unfinished UDP handshake/data plane; in-memory
rate limits/challenges; no CSRF cookie flow (API uses bearer headers); no key
rotation/recovery UX; no command delivery/results; and no completed client-side
Windows or macOS adapter/data-plane integration. Native adapter creation requires
elevated OS privileges. The Windows helper pins and verifies the official Wintun
archive checksum before installing its DLL.
TLS termination and hardening remain operator responsibilities.

Initial web setup is LAN-accessible by default. Operators must limit it to a
trusted private network because first credentials cross that listener, or use
`--local-web`/`allow_remote_setup: false` to restrict it. Setup can create only
the first user and becomes ineffective immediately afterward. Public and
reverse-proxy deployments should use TLS and bootstrap through the local CLI.

The client web console is also LAN-accessible by default and exposes enrollment
and status endpoints without UI authentication. Use `--local-web` or
`allow_remote_ui: false` when remote administration is unnecessary. TCP 9070
must be firewalled to the trusted subnet and never published to the internet.
Enrollment still requires a valid one-time token.

Role-aware client management requires an explicit server-account login. The
client proxies an allowlisted set of requests, while the server performs all
authorization. Bearer sessions remain memory/session-storage only and expire
normally. Tray device data and commands are available only through loopback;
remote requests to `/api/tray` are rejected. A local process running as the same
OS user is within the trusted-host boundary.

Tray startup and shutdown operations additionally require a high-entropy token
shared through the core and companion process environments. The endpoints also
require a loopback source. The token is embedded into protected platform startup
registrations, is never returned by the web API, and must not be logged.
