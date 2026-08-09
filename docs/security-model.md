# Security model

Assets are device private keys, admin credentials, enrollment/session tokens,
session keys and routed packet confidentiality/integrity. Attackers may control
the network, possess expired tokens, replay packets, spoof virtual source IPs or
send malformed API/UDP input. The host administrator and server are trusted.

Controls include TLS certificate validation, Ed25519 device proof, Argon2id
password hashing, SHA-256 random-token hashing, short sessions, RBAC, rate limits,
audit events, direction-separated HKDF keys, ChaCha20-Poly1305, replay windows,
authenticated NAT endpoint learning, source-IP validation, input limits and
private key file mode 0600. The server can observe traffic metadata and, in the
central design, is a high-value trusted relay. Data-plane credentials are issued
only through a certificate-validated HTTPS control connection.

The flow dashboard is limited to admins, operators, and viewers because it
reveals communication relationships across users. It records no payload,
DNS names, or application-protocol contents. Source/destination virtual IP,
IPv4 protocol number, TCP/UDP ports or ICMP type/code, packet count, and byte
count are held only in process memory for 60 seconds and are discarded on expiry
or server restart. The window is capped at 4096 unique flow keys to bound memory
use. Packets rejected by source validation or routing are excluded from telemetry.

Known gaps: unaudited protocol; no per-session UDP bandwidth limiter; in-memory
rate limits/challenges; no explicit CSRF token beyond a Strict SameSite session
cookie; no key rotation/recovery UX; and no production macOS Network Extension.
Native adapter creation requires
elevated OS privileges. The Windows helper pins and verifies the official Wintun
archive checksum before installing its DLL.
TLS termination and hardening remain operator responsibilities. The provided
HTTPS Compose stack uses Caddy for automatic public certificates.

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
authorization. Normal bearer sessions remain in session storage. A user-selected
30-day session is available only when the server URL uses HTTPS; the server
dashboard stores it in a Secure, HttpOnly, Strict SameSite cookie, while the
loopback-only packaged client UI stores its bearer token in browser local
storage. Only administrators can reset user passwords. The server hashes the
replacement with Argon2id and atomically revokes all bearer sessions belonging
to that user; password values and hashes are excluded from audit details. Tray
device data and commands are available only through loopback;
remote requests to `/api/tray` are rejected. A local process running as the same
OS user is within the trusted-host boundary.

Remote commands are fixed enums delivered only after device authentication.
Clients reject unknown types and expired commands, report `accepted` before
execution, and deduplicate accepted IDs to avoid repeated destructive actions.
Results require a fresh challenge and a domain-separated Ed25519 signature bound
to the device, command, status, and sanitized result. The server never supplies
an executable name, argument, script, or shell text. Windows, Linux, and macOS
implement fixed argument arrays, and the service account must already possess
the required shutdown privilege.

Tray startup and shutdown operations additionally require a high-entropy token
shared through the core and companion process environments. The endpoints also
require a loopback source. The token is embedded into protected platform startup
registrations, is never returned by the web API, and must not be logged.
