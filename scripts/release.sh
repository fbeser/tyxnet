#!/bin/sh
set -eu
mkdir -p dist
build() { GOOS="$1" GOARCH="$2" CGO_ENABLED=0 go build -trimpath -o "dist/$4" "./cmd/$3"; }
for arch in amd64 arm64; do
  build linux "$arch" tyxnet-server "tyxnet-server-linux-$arch"
  build linux "$arch" tyxnet-client "tyxnet-client-linux-$arch"
  build linux "$arch" tyxnetctl "tyxnetctl-linux-$arch"
done
build windows amd64 tyxnet-client tyxnet-client-windows-amd64.exe
build windows amd64 tyxnetctl tyxnetctl-windows-amd64.exe
for arch in amd64 arm64; do
  build darwin "$arch" tyxnet-server "tyxnet-server-darwin-$arch"
  build darwin "$arch" tyxnet-client "tyxnet-client-darwin-$arch"
  build darwin "$arch" tyxnetctl "tyxnetctl-darwin-$arch"
done
(cd dist && sha256sum * > checksums.txt)
