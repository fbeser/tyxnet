# Development

Go 1.25+ is required. Run `go mod download`, `make fmt`, `make test`, `make vet`
and `make build`. Rootless tests use SQLite `:memory:` and fake packet sinks/TUN.
Linux TUN manual tests require CAP_NET_ADMIN. Keep OpenAPI, protocol docs and CLI
README commands synchronized. CI is the release quality gate.
