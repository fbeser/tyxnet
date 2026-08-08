#!/bin/sh
set -eu

version="${1:-0.3.12}"
case "$version" in
  *[!0-9.]*|.*|*.|*..*) echo "MSI version must contain three numeric parts: $version" >&2; exit 1 ;;
esac
if [ "$(printf '%s' "$version" | awk -F. '{print NF}')" -ne 3 ]; then
  echo "MSI version must contain three numeric parts: $version" >&2
  exit 1
fi
command -v wixl >/dev/null 2>&1 || { echo "wixl is required (macOS: brew install msitools)" >&2; exit 1; }

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
stage=$(mktemp -d "${TMPDIR:-/tmp}/tyxnet-windows.XXXXXX")
trap 'rm -rf "$stage"' EXIT INT TERM
output="$repo_root/dist/TyxNet-$version-windows-amd64.msi"
mkdir -p "$repo_root/dist"

build() {
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/fbeser/tyxnet/internal/buildinfo.Version=$version" -o "$stage/$2" "$repo_root/cmd/$1"
}
build tyxnet-client tyxnet-client.exe
build tyxnet-server tyxnet-server.exe
build tyxnetctl tyxnetctl.exe
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o "$stage/tyxnet-tray.exe" "$repo_root/cmd/tyxnet-tray"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o "$stage/tyxnet-server-tray.exe" "$repo_root/cmd/tyxnet-server-tray"
cp "$repo_root/packaging/windows/Launch-TyxNet.ps1" "$stage/"
cp "$repo_root/configs/server.yaml" "$stage/"

wintun_archive="$stage/wintun.zip"
curl --fail --location --silent --show-error "https://www.wintun.net/builds/wintun-0.14.1.zip" --output "$wintun_archive"
printf '%s  %s\n' "07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51" "$wintun_archive" | shasum -a 256 -c - >/dev/null
unzip -q "$wintun_archive" "wintun/bin/amd64/wintun.dll" "wintun/LICENSE.txt" -d "$stage/wintun-extract"
cp "$stage/wintun-extract/wintun/bin/amd64/wintun.dll" "$stage/"
cp "$stage/wintun-extract/wintun/LICENSE.txt" "$stage/wintun-LICENSE.txt"

wixl -D Version="$version" -D SourceDir="$stage" -a x64 -o "$output" "$repo_root/packaging/windows/Product.wxs"
echo "$output"
