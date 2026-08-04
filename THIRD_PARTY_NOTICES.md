# Third-party notices

Direct Go dependencies (verify transitive notices during each release):

- `golang.org/x/crypto` — BSD-3-Clause
- `fyne.io/systray` — Apache-2.0
- `github.com/godbus/dbus/v5` — BSD-2-Clause (transitive tray dependency)
- `golang.zx2c4.com/wireguard/tun` — MIT; TyxNet uses only the OS TUN package,
  not the WireGuard protocol
- `golang.zx2c4.com/wintun` — MIT
- `gopkg.in/yaml.v3` — MIT and Apache-2.0 portions
- `modernc.org/sqlite` and its transitive packages — BSD-style licenses; SQLite
  itself is public domain

The repository does not contain a Wintun binary. On Windows, the explicit helper
downloads the official signed Wintun 0.14.1 distribution, verifies archive
SHA-256 `07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51`,
and copies the included `LICENSE.txt` beside the installed DLL as
`wintun-LICENSE.txt`.

Run dependency license and vulnerability scans before release.
