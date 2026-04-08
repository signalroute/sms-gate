# SPDX-License-Identifier: GPL-3.0-or-later
# go-sms-gate Makefile

BINARY     := go-sms-gate
MODULE     := github.com/yanujz/go-sms-gate
CMD        := ./cmd/gateway

VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -s -w \
              -X main.version=$(VERSION) \
              -X main.buildTime=$(BUILD_TIME)

# Host platform default
GOOS       ?= $(shell go env GOOS)
GOARCH     ?= $(shell go env GOARCH)
CGO_ENABLED := 0

# Installation paths
INSTALL_BIN  := /usr/local/bin
INSTALL_CONF := /etc/go-sms-gate
SYSTEMD_UNIT := /etc/systemd/system

.PHONY: all build test test-verbose test-race lint vet tidy \
        build-arm64 build-arm32 build-amd64 \
        install install-service uninstall clean help

# ── Default target ─────────────────────────────────────────────────────────
all: tidy build

# ── Build ──────────────────────────────────────────────────────────────────
build:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)
	@echo "Built $(BINARY) ($(GOOS)/$(GOARCH))"

# Cross-compile for Raspberry Pi 4 / Pi 5 (ARM64)
build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	  go build -ldflags="$(LDFLAGS)" -o $(BINARY)-arm64 $(CMD)
	@echo "Built $(BINARY)-arm64"

# Cross-compile for Raspberry Pi 2 / 3 (ARM32 v7)
build-arm32:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
	  go build -ldflags="$(LDFLAGS)" -o $(BINARY)-arm32 $(CMD)
	@echo "Built $(BINARY)-arm32"

# Native AMD64
build-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -ldflags="$(LDFLAGS)" -o $(BINARY)-amd64 $(CMD)
	@echo "Built $(BINARY)-amd64"

# Build all cross-compile targets
build-all: build-arm64 build-arm32 build-amd64

# ── Test ───────────────────────────────────────────────────────────────────
test:
	CGO_ENABLED=0 go test ./... -count=1

test-verbose:
	CGO_ENABLED=0 go test ./... -v -count=1

test-race:
	CGO_ENABLED=0 go test ./... -race -count=1 -timeout 60s

# Run only tests matching a pattern: make test-run T=TestDecodePDU
test-run:
	CGO_ENABLED=0 go test ./... -run $(T) -v -count=1

# ── Code quality ───────────────────────────────────────────────────────────
vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
	  { echo "golangci-lint not found; install from https://golangci-lint.run/"; exit 1; }
	golangci-lint run ./...

tidy:
	go mod tidy

# ── Install ────────────────────────────────────────────────────────────────

# Install binary + systemd unit.
# Usage: sudo make install
install: build
	install -m 755 $(BINARY) $(INSTALL_BIN)/$(BINARY)
	@echo "Installed $(INSTALL_BIN)/$(BINARY)"

# Install config directory and example config (non-destructive: skips if exists).
install-config:
	install -d -m 750 $(INSTALL_CONF)
	@if [ ! -f $(INSTALL_CONF)/config.yaml ]; then \
	  install -m 640 configs/config.example.yaml $(INSTALL_CONF)/config.yaml; \
	  echo "Installed example config to $(INSTALL_CONF)/config.yaml"; \
	else \
	  echo "Config already exists, skipping: $(INSTALL_CONF)/config.yaml"; \
	fi
	@if [ ! -f $(INSTALL_CONF)/env ]; then \
	  install -m 600 /dev/null $(INSTALL_CONF)/env; \
	  echo "# Set GATEWAY_TOKEN here" >> $(INSTALL_CONF)/env; \
	  echo "Created empty env file: $(INSTALL_CONF)/env"; \
	fi

# Install systemd unit file.
install-service: install install-config
	install -m 644 deployments/go-sms-gate.service $(SYSTEMD_UNIT)/go-sms-gate.service
	systemctl daemon-reload
	@echo ""
	@echo "Installed systemd service. Next steps:"
	@echo "  1. Edit $(INSTALL_CONF)/config.yaml"
	@echo "  2. Set GATEWAY_TOKEN in $(INSTALL_CONF)/env"
	@echo "  3. systemctl enable --now go-sms-gate"

uninstall:
	-systemctl disable --now go-sms-gate 2>/dev/null || true
	-rm -f $(INSTALL_BIN)/$(BINARY)
	-rm -f $(SYSTEMD_UNIT)/go-sms-gate.service
	systemctl daemon-reload
	@echo "Uninstalled. Config left in $(INSTALL_CONF) — remove manually if desired."

# ── Utility ────────────────────────────────────────────────────────────────
clean:
	rm -f $(BINARY) $(BINARY)-arm64 $(BINARY)-arm32 $(BINARY)-amd64

# Quick deploy to a remote Pi: make deploy HOST=pi@raspberrypi.local ARCH=arm64
deploy: build-$(ARCH)
	scp $(BINARY)-$(ARCH) $(HOST):/tmp/$(BINARY).new
	ssh $(HOST) "mv /tmp/$(BINARY).new $(INSTALL_BIN)/$(BINARY) && systemctl restart go-sms-gate"
	@echo "Deployed to $(HOST)"

help:
	@echo "go-sms-gate build targets"
	@echo ""
	@echo "  make build           Native build (current OS/arch)"
	@echo "  make build-arm64     Cross-compile for Raspberry Pi 4/5"
	@echo "  make build-arm32     Cross-compile for Raspberry Pi 2/3"
	@echo "  make build-all       All three targets"
	@echo ""
	@echo "  make test            Run all tests"
	@echo "  make test-race       Run tests with race detector"
	@echo "  make test-run T=X    Run tests matching pattern X"
	@echo ""
	@echo "  make install-service Install binary + config + systemd unit"
	@echo "  make deploy HOST=... ARCH=...  One-shot remote deploy"
	@echo ""
	@echo "  make clean           Remove build artefacts"
