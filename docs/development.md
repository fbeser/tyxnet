# Development

Go 1.25+ is required. Run `go mod download`, `make fmt`, `make test`, `make vet`
and `make build`. Rootless tests use SQLite `:memory:` and fake packet sinks/TUN.
Linux TUN manual tests require CAP_NET_ADMIN. Keep OpenAPI, protocol docs and CLI
README commands synchronized. CI is the release quality gate.

For release artifacts, do not use GitHub Actions. Build and publish from the
local release machine:

- `make release` builds cross-platform binaries and `dist/checksums.txt`.
- `make release-full` builds those binaries plus `TyxNet-<version>-macos-universal.dmg`
	and `TyxNet-<version>-windows-amd64.msi`.
- Set `VERSION` explicitly when needed, for example `make release-full VERSION=0.3.8`.
- Run `gh release create v<version> dist/* --title v<version>` after pushing the
	annotated tag, then verify the release page includes every local artifact.
- GitHub Releases do not update the CasaOS image. Publish a container image
	separately and pass `VERSION=<version>` to the Docker build so the console
	displays the actual release version instead of `dev`.
