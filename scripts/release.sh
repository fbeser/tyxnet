#!/bin/sh
set -eu
version="${1:-${GITHUB_REF_NAME:-dev}}"
version="${version#v}"
version_flag="-X github.com/fbeser/tyxnet/internal/buildinfo.Version=$version"
mkdir -p dist
build() { GOOS="$1" GOARCH="$2" CGO_ENABLED=0 go build -trimpath -ldflags "$version_flag" -o "dist/$4" "./cmd/$3"; }
for arch in amd64 arm64; do
  build linux "$arch" tyxnet-server "tyxnet-server-linux-$arch"
  build linux "$arch" tyxnet-client "tyxnet-client-linux-$arch"
  build linux "$arch" tyxnetctl "tyxnetctl-linux-$arch"
done
for arch in amd64 arm64; do
  build windows "$arch" tyxnet-server "tyxnet-server-windows-$arch.exe"
  build windows "$arch" tyxnet-client "tyxnet-client-windows-$arch.exe"
  build windows "$arch" tyxnetctl "tyxnetctl-windows-$arch.exe"
  GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui $version_flag" -o "dist/tyxnet-tray-windows-$arch.exe" ./cmd/tyxnet-tray
  GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui $version_flag" -o "dist/tyxnet-server-tray-windows-$arch.exe" ./cmd/tyxnet-server-tray
done
for arch in amd64 arm64; do
  build darwin "$arch" tyxnet-server "tyxnet-server-darwin-$arch"
  build darwin "$arch" tyxnet-client "tyxnet-client-darwin-$arch"
  build darwin "$arch" tyxnetctl "tyxnetctl-darwin-$arch"
done
rm -f dist/checksums.txt
if command -v sha256sum >/dev/null 2>&1; then
  (cd dist && sha256sum * > checksums.txt)
else
  (cd dist && shasum -a 256 * > checksums.txt)
fi
