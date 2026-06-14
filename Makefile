CMD_PKG := ./cmd/claude-acp-adapter
BINARY := claude-acp-adapter
BIN_DIR := bin
BIN := $(BIN_DIR)/$(BINARY)
GO_FILES := $(shell find cmd internal -name '*.go' -type f)

.PHONY: help build run query fmt test test-race vet check smoke clean

help:
	@printf '%s\n' \
		'Targets:' \
		'  make build      Build bin/claude-acp-adapter' \
		'  make run        Run the ACP stdio server' \
		'  make query      Run direct query smoke prompt' \
		'  make fmt        Format Go sources' \
		'  make test       Run unit tests' \
		'  make test-race  Run race-enabled tests' \
		'  make vet        Run go vet' \
		'  make check      Run fmt, test, race test, vet' \
		'  make smoke      Run local Claude transport smoke' \
		'  make clean      Remove build artifacts'

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN) $(CMD_PKG)

run:
	go run $(CMD_PKG)

query:
	go run $(CMD_PKG) query -cwd /tmp -prompt "Reply with exactly one word: OK"

fmt:
	gofmt -w $(GO_FILES)

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

check: fmt test test-race vet

smoke:
	go run $(CMD_PKG) query -cwd /tmp -timeout 45s -prompt "Reply with exactly one word: OK"

clean:
	rm -rf $(BIN_DIR)
