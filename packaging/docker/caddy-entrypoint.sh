#!/bin/sh
set -eu

umask 077

config_path=/tmp/tyxnet-Caddyfile
domain=${TYXNET_DOMAIN:-}
public_ip=${TYXNET_PUBLIC_IP:-}

if [ -n "$domain" ] && [ -n "$public_ip" ]; then
	echo "Set only one of TYXNET_DOMAIN or TYXNET_PUBLIC_IP" >&2
	exit 1
fi

if [ -n "$domain" ]; then
	case "$domain" in
		*[!A-Za-z0-9.-]*)
			echo "TYXNET_DOMAIN contains unsupported characters" >&2
			exit 1
			;;
	esac
	cat >"$config_path" <<EOF
https://$domain {
	reverse_proxy tyxnet-server:8443
}
EOF
elif [ -n "$public_ip" ]; then
	case "$public_ip" in
		*[!0-9A-Fa-f:.]*)
			echo "TYXNET_PUBLIC_IP contains unsupported characters" >&2
			exit 1
			;;
	esac
	public_address=$public_ip
	case "$public_ip" in
		*:*) public_address="[$public_ip]" ;;
	esac
	cat >"$config_path" <<EOF
{
	default_sni $public_ip
}

https://$public_address {
	tls {
		issuer acme {
			dir https://acme-v02.api.letsencrypt.org/directory
			profile shortlived
		}
	}
	reverse_proxy tyxnet-server:8443
}
EOF
else
	cat >"$config_path" <<EOF
{
	admin off
	auto_https off
}

http://127.0.0.1:2019 {
	respond "TyxNet HTTPS is disabled" 404
}
EOF
fi

caddy validate --config "$config_path" --adapter caddyfile
exec caddy run --config "$config_path" --adapter caddyfile
