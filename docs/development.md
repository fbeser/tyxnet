# Development

Go 1.25+ is required. Run `go mod download`, `make fmt`, `make test`, `make vet`
and `make build`. Rootless tests use SQLite `:memory:` and fake packet sinks/TUN.
Linux TUN manual tests require CAP_NET_ADMIN. Keep OpenAPI, protocol docs and CLI
README commands synchronized. CI is the release quality gate.

For release artifacts:

- `make release` builds cross-platform binaries and `dist/checksums.txt`.
- `make release-full` builds those binaries plus `TyxNet-<version>-macos-universal.dmg`
	and `TyxNet-<version>-windows-amd64.msi`.
- Set `VERSION` explicitly when needed, for example `make release-full VERSION=0.3.8`.
- Verify the release page includes the binaries, `checksums.txt`, DMG, and MSI.
