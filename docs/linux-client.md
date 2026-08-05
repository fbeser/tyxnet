# Linux client

Build with `make build-client`, join with `tyxnet-client join`, then run using a
validated YAML file. Native `install` joins first, writes `/etc/tyxnet/client.yaml`
and registers systemd. Identity is under `/var/lib/tyxnet/client` mode 0600.
The server and client create/configure separate named TUN devices and require
CAP_NET_ADMIN/root. The client name is stable for its server URL unless
`tunnel_name` overrides it. Packet forwarding into the UDP transport remains
Experimental, so adapter presence alone does not provide connectivity.

The administrator-only web startup switch writes and enables a systemd unit;
clearing it disables and removes that unit. The current process keeps running
until explicitly stopped. Linux currently uses the web interface rather than a
native tray companion.

The local web console's **Leave server** action closes the active control and
data-plane connections, removes the saved identity and server URL, and returns
the process to enrollment without stopping its systemd service. The destructive
endpoint accepts only loopback requests. Revoke the old offline device
separately in the server console.
