#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This launcher is for macOS."
  exit 1
fi

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
config="${1:-client-state/client.yaml}"

mkdir -p bin logs
echo "Building TyxNet Client and menu bar app..."
go build -o bin/tyxnet-client ./cmd/tyxnet-client
go build -o bin/tyxnet-tray ./cmd/tyxnet-tray

tray_token="$(uuidgen | tr -d '-')$(uuidgen | tr -d '-')"
export TYXNET_TRAY_TOKEN="$tray_token"
nohup ./bin/tyxnet-tray --client-url http://127.0.0.1:9070 >logs/client-tray.log 2>&1 &
sudo -v
sudo env TYXNET_TRAY_TOKEN="$tray_token" nohup ./bin/tyxnet-client run --config "$config" >logs/client.log 2>logs/client-error.log &
echo "TyxNet Client is running in the background. Open http://127.0.0.1:9070 or use the menu bar icon."
