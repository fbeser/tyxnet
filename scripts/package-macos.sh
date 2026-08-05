#!/bin/sh
set -eu

version="${1:-0.2.1}"
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
stage=$(mktemp -d "${TMPDIR:-/tmp}/tyxnet-macos.XXXXXX")
trap 'rm -rf "$stage"' EXIT INT TERM
volume="$stage/TyxNet $version"
output="$repo_root/dist/TyxNet-$version-macos-universal.dmg"
mkdir -p "$repo_root/dist" "$volume"

icon_source="$stage/TyxNet-1024.png"
iconset="$stage/TyxNet.iconset"
mkdir -p "$iconset"
go run "$repo_root/packaging/macos/icon/main.go" "$icon_source"
for icon_spec in "16:icon_16x16.png" "32:icon_16x16@2x.png" "32:icon_32x32.png" "64:icon_32x32@2x.png" "128:icon_128x128.png" "256:icon_128x128@2x.png" "256:icon_256x256.png" "512:icon_256x256@2x.png" "512:icon_512x512.png" "1024:icon_512x512@2x.png"; do
  icon_pixels=${icon_spec%%:*}
  icon_name=${icon_spec#*:}
  sips -z "$icon_pixels" "$icon_pixels" "$icon_source" --out "$iconset/$icon_name" >/dev/null
done
iconutil -c icns "$iconset" -o "$stage/TyxNet.icns"

build_universal() {
  command_name=$1
  output_path=$2
  ldflags=$3
  case "$command_name" in
    tyxnet-client|tyxnet-server) ldflags="$ldflags -X github.com/fbeser/tyxnet/internal/buildinfo.Version=$version" ;;
  esac
  cgo_enabled=0
  case "$command_name" in
    *tray) cgo_enabled=1 ;;
  esac
  if [ "$cgo_enabled" -eq 1 ]; then
    GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 CC="clang -arch x86_64" go build -trimpath -ldflags "$ldflags" -o "$stage/$command_name-amd64" "$repo_root/cmd/$command_name"
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 CC="clang -arch arm64" go build -trimpath -ldflags "$ldflags" -o "$stage/$command_name-arm64" "$repo_root/cmd/$command_name"
  else
    GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$stage/$command_name-amd64" "$repo_root/cmd/$command_name"
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$stage/$command_name-arm64" "$repo_root/cmd/$command_name"
  fi
  lipo -create "$stage/$command_name-amd64" "$stage/$command_name-arm64" -output "$output_path"
}

make_app() {
  role=$1
  display_name=$2
  identifier=$3
  core_name=$4
  tray_name=$5
  app="$volume/$display_name.app"
  contents="$app/Contents"
  mkdir -p "$contents/MacOS" "$contents/Resources"
  cp "$stage/TyxNet.icns" "$contents/Resources/TyxNet.icns"
  build_universal "$core_name" "$contents/MacOS/$core_name" "-s -w"
  build_universal "$tray_name" "$contents/MacOS/$tray_name" "-s -w"
  sed -e "s/@ROLE@/$role/g" -e "s/@CORE@/$core_name/g" -e "s/@TRAY@/$tray_name/g" "$repo_root/packaging/macos/launcher.sh" > "$contents/MacOS/TyxNet"
  chmod 0755 "$contents/MacOS/TyxNet" "$contents/MacOS/$core_name" "$contents/MacOS/$tray_name"
  if [ "$role" = "server" ]; then
    cp "$repo_root/configs/server.yaml" "$contents/Resources/server.yaml"
  fi
  sed -e "s/@DISPLAY_NAME@/$display_name/g" -e "s/@IDENTIFIER@/$identifier/g" -e "s/@VERSION@/$version/g" "$repo_root/packaging/macos/Info.plist.in" > "$contents/Info.plist"
  plutil -lint "$contents/Info.plist" >/dev/null
  codesign --force --deep --sign - "$app" >/dev/null
}

make_app client "TyxNet Client" com.tyxnet.client tyxnet-client tyxnet-tray
make_app server "TyxNet Server" com.tyxnet.server tyxnet-server tyxnet-server-tray
ln -s /Applications "$volume/Applications"
hdiutil create -quiet -volname "TyxNet $version" -srcfolder "$volume" -ov -format UDZO "$output"
echo "$output"
