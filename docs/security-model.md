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
rotation/recovery UX; minimal UI; no command delivery/results; no Windows TUN.
TLS termination and hardening remain operator responsibilities.
