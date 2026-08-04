# Windows packaging status

The native server supports Wintun adapter creation. The repository does not
bundle Wintun: `scripts/install-wintun.ps1` downloads the official 0.14.1 archive,
verifies its pinned SHA-256 checksum, and installs the architecture-specific DLL
and license beside `tyxnet-server.exe`.

A role-aware `tyxnet-tray.exe` notification-area companion is implemented and
the development start script launches it separately from the connectivity
process. A production WiX installer and Windows Service remain planned. They will install
under `Program Files\TyxNet`, register Automatic startup, configure fixed firewall
rules, preserve Wintun notices, and keep connectivity independent of the tray UI.

The server development launcher also builds and starts
`tyxnet-server-tray.exe`. It shows local server availability and opens the
server web console. The tray and web startup switch creates or removes an
elevated Scheduled Task, and quitting the tray gracefully stops the elevated
server process.
