# Windows packaging

Build the unsigned x64 MSI on macOS with:

```text
brew install msitools
sh scripts/package-windows.sh 0.3.16
```

The installer places the server, client, tray companions, CLI, and verified
Wintun 0.14.1 files under `Program Files\TyxNet`. Start-menu shortcuts request
administrator access and keep mutable configuration and logs under
`ProgramData\TyxNet`. The installer is unsigned until it is passed through a
Windows code-signing pipeline.

The native server supports Wintun adapter creation. The repository does not
commit Wintun binaries; the packaging script downloads the official 0.14.1
archive and verifies its pinned SHA-256 checksum before including the x64 DLL
and license.

Automatic startup is configured from the tray after the first launch. A native
Windows Service, installer-managed firewall rules, and Authenticode signing
remain planned.

The server and client tray companions show local availability, open the web
console, and can gracefully stop their core process.
