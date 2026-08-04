# Client UI

The current localhost-only status page and JSON API are in
`internal/client/client.go`. A future tray/web UI must communicate only through a
secure localhost or platform IPC boundary and must not own tunnel connectivity.
