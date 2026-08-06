# TyxNet agent guide

## Architecture and ownership

- `cmd/`: thin entrypoints for server, client and remote CLI.
- `internal/control`: management/enrollment/device-control HTTP surface.
- `internal/client`: enrollment, device identity, reconnect and local UI.
- `internal/dataplane`: encrypted UDP sessions, authenticated endpoint learning,
  replay protection and central virtual-IP packet forwarding.
- `internal/storage`: SQLite and append-only migrations.
- `internal/crypto` and `pkg/protocol`: security-critical session primitives and
  portable wire format. `pkg/protocol` is the only public shared package.
- `internal/routing`, `internal/tunnel`: L3 source/destination policy, payload-blind
  traffic accounting and native adapter abstractions.
- `internal/platform/{linux,windows,darwin}`: build-tagged platform code only.
- `internal/control/web`: dependency-free management console assets embedded by
  the server. `web/` is reserved for a future source build if one becomes useful.
- `internal/client/web`: dependency-free local setup/status/management proxy UI
  embedded by the client. Keep it separate from the server console.
- `packaging/`, `configs/`, `docs/`: deployment, examples and authoritative docs.

`docker-compose.yml` is the canonical Docker deployment for LAN access and
domain or public-IP HTTPS. Keep it compatible with legacy Compose 1.29; do not
add parallel Compose variants when environment variables can express the mode.
CasaOS and standard Docker use one container supervised by the image-owned
`packaging/docker/entrypoint.sh`. Keep TyxNet startup before Caddy, forward stop
signals to both processes, fail when either exits unexpectedly, and preserve the
three configurable legacy volume names.

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

The data-plane bootstrap is delivered only after device authentication and only
over HTTPS. Derive directional keys with HKDF, keep nonce spaces directional,
bind packets to their session ID and learn UDP endpoints only from successfully
authenticated packets. A control disconnect must revoke its data-plane session.
Rate-limit invalid datagrams before expensive processing.

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

The server console and client UI are embedded no-build assets. Keep responses
defensive against missing/null collections, preserve responsive SVG content
inside its viewBox, and support clipboard fallbacks where the browser Clipboard
API is unavailable. UI behavior changes require embedded-asset or handler tests.
Destructive client-local actions such as forgetting enrollment must be restricted
to loopback requests, cancel active control/data-plane work, remove local identity
material and clearly state that server-side device revocation is separate.

Docker runs the server as the container entrypoint, so platform startup controls
must report unavailable rather than invoking systemd. Public deployments use the
HTTPS Compose overlay and Caddy; do not expose plain management HTTP publicly.
The TCP control port and UDP tunnel port are distinct and both must be documented.

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

For releases, update every user-facing version example, `internal/buildinfo.Version`
fallback, and packaging-script default together. Run the full quality gate,
`scripts/release.sh <version>`, and the supported installer builders before
tagging. Tags are `vMAJOR.MINOR.PATCH`. Standard releases are published locally:
commit and push `main`, build artifacts locally, create and push the annotated
tag, then use `gh release create v<version> dist/*`. Do not use GitHub Actions
for release artifact publishing; tag pushes must not trigger a release workflow.
Verify the GitHub Release contains every local artifact and its checksum file.

GitHub Releases and GHCR images are independent products. A GitHub Release does
not update CasaOS, which pulls `ghcr.io/fbeser/tyxnet:latest`. If a container
image is needed, publish it from the local release machine after the GitHub
Release. Authenticate with `gh auth refresh --scopes write:packages`, create a
`docker-container` Buildx builder when necessary, then build and push both
`ghcr.io/fbeser/tyxnet:<version>` and `ghcr.io/fbeser/tyxnet:latest` for
`linux/amd64,linux/arm64`, passing `--build-arg VERSION=<version>`. Verify both
tags resolve to the same multi-platform manifest and inspect the published ARM64
binary for the embedded version before asking CasaOS to redeploy. Never rely on
the `dev` fallback. The setup and server-status APIs expose `buildinfo.Version`,
and the embedded console renders that exact value.

Commits should be focused and imperative. PRs describe risk, tests, migrations,
API/protocol effects, docs and rollback. Preserve unrelated user changes.
