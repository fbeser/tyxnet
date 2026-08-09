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
- Set `VERSION` explicitly when needed, for example `make release-full VERSION=0.3.16`.
- Run `gh release create v<version> dist/* --title v<version>` after pushing the
	annotated tag, then verify the release page includes every local artifact.
- GitHub Releases do not update the CasaOS image. After publishing the release,
	publish the matching multi-architecture image from the same local checkout:

```bash
version=0.3.16
gh auth refresh --hostname github.com --scopes write:packages
gh auth token | docker login ghcr.io -u "$(gh api user --jq .login)" --password-stdin
docker buildx create --name tyxnet-release --driver docker-container --use 2>/dev/null || true
docker buildx inspect --bootstrap
docker buildx build --builder tyxnet-release \
	--platform linux/amd64,linux/arm64 \
	--file packaging/docker/Dockerfile \
	--build-arg VERSION="$version" \
	--tag "ghcr.io/fbeser/tyxnet:$version" \
	--tag ghcr.io/fbeser/tyxnet:latest \
	--push .
```

	The `VERSION` build argument is required: it replaces the runtime `dev`
	fallback shown by the console. Confirm both tags have the same manifest and
	contain Linux ARM64 before a CasaOS redeploy:

```bash
docker buildx imagetools inspect "ghcr.io/fbeser/tyxnet:$version"
docker buildx imagetools inspect ghcr.io/fbeser/tyxnet:latest
```

	After CasaOS updates the application, verify the running server rather than
	the Docker tag label:

```bash
curl -sk http://SERVER_IP:8443/api/v1/setup/status
```

	The response must include `"version":"<version>"`.
