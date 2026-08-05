# Windows

Server and client adapter status: **Experimental / implemented**.

Run `scripts/start-server.ps1` in an Administrator PowerShell window. It builds
the server and, if necessary, downloads Wintun 0.14.1 from the official Wintun
site, verifies the pinned SHA-256 checksum, and copies `wintun.dll` plus its
license beside the executable. The upstream Wintun API reuses an adapter with
the same `TyxNet` name, while TyxNet reapplies its address and MTU on every start.

The fixed TyxNet Wintun GUID prevents Windows from generating a new numbered NLA
profile after every restart. Older numbered profiles are preserved intentionally
and may be removed manually after verifying they are unused.

`scripts/start-client.ps1` requests Administrator access, installs the verified
Wintun DLL when needed, and builds the client and GUI-subsystem
`tyxnet-tray.exe`. After authentication, the client creates a stable adapter
whose `TyxC-xxxxxxxx` name and GUID are derived from the server URL. It therefore
does not reuse the server's `TyxNet` adapter, and a different server URL receives
a different adapter. The notification-area menu opens the web console, shows the
role-permitted cached device list, and exposes admin/operator controls.
The Wintun adapter exchanges virtual IPv4 packets through the authenticated,
encrypted UDP data plane after HTTPS control authentication.
**Run at startup** installs an elevated logon task without recurring UAC prompts,
and **Quit TyxNet** stops the client as well as the tray. On macOS,
`sh scripts/package-windows.sh 0.3.0` creates an unsigned x64 MSI using `wixl`.
Authenticode signing, native Windows Service integration, and installer-managed
firewall rules remain planned.
