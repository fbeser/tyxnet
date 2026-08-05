# TyxNet protocol v1 (experimental)

This design has not received an independent security audit. The implemented
packet codec and cryptographic building blocks are a testable skeleton; the UDP
handshake/data-plane wiring is incomplete.

## Header

Network byte order: `Magic[4]="TYXN"`, version u8, type u8, flags u16, network ID
u32, session ID u64, source device ID u64, destination device ID u64, sequence
u32 and payload length u16, followed by payload. Current maximum payload is 64
KiB at the codec boundary; deployed UDP should use a conservative ~1280 MTU and
reject fragmentation-dependent oversize packets.

Types: HELLO, CHALLENGE, AUTH, AUTH_OK, DATA, KEEPALIVE, CONTROL, COMMAND,
COMMAND_RESULT, DISCONNECT and ERROR.

## Intended handshake

1. Client sends version, device ID, fresh X25519 ephemeral key and random nonce.
2. Server replies with its ephemeral key, nonce, certificate-bound server
   identity and transcript signature.
3. Client verifies TLS hostname/certificate and the signed transcript, then signs
   the complete transcript with its enrolled Ed25519 key.
4. Server verifies the stored device public key. Both sides compute X25519 ECDH
   and derive directional keys with HKDF-SHA256 using transcript hash as salt and
   version/direction labels as info.
5. AUTH_OK assigns a random session ID and starts sequence counters at 1.

The current control channel implements the Ed25519 challenge portion over TLS,
not this full UDP handshake.

## HTTP control stream v1

After the Ed25519 challenge, `/control/v1/connect` opens an SSE stream. Every
JSON event includes `protocol_version: 1`. The initial `connected` event includes
the authoritative `virtual_ip`, `virtual_network`, and `ping_interval_seconds`.
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

DATA and control payloads use ChaCha20-Poly1305. Header bytes are AAD. Nonces are
derived from session identifier/direction and monotonically increasing sequence;
a key/session must terminate before counter wrap. Directional keys prevent nonce
collision across directions. Authentication failure drops the packet without an
oracle response.

The implemented replay window tracks the highest sequence and 64 prior packets.
Duplicate, zero and stale sequences are rejected. Invalid magic/version/length,
unknown session/source/destination and source-IP mismatch are dropped and
rate-limited. Never route before authentication and source validation.

## Lifecycle

The default control ping interval is 25 seconds and administrators can persist a
value from 5 seconds through 1 hour. Reconnect uses 1, 2, 5, 10, 30, then 60
second delays. Target session lifetime is 15 minutes with proactive rekey before expiry;
new ephemeral X25519 keys create a new session ID and reset replay state. Command
restart/shutdown validity is at most two minutes. Deployments must cap handshake,
authentication and per-session packet/bandwidth rates.

There is no TAP, Ethernet broadcast, DHCP, default-route capture, P2P, STUN or
hole punching in v1.
