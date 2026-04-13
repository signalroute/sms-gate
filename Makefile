# SPDX-License-Identifier: MIT
# sms-gate Makefile

BINARY     := bin/sms-gate
MODULE     := github.com/signalroute/sms-gate
CMD        := ./cmd/gateway

VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -s -w \
              -X main.version=$(VERSION) \
              -X main.commit=$(COMMIT) \
              -X main.buildTime=$(BUILD_TIME)

# Host platform default
GOOS       ?= $(shell go env GOOS)
GOARCH     ?= $(shell go env GOARCH)
CGO_ENABLED := 0

# Installation paths
INSTALL_BIN  := /usr/local/bin
INSTALL_CONF := /etc/sms-gate
SYSTEMD_UNIT := /etc/systemd/system

.PHONY: all build test test-verbose test-race lint vet tidy fmt check \
        build-arm64 build-arm32 build-amd64 build-all \
        docker e2e bench \
        install install-config install-service uninstall \
        pre-commit clean help

# ── Default target ─────────────────────────────────────────────────────────
all: tidy build

# ── Build ──────────────────────────────────────────────────────────────────
build:
	mkdir -p bin
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)
	@echo "Built $(BINARY) ($(GOOS)/$(GOARCH))"

# Cross-compile for Raspberry Pi 4 / Pi 5 (ARM64)
build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	  go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY)-arm64 $(CMD)
	@echo "Built $(BINARY)-arm64"

# Cross-compile for Raspberry Pi 2 / 3 (ARM32 v7)
build-arm32:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
	  go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY)-arm32 $(CMD)
	@echo "Built $(BINARY)-arm32"

# Native AMD64
build-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY)-amd64 $(CMD)
	@echo "Built $(BINARY)-amd64"

# Build all cross-compile targets in parallel (#152)
build-all:
	$(MAKE) -j3 build-arm64 build-arm32 build-amd64

# ── Test ───────────────────────────────────────────────────────────────────
test:
	go test -count=1 -timeout 120s ./...

test-verbose:
	CGO_ENABLED=0 go test ./... -v -count=1

test-race:
	go test -race -count=1 -timeout 120s ./...

# Run only tests matching a pattern: make test-run T=TestDecodePDU
test-run:
	CGO_ENABLED=0 go test ./... -run $(T) -v -count=1

# ── E2E test against modem-emu (#118) ─────────────────────────────────────
e2e:
	docker compose up -d --build --wait
	go test -tags=e2e -timeout 60s ./e2e/... -v
	docker compose down

# ── Benchmarks ─────────────────────────────────────────────────────────────
bench:
	go test -bench=. -benchmem -run=^$$ ./...

# ── Code quality ───────────────────────────────────────────────────────────
fmt:
	gofmt -w -s .

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
	  { echo "golangci-lint not found; install from https://golangci-lint.run/"; exit 1; }
	golangci-lint run ./...

tidy:
	go mod tidy

# check runs all quality gates in sequence (#216)
check: fmt vet test

# pre-commit runs gofmt + go vet as a lightweight pre-commit check (#216)
pre-commit: fmt vet
	@echo "Pre-commit checks passed"

# ── Docker ─────────────────────────────────────────────────────────────────
docker:
	docker build -t sms-gate .

# ── Install ────────────────────────────────────────────────────────────────

# Install binary + systemd unit.
# Usage: sudo make install
install: build
	install -m 755 $(BINARY) $(INSTALL_BIN)/sms-gate
	@echo "Installed $(INSTALL_BIN)/sms-gate"

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
	install -m 644 deployments/sms-gate.service $(SYSTEMD_UNIT)/sms-gate.service
	systemctl daemon-reload
	@echo ""
	@echo "Installed systemd service. Next steps:"
	@echo "  1. Edit $(INSTALL_CONF)/config.yaml"
	@echo "  2. Set GATEWAY_TOKEN in $(INSTALL_CONF)/env"
	@echo "  3. systemctl enable --now sms-gate"

uninstall:
	-systemctl disable --now sms-gate 2>/dev/null || true
	-rm -f $(INSTALL_BIN)/sms-gate
	-rm -f $(SYSTEMD_UNIT)/sms-gate.service
	systemctl daemon-reload
	@echo "Uninstalled. Config left in $(INSTALL_CONF) — remove manually if desired."

# ── Utility ────────────────────────────────────────────────────────────────
clean:
	rm -rf bin

# Quick deploy to a remote Pi: make deploy HOST=pi@raspberrypi.local ARCH=arm64
deploy: build-$(ARCH)
	scp $(BINARY)-$(ARCH) $(HOST):/tmp/sms-gate.new
	ssh $(HOST) "mv /tmp/sms-gate.new $(INSTALL_BIN)/sms-gate && systemctl restart sms-gate"
	@echo "Deployed to $(HOST)"

help:
	@echo "sms-gate build targets"
	@echo ""
	@echo "  make build           Native build (current OS/arch)"
	@echo "  make build-arm64     Cross-compile for Raspberry Pi 4/5"
	@echo "  make build-arm32     Cross-compile for Raspberry Pi 2/3"
	@echo "  make build-all       All three targets (parallel)"
	@echo ""
	@echo "  make test            Run all tests"
	@echo "  make test-race       Run tests with race detector"
	@echo "  make test-run T=X    Run tests matching pattern X"
	@echo "  make bench           Run benchmarks"
	@echo "  make e2e             E2E test against modem-emu (Docker)"
	@echo ""
	@echo "  make check           Run fmt + vet + test"
	@echo "  make pre-commit      Run gofmt + go vet"
	@echo ""
	@echo "  make install-service Install binary + config + systemd unit"
	@echo "  make deploy HOST=... ARCH=...  One-shot remote deploy"
	@echo ""
	@echo "  make clean           Remove build artefacts"
