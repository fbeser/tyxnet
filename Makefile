.PHONY: build build-server build-client build-tray build-server-tray build-cli test test-race vet lint fmt web docker clean release package-windows package-macos
GO ?= go
BIN := bin

build: build-server build-client build-tray build-server-tray build-cli
$(BIN):
	mkdir -p $(BIN)
build-server: | $(BIN)
	$(GO) build -o $(BIN)/tyxnet-server ./cmd/tyxnet-server
build-client: | $(BIN)
	$(GO) build -o $(BIN)/tyxnet-client ./cmd/tyxnet-client
build-tray: | $(BIN)
	$(GO) build -o $(BIN)/tyxnet-tray ./cmd/tyxnet-tray
build-server-tray: | $(BIN)
	$(GO) build -o $(BIN)/tyxnet-server-tray ./cmd/tyxnet-server-tray
build-cli: | $(BIN)
	$(GO) build -o $(BIN)/tyxnetctl ./cmd/tyxnetctl
test:
	$(GO) test ./...
test-race:
	$(GO) test -race ./...
vet:
	$(GO) vet ./...
lint:
	golangci-lint run
fmt:
	gofmt -w cmd internal pkg
web:
	@echo "Web assets are Go-embedded templates; no separate build is required."
docker:
	docker build -f packaging/docker/Dockerfile -t tyxnet:dev .
clean:
	$(GO) clean
	rm -rf $(BIN) dist
release:
	sh scripts/release.sh
package-windows:
	sh scripts/package-windows.sh
package-macos:
	sh scripts/package-macos.sh
