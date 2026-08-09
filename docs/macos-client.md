# macOS

Server and command-line client adapter status: **Experimental / implemented**.

The privileged server and client open separate native utun devices and configure
their addresses and MTU. macOS assigns each interface name (`utunN`), so the configured `tunnel_name`
cannot become the visible interface name. The device is process-scoped and
disappears when the server closes; a later start may receive a different number.
This is normal and does not leave duplicate persistent adapters.

The release pipeline cross-compiles the command-line binaries. Enrollment,
identity storage, authenticated control connections, reconnect behavior, and the
LAN-accessible web console are portable Go code. The development client creates
its utun after receiving the authoritative virtual network and forwards IPv4
packets through the encrypted UDP data plane. The privileged client installs the
server-authoritative virtual IPv4 network route on that utun; macOS removes the
route when the process-scoped interface closes.

`tyxnet-tray` builds natively on macOS with Cocoa and places the same role-aware
device/browser menu in the menu bar. `sh scripts/package-macos.sh 0.3.14` creates
an ad-hoc-signed universal DMG for local testing. Public distribution still
requires a Developer ID signature and Apple notarization.

`bash scripts/start-client-macos.sh` starts the privileged client and user menu
bar process in the background. The web and tray **Run at startup** switches
install a root LaunchDaemon for the client and a LaunchAgent for the menu bar.
Clearing the switch removes both registrations. **Quit TyxNet** performs a
graceful client shutdown and then closes the menu bar process.

The local web console's **Leave server** action closes the active control and
data-plane connections, removes the saved identity and server URL, and returns
the client to enrollment. The destructive endpoint accepts only loopback
requests. Revoke the old offline device separately in the server console.

Production client integration should use a signed Network Extension with
`NEPacketTunnelProvider`. It also requires entitlements, notarization, a signed
package, safe Keychain storage, subnet-limited routes, and uninstall logic. The
connectivity service remains independent of the menu-bar companion.
