# Server installation

Use the README Docker or native instructions. A non-loopback bind requires both
`tls_cert`/`tls_key` unless `allow_insecure_http` is explicitly set for a trusted
reverse-proxy network. Allow TCP 8443 and UDP 51830 only,
create the first admin through stdin, back up `/var/lib/tyxnet`, and review systemd
hardening. `uninstall` removes binary/unit/config but deliberately preserves data.
