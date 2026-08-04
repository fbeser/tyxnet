#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This launcher is for macOS."
  exit 1
fi

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

config="configs/server.yaml"
if [[ $# -gt 0 && "$1" != --* ]]; then
  config="$1"
  shift
fi

mkdir -p bin
mkdir -p logs
echo "Building TyxNet Server and menu bar app..."
go build -o bin/tyxnet-server ./cmd/tyxnet-server
go build -o bin/tyxnet-server-tray ./cmd/tyxnet-server-tray

tray_token="$(uuidgen | tr -d '-')$(uuidgen | tr -d '-')"
export TYXNET_TRAY_TOKEN="$tray_token"
nohup ./bin/tyxnet-server-tray --server-url http://127.0.0.1:8443 >logs/server-tray.log 2>&1 &

echo "TyxNet will be available at http://127.0.0.1:8443."
echo "The menu bar icon opens the web console and can stop the server."
sudo -v
sudo env TYXNET_TRAY_TOKEN="$tray_token" nohup ./bin/tyxnet-server run --config "$config" "$@" >logs/server.log 2>logs/server-error.log &
echo "TyxNet Server is running in the background."
