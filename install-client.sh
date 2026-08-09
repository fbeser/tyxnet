#!/bin/sh
set -eu

repository="fbeser/tyxnet"
version="latest"
server=""
token=""
name=""

usage() {
  echo "usage: install-client.sh [--version VERSION] [--server URL --token TOKEN --name NAME]" >&2
  exit 2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version|--server|--token|--name)
      [ "$#" -ge 2 ] || usage
      case "$1" in
        --version) version="${2#v}" ;;
        --server) server="$2" ;;
        --token) token="$2" ;;
        --name) name="$2" ;;
      esac
      shift 2
      ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

if { [ -n "$server" ] || [ -n "$token" ] || [ -n "$name" ]; } && { [ -z "$server" ] || [ -z "$token" ] || [ -z "$name" ]; }; then
  echo "--server, --token, and --name must be supplied together." >&2
  exit 2
fi

[ "$(id -u)" -eq 0 ] || { echo "Run this installer as root (for example: curl ... | sudo sh)." >&2; exit 1; }
[ "$(uname -s)" = "Linux" ] || { echo "This installer supports Linux only." >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "curl is required." >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { echo "sha256sum is required." >&2; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "systemd is required." >&2; exit 1; }
command -v ip >/dev/null 2>&1 || { echo "iproute2 (the ip command) is required." >&2; exit 1; }
[ -d /run/systemd/system ] || { echo "systemd is not running as PID 1 on this host." >&2; exit 1; }

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m). Supported: amd64, arm64." >&2; exit 1 ;;
esac

if [ "$version" = "latest" ]; then
  base_url="https://github.com/$repository/releases/latest/download"
else
  base_url="https://github.com/$repository/releases/download/v$version"
fi

temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM
cd "$temporary_directory"

client_asset="tyxnet-client-linux-$arch"
echo "Downloading TyxNet Client for $arch..."
curl -fL --retry 3 -o "$client_asset" "$base_url/$client_asset"
curl -fL --retry 3 -o checksums.txt "$base_url/checksums.txt"
grep "[[:space:]]\./$client_asset\$" checksums.txt | sha256sum -c -
chmod 0755 "$client_asset"

if [ -f /etc/tyxnet/client.yaml ]; then
  [ -f /etc/systemd/system/tyxnet-client.service ] || { echo "Existing config found but systemd unit is missing; refusing to overwrite /etc/tyxnet/client.yaml." >&2; exit 1; }
  install -m 0755 "$client_asset" /usr/local/bin/tyxnet-client
  systemctl daemon-reload
  systemctl enable tyxnet-client.service >/dev/null
  systemctl restart tyxnet-client.service
elif [ -n "$server" ]; then
  "./$client_asset" install --server "$server" --token "$token" --name "$name"
else
  "./$client_asset" install
fi

systemctl is-active --quiet tyxnet-client.service
echo "TyxNet Client is installed."
if [ -z "$server" ] && [ ! -f /var/lib/tyxnet/client/identity.json ]; then
  echo "Open http://127.0.0.1:9070 locally, or http://DEVICE_LAN_IP:9070 from your LAN, to enroll this device."
fi
echo "Status: sudo systemctl status tyxnet-client"
