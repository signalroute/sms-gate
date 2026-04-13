# Contributing to sms-gate

Thank you for your interest in contributing! This document explains how to get started.

## Prerequisites

| Tool | Version | Purpose |
|---|---|---|
| [Go](https://go.dev/dl/) | 1.23+ | Build and test |
| `make` | any | Build automation |
| [Docker](https://docs.docker.com/get-docker/) | 24+ | Integration tests / modem-emu |
| [golangci-lint](https://golangci-lint.run/usage/install/) | latest | Linting |
| [pre-commit](https://pre-commit.com/#install) | latest | Git hooks (optional but recommended) |

## Building

```bash
# Native build
make build
# or
go build ./cmd/gateway

# All cross-compile targets
make build-all
```

See [BUILDING.md](BUILDING.md) for cross-compilation details.

## Running Tests

```bash
# All unit tests
make test
# or
go test ./...

# With race detector (required before merging)
make test-race
# or
go test -race ./...

# Run a specific test
make test-run T=TestDecodePDU
```

## Running with modem-emu

[modem-emu](https://github.com/signalroute/modem-emu) simulates one or more AT modems over a TCP socket, letting you develop without physical hardware.

```bash
# Start the modem emulator (see modem-emu README for config)
docker run --rm -p 7000:7000 ghcr.io/signalroute/modem-emu

# Point sms-gate at it
cat > /tmp/dev.yaml <<'EOF'
gateway:
  id: dev-gateway
tunnel:
  url: ws://localhost:8080/ws/gateway
  token: dev-token
modems:
  - port: 127.0.0.1:7000
    baud: 115200
EOF
./bin/sms-gate -config /tmp/dev.yaml
```

## Code Style

* All code is formatted with **gofmt** — run `gofmt -w .` before committing.
* Linting is enforced by **golangci-lint**: `make lint`.
* Static analysis: `make vet` (`go vet ./...`).
* Comments: only add comments that clarify non-obvious logic.

## Pre-commit Hooks (recommended)

Install [pre-commit](https://pre-commit.com/#install), then enable the hooks:

```bash
pip install pre-commit
pre-commit install
```

The hooks run `gofmt`, `go vet`, and `go test` automatically before every commit. Configuration is in [.pre-commit-config.yaml](.pre-commit-config.yaml).

## PR Workflow

1. **Fork** the repository and create a branch from `main`.
2. Branch naming convention:
   - `feat/<short-description>` — new features
   - `fix/<short-description>` — bug fixes
   - `docs/<short-description>` — documentation only
   - `chore/<short-description>` — maintenance / tooling
3. Keep commits focused; squash fixup commits before opening the PR.
4. Ensure `make test-race` passes locally.
5. Open a PR against `main` and fill in the PR template.
6. At least one maintainer review is required before merge.
7. PRs are **squash-merged** into main.

## Commit Messages

Use the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) format:

```
feat(buffer): add configurable retention_days per modem

Fixes #123
```

## License

By contributing you agree that your contributions will be licensed under the project's MIT License.
