# Windows client

Status: **Planned / not implemented**. Cross-compilation succeeds and a build-tagged
factory returns `TUN adapter is not implemented on this platform`. No driver is
bundled. Future work will use a separately reviewed Wintun distribution, Windows
Service for connectivity, localhost/named-pipe IPC for UI, fixed firewall rules,
Automatic startup and a WiX installer. Closing a future tray UI must not stop the
service. See `packaging/windows/` and `THIRD_PARTY_NOTICES.md`.
