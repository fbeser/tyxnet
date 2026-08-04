# TyxNet agent guide

## Architecture and ownership

- `cmd/`: thin entrypoints for server, client and remote CLI.
- `internal/control`: management/enrollment/device-control HTTP surface.
- `internal/client`: enrollment, device identity, reconnect and local UI.
- `internal/storage`: SQLite and append-only migrations.
- `internal/crypto` and `pkg/protocol`: security-critical session primitives and
  portable wire format. `pkg/protocol` is the only public shared package.
- `internal/routing`, `internal/tunnel`: L3 policy and adapter abstractions.
- `internal/platform/{linux,windows,darwin}`: build-tagged platform code only.
- `internal/control/web`: dependency-free management console assets embedded by
  the server. `web/` is reserved for a future source build if one becomes useful.
- `packaging/`, `configs/`, `docs/`: deployment, examples and authoritative docs.

Server-only code must not leak into client packages. Portable protocol messages
must never import OS packages. Keep entrypoints thin and inject dependencies.

## Mandatory safety rules

`internal/crypto`, `pkg/protocol`, enrollment, session/token storage, routing
source validation and command execution are security-critical. Never invent a
cryptographic primitive. Use standard X25519, Ed25519, ChaCha20-Poly1305/AES-GCM,
HKDF-SHA256, Argon2id and `crypto/rand`. A cryptography change requires focused
positive/negative tests, protocol/security documentation and reviewer attention.

Every wire-format or handshake change must increment/define protocol versioning
and document compatibility in `docs/protocol.md`. Never reuse an AEAD nonce.
Replay and packet-size checks must occur before routing. Never log passwords,
tokens, private keys, session keys or plaintext tunneled packets.

Remote commands are enums from a strict allowlist. Never execute server-provided
free text or invoke a shell. Fixed OS commands require fixed argument arrays.

## Data, API and compatibility

Migrations are immutable and append-only; add a numbered migration and migration
test. Never edit a migration already released. Database calls accept context.
Token/password material is stored only as a one-way hash.

API changes require `docs/openapi.yaml`, tests and README examples in the same
change. CLI command/flag/output changes require README updates. Prefer additive
API/protocol changes; breaking changes require a version boundary and migration
notes. Platform stubs must return an explicit `not implemented` error and must
never claim success.

## Quality gate

Every feature and bug fix needs tests. Before handoff run:

```text
gofmt -w cmd internal pkg
go test ./...
go test -race ./...
go vet ./...
golangci-lint run
```

Use `gofmt`; lint must remain clean. Keep packages small, avoid mutable globals,
propagate context, wrap errors, use structured logs with redaction, and implement
graceful shutdown. Create interfaces only at real substitution boundaries.

TODOs must state what is missing, why, and the completion condition. Do not call
unfinished work complete. New dependencies need a maintenance/security/license
case; run vulnerability and third-party license checks and update notices.

Commits should be focused and imperative. PRs describe risk, tests, migrations,
API/protocol effects, docs and rollback. Preserve unrelated user changes.
