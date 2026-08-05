# macOS packaging

Build the universal Intel/Apple Silicon DMG on macOS with:

```text
sh scripts/package-macos.sh 0.1.0
```

The DMG contains `TyxNet Client.app` and `TyxNet Server.app`. Each app requests
administrator access for the native tunnel process and runs its menu-bar
companion as the logged-in user. Mutable configuration is stored under
`~/Library/Application Support/TyxNet`, with logs under `~/Library/Logs/TyxNet`.
Both application bundles include a native multi-resolution macOS icon.

Local builds use ad-hoc signing. Public distribution still requires an Apple
Developer ID signature and notarization. Client packet routing and a production
Network Extension also remain incomplete. See `docs/macos-client.md` for the
remaining requirements.

Run `bash scripts/start-client-macos.sh` or
`bash scripts/start-server-macos.sh` for local development without creating the
DMG. The web and tray startup switches register a root LaunchDaemon and user
LaunchAgent. They are removed when the switch is cleared.
