# macOS client

Status: **Planned / control-plane only**.

The release pipeline cross-compiles `tyxnet-client`, `tyxnetctl`, and the server
for Darwin amd64 and arm64. Enrollment, identity storage, the authenticated
control connection, reconnect behavior, and the localhost status page are
portable Go code. The current Darwin virtual-adapter factory deliberately returns
`TUN adapter is not implemented on this platform`, so virtual-IP traffic does not
work on macOS yet.

The repository includes a LaunchDaemon property-list scaffold. It is not a
finished installer and does not install or authorize a Network Extension.

## Planned native integration

The macOS implementation must choose and independently review one of these
approaches:

- a signed Network Extension using `NEPacketTunnelProvider`; or
- an appropriately privileged and sandboxed utun integration.

A production package will also require Apple signing identities, hardened
runtime settings, entitlements, notarization, a signed `.pkg`, safe Keychain
storage, route configuration limited to the TyxNet subnet, and uninstall logic.
The connectivity service must remain independent of any future menu-bar UI.

Until those items are complete, run the binary only for control-plane development
and do not present it as a functioning macOS VPN client.
