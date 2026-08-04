# Linux client

Build with `make build-client`, join with `tyxnet-client join`, then run using a
validated YAML file. Native `install` joins first, writes `/etc/tyxnet/client.yaml`
and registers systemd. Identity is under `/var/lib/tyxnet/client` mode 0600.
Linux TUN opening exists, but route configuration and UDP transport integration
remain Experimental and require CAP_NET_ADMIN/root when completed.
