# Building sms-gate

This document covers cross-compilation targets, binary optimisation, and Docker builds.

## Prerequisites

- Go 1.23+
- `CGO_ENABLED=0` is used throughout — no C toolchain is required.

## Quick Builds

```bash
# Native (current OS/arch)
go build ./cmd/gateway

# Using make
make build
```

## Cross-Compilation Targets

### Linux amd64 (x86-64 servers, VMs, WSL)

```bash
GOOS=linux GOARCH=amd64 go build ./cmd/gateway
```

### Linux arm64 (Raspberry Pi 4 / Pi 5, AWS Graviton)

```bash
GOOS=linux GOARCH=arm64 go build ./cmd/gateway
```

### Linux arm — Raspberry Pi 32-bit (Pi 2 / Pi 3 with 32-bit OS)

```bash
GOOS=linux GOARCH=arm GOARM=7 go build ./cmd/gateway
```

### macOS (Apple Silicon and Intel)

```bash
go build ./cmd/gateway
```

For a universal binary targeting both architectures:

```bash
GOOS=darwin GOARCH=amd64 go build -o gateway-darwin-amd64 ./cmd/gateway
GOOS=darwin GOARCH=arm64 go build -o gateway-darwin-arm64 ./cmd/gateway
lipo -create -output gateway-darwin-universal gateway-darwin-amd64 gateway-darwin-arm64
```

### Windows (amd64)

```bash
GOOS=windows go build -o gateway.exe ./cmd/gateway
```

## All Targets in Parallel

```bash
make build-all
```

Output binaries are placed in `dist/`.

## Production-Optimised Binaries

Strip debug symbols and DWARF tables to reduce binary size:

```bash
go build -ldflags="-s -w" ./cmd/gateway
```

With version stamping (as used by `make build`):

```bash
VERSION=$(git describe --tags --always --dirty)
go build -ldflags="-s -w -X main.version=${VERSION}" ./cmd/gateway
```

## Optional: UPX Compression

[UPX](https://upx.github.io/) can further shrink the binary by ~60-70%. Install it from your package manager, then:

```bash
upx --best gateway
```

> **Note:** UPX decompresses on first execution (~100 ms). Avoid on latency-sensitive startup paths or very-low-RAM devices.

## Docker Multi-Stage Build

The included `Dockerfile` uses a multi-stage build:

1. **Builder stage** — compiles the binary with `CGO_ENABLED=0`.
2. **Runtime stage** — copies only the binary into a minimal `gcr.io/distroless/static` image.

```bash
# Build and tag
docker build -t sms-gate:latest .

# Cross-build for ARM64 on amd64 host (requires buildx)
docker buildx build --platform linux/arm64 -t sms-gate:arm64 .
```

## Verifying a Build

```bash
# Check linked libraries (should be empty for a static binary)
ldd ./gateway 2>&1 || file ./gateway

# Check embedded version
./gateway --version
```

## CI Matrix

The GitHub Actions CI builds the following matrix automatically on every push:

| GOOS | GOARCH |
|---|---|
| linux | amd64 |
| linux | arm64 |
| darwin | amd64 |
| darwin | arm64 |

See [`.github/workflows/ci.yml`](.github/workflows/ci.yml) for the full pipeline.
