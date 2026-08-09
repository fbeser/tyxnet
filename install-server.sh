#!/bin/sh
set -eu

repository="fbeser/tyxnet"
version="latest"
listen_address="0.0.0.0"
api_port="8443"
tunnel_port="51830"
network="10.90.0.0/24"
tls_cert=""
tls_key=""

usage() {
  echo "usage: install-server.sh [--version VERSION] [--listen-address IP] [--api-port PORT] [--tunnel-port PORT] [--network CIDR] [--tls-cert PATH --tls-key PATH]" >&2
  exit 2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version|--listen-address|--api-port|--tunnel-port|--network|--tls-cert|--tls-key)
      [ "$#" -ge 2 ] || usage
      case "$1" in
        --version) version="${2#v}" ;;
        --listen-address) listen_address="$2" ;;
        --api-port) api_port="$2" ;;
        --tunnel-port) tunnel_port="$2" ;;
        --network) network="$2" ;;
        --tls-cert) tls_cert="$2" ;;
        --tls-key) tls_key="$2" ;;
      esac
      shift 2
      ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

if { [ -n "$tls_cert" ] || [ -n "$tls_key" ]; } && { [ -z "$tls_cert" ] || [ -z "$tls_key" ]; }; then
  echo "--tls-cert and --tls-key must be supplied together." >&2
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

server_asset="tyxnet-server-linux-$arch"
ctl_asset="tyxnetctl-linux-$arch"
echo "Downloading TyxNet Server for $arch..."
curl -fL --retry 3 -o "$server_asset" "$base_url/$server_asset"
curl -fL --retry 3 -o "$ctl_asset" "$base_url/$ctl_asset"
curl -fL --retry 3 -o checksums.txt "$base_url/checksums.txt"
grep "[[:space:]]\./$server_asset\$" checksums.txt | sha256sum -c -
grep "[[:space:]]\./$ctl_asset\$" checksums.txt | sha256sum -c -
chmod 0755 "$server_asset" "$ctl_asset"
install -m 0755 "$ctl_asset" /usr/local/bin/tyxnetctl

if [ -f /etc/tyxnet/server.yaml ]; then
  [ -f /etc/systemd/system/tyxnet-server.service ] || { echo "Existing config found but systemd unit is missing; refusing to overwrite /etc/tyxnet/server.yaml." >&2; exit 1; }
  install -m 0755 "$server_asset" /usr/local/bin/tyxnet-server
  systemctl daemon-reload
  systemctl enable tyxnet-server.service >/dev/null
  systemctl restart tyxnet-server.service
else
  "./$server_asset" install \
    --listen-address "$listen_address" \
    --api-port "$api_port" \
    --tunnel-port "$tunnel_port" \
    --network "$network" \
    --tls-cert "$tls_cert" \
    --tls-key "$tls_key"
fi

systemctl is-active --quiet tyxnet-server.service
echo "TyxNet Server and tyxnetctl are installed."
echo "Web console uses the management port in /etc/tyxnet/server.yaml (new installs: $api_port)."
if [ -z "$tls_cert" ]; then
  echo "Plain HTTP is suitable only for a trusted LAN; configure TLS before public exposure."
fi
echo "Status: sudo systemctl status tyxnet-server"
