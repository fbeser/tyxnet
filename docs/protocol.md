# TyxNet protocol v1 (experimental)

This design has not received an independent security audit. Protocol v1 carries
IPv4 packets through a central encrypted UDP relay after device authentication
on the HTTPS control stream.

## Header

Network byte order: `Magic[4]="TYXN"`, version u8, type u8, flags u16, network ID
u32, session ID u64, source device ID u64, destination device ID u64, sequence
u64 and payload length u16, followed by payload. The codec maximum is 64 KiB;
the deployed data plane rejects inner packets larger than the configured TUN
MTU and avoids IP fragmentation under the default 1280-byte setting.

Types: HELLO, CHALLENGE, AUTH, AUTH_OK, DATA, KEEPALIVE, CONTROL, COMMAND,
COMMAND_RESULT, DISCONNECT and ERROR.

## Data-session bootstrap

1. The client validates the server HTTPS certificate and authenticates to the
   control stream with its enrolled Ed25519 key and a one-use challenge.
2. For a secure direct or reverse-proxied HTTPS request, the server creates a
   random 256-bit secret, random non-zero 64-bit session ID, UDP port and expiry.
3. The `connected` event carries this `data_plane` object inside the protected
   HTTPS stream. Plain HTTP control connections never receive these credentials.
4. Both sides derive independent client-to-server and server-to-client keys with
   HKDF-SHA256 using the session ID as salt and direction-specific labels.
5. The client sends an authenticated UDP KEEPALIVE. Only after successful AEAD
   verification does the server associate that session with the observed UDP
   source endpoint, allowing NAT rebinding without trusting unauthenticated
   addresses.

## HTTP control stream v1

After the Ed25519 challenge, `/control/v1/connect` opens an SSE stream. Every
JSON event includes `protocol_version: 1`. The initial `connected` event includes
the authoritative `virtual_ip`, `virtual_network`, `ping_interval_seconds`, and
an optional `data_plane` bootstrap.
Subsequent `ping` events repeat the current assignment and interval so a static
IP change can be applied without reenrollment. A v1 client must ignore unknown JSON fields;
this permits additive fields without a version change. Removing or changing the
meaning or type of an existing field requires a new control endpoint version.

The stream can also emit an additive `command` event containing
`protocol_version`, command ID, an allowlisted type, creation time, and expiry.
Older v1 clients ignore this event, so adding it does not change the protocol
version. The server may redeliver `queued` or `delivered` commands until the
target reports `accepted`. A client records the ID before execution and never
executes a duplicate during that process lifetime. `accepted` is the at-most-once
boundary; terminal states are `succeeded` and `failed`, while commands that are
not accepted before their two-minute deadline become `expired`.

Command results use a fresh one-use device challenge. The Ed25519 signing input
is the ASCII domain `tyxnet-command-result-v1`, a zero byte, the 32-byte nonce,
then unsigned-16-bit big-endian length-prefixed device ID, command ID, status,
result, and error strings. This binds every result to its endpoint purpose and
prevents a connect proof or result for another command from being replayed.

## Packet protection

DATA and KEEPALIVE payloads use ChaCha20-Poly1305. Header bytes are AAD. Nonces are
derived from session identifier/direction and monotonically increasing sequence;
a key/session must terminate before counter wrap. Directional keys prevent nonce
collision across directions. Authentication failure drops the packet without an
oracle response.

The implemented replay window tracks the highest sequence and 64 prior packets.
Duplicate, zero and stale sequences are rejected. Invalid magic/version/length,
unknown session/source/destination and source-IP mismatch are dropped. The
client also rejects packets from an unexpected server UDP endpoint or carrying
an inner destination other than its assigned virtual IP. Routing occurs only
after authentication, replay checks and source validation.

## Lifecycle

The default control ping interval is 25 seconds and administrators can persist a
value from 5 seconds through 1 hour. Reconnect uses 1, 2, 5, 10, 30, then 60
second delays. Data sessions last 15 minutes; expiry closes the associated
control stream, causing a fresh random secret/session and reset replay state. Command
restart/shutdown validity is at most two minutes. Deployments must cap handshake,
authentication and per-session packet/bandwidth rates.

There is no TAP, Ethernet broadcast, DHCP, default-route capture, P2P, STUN or
hole punching in v1. Older v1 clients ignore the additive `data_plane` field and
remain connected to the management/control plane without virtual-IP forwarding.
