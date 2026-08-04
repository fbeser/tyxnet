# macOS packaging status

This directory is a packaging scaffold, not a finished installer. The supplied
LaunchDaemon plist shows the intended service boundary. The native
`tyxnet-tray` development binary provides a role-aware menu-bar menu, but no
signed/notarized `.app` bundle is produced yet. Client packet routing and a
production Network Extension also remain incomplete. See
`docs/macos-client.md` for the remaining requirements.

The `tyxnet-server-tray` development binary provides the equivalent server
menu-bar status and web-console shortcut. Run `bash scripts/start-server-macos.sh`
to build and launch it with the server. It is also an unsigned development
binary rather than a packaged `.app`.

The development web and tray startup switches register a root LaunchDaemon and
user LaunchAgent. They are removed when the switch is cleared. The tray's
**Quit TyxNet** action uses an authenticated loopback request to stop the core
process before closing the menu-bar companion.
