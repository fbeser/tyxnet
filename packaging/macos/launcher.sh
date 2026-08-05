#!/bin/bash
set -eu

role="@ROLE@"
core_name="@CORE@"
tray_name="@TRAY@"
contents_dir="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
data_dir="$HOME/Library/Application Support/TyxNet"
log_dir="$HOME/Library/Logs/TyxNet"
mkdir -p "$data_dir" "$log_dir"

if [[ "$role" == "server" ]]; then
  config_path="$data_dir/server.yaml"
  if [[ ! -f "$config_path" ]]; then
    cp "$contents_dir/Resources/server.yaml" "$config_path"
  fi
  core_arguments=(run --config "$config_path" --local-web)
  tray_arguments=(--server-url http://127.0.0.1:8443)
  web_url="http://127.0.0.1:8443"
else
  config_path="$data_dir/client.yaml"
  core_arguments=(run --config "$config_path" --local-web)
  tray_arguments=(--client-url http://127.0.0.1:9070)
  web_url="http://127.0.0.1:9070"
fi

restart_prefix=""
if pgrep -x "$core_name" >/dev/null 2>&1; then
  if pgrep -x "$tray_name" >/dev/null 2>&1; then
    open "$web_url"
    exit 0
  fi
  printf -v restart_prefix '/usr/bin/pkill -x %q; ' "$core_name"
fi

tray_token="$(uuidgen | tr -d '-')$(uuidgen | tr -d '-')"
printf -v privileged_command '%scd %q && trap "" HUP; /usr/bin/env TYXNET_TRAY_TOKEN=%q %q' "$restart_prefix" "$data_dir" "$tray_token" "$contents_dir/MacOS/$core_name"
for argument in "${core_arguments[@]}"; do
  printf -v privileged_command '%s %q' "$privileged_command" "$argument"
done
printf -v privileged_command '%s >>%q 2>>%q </dev/null &' "$privileged_command" "$log_dir/$core_name.log" "$log_dir/$core_name-error.log"

/usr/bin/env TYXNET_TRAY_TOKEN="$tray_token" "$contents_dir/MacOS/$tray_name" "${tray_arguments[@]}" &
tray_pid=$!
if ! /usr/bin/osascript - "$privileged_command" <<'APPLESCRIPT'
on run argv
  do shell script (item 1 of argv) with administrator privileges
end run
APPLESCRIPT
then
  kill "$tray_pid" 2>/dev/null || true
  wait "$tray_pid" 2>/dev/null || true
  exit 1
fi

wait "$tray_pid"
