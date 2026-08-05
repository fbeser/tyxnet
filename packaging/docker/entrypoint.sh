#!/bin/sh
set -u

umask 077

config_path=/tmp/tyxnet-Caddyfile
server_pid=
caddy_pid=
monitor_pid=
stopping=0

stop_children() {
	stopping=1
	if [ -n "$server_pid" ]; then
		kill -TERM "$server_pid" 2>/dev/null || true
	fi
	if [ -n "$caddy_pid" ]; then
		kill -TERM "$caddy_pid" 2>/dev/null || true
	fi
	if [ -n "$monitor_pid" ]; then
		kill -TERM "$monitor_pid" 2>/dev/null || true
	fi
}

wait_with_timeout() {
	pid=$1
	if [ -z "$pid" ]; then
		return
	fi
	(
		sleep 10
		kill -KILL "$pid" 2>/dev/null || true
	) &
	watchdog_pid=$!
	wait "$pid" 2>/dev/null
	status=$?
	kill "$watchdog_pid" 2>/dev/null || true
	wait "$watchdog_pid" 2>/dev/null || true
	return "$status"
}

fail_status() {
	status=$1
	if [ "$status" -eq 0 ]; then
		return 1
	fi
	return "$status"
}

trap stop_children TERM INT

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
	reverse_proxy 127.0.0.1:8443
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
	reverse_proxy 127.0.0.1:8443
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

if ! caddy validate --config "$config_path" --adapter caddyfile; then
	exit 1
fi

tyxnet-server run --config /etc/tyxnet/server.https.yaml &
server_pid=$!

attempt=0
while ! wget -q -O /dev/null http://127.0.0.1:8443/; do
	if ! kill -0 "$server_pid" 2>/dev/null; then
		wait "$server_pid"
		status=$?
		fail_status "$status"
		exit $?
	fi
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 120 ]; then
		echo "TyxNet server did not become ready" >&2
		stop_children
		wait_with_timeout "$server_pid" || true
		exit 1
	fi
	sleep 0.25
done

caddy run --config "$config_path" --adapter caddyfile &
caddy_pid=$!

(
	while kill -0 "$server_pid" 2>/dev/null && kill -0 "$caddy_pid" 2>/dev/null; do
		sleep 0.25
	done
	if ! kill -0 "$server_pid" 2>/dev/null; then
		kill -TERM "$caddy_pid" 2>/dev/null || true
	fi
) &
monitor_pid=$!

wait "$caddy_pid"
caddy_status=$?
kill -TERM "$monitor_pid" 2>/dev/null || true
wait "$monitor_pid" 2>/dev/null || true
monitor_pid=

if [ "$stopping" -eq 1 ]; then
	wait_with_timeout "$server_pid" || true
	exit 0
fi

if ! kill -0 "$server_pid" 2>/dev/null; then
	wait "$server_pid"
	server_status=$?
	fail_status "$server_status"
	exit $?
fi

kill -TERM "$server_pid" 2>/dev/null || true
wait_with_timeout "$server_pid" || true
fail_status "$caddy_status"
exit $?
