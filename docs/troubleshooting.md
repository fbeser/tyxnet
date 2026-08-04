# Troubleshooting

- `invalid enrollment token`: check exact token, expiry, revocation and use count.
- TLS errors: certificate SAN must match the configured server hostname.
- `load identity`: join first and verify state directory permissions/ownership.
- reconnect loop: inspect server JSON log and `tyxnet-client status/logs`.
- TUN permission error: ensure `/dev/net/tun` and CAP_NET_ADMIN; data plane is
  currently Experimental.
- SQLite lock: use one TyxNet server per database and writable data ownership.
