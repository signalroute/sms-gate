# Changelog

All notable changes to sms-gate are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased] — v2.0.0

### Added

- **Disk-backed queue** (`internal/diskqueue`): SQLite WAL-mode buffer that persists undelivered SMS across gateway restarts, preventing message loss during network outages or power cycles.
- **Dead-letter queue** (`internal/dlq`): Bounded in-memory DLQ for tasks that have exhausted all retry attempts; prevents runaway retry loops from blocking the main worker queue.
- **Sliding-window rate limiter** (`internal/ratelimit`): Three independent rate windows (per-minute / per-hour / per-day) per modem SIM, preventing carrier bans on high-volume SIMs.
- **mTLS support** (`internal/tlsconfig`): Client certificate and CA pinning for the WebSocket tunnel; configured via `CLOUD_TLS_CERT`, `CLOUD_TLS_KEY`, `CLOUD_TLS_CA` environment variables.
- **Circuit breaker** (`internal/breaker`): Per-modem circuit breaker that stops routing tasks to a modem in persistent failure, allowing automatic recovery without manual intervention.
- **SIGHUP hot-reload**: The gateway re-reads `config.yaml` on SIGHUP without dropping in-flight messages or WebSocket connections.
- **Prometheus metrics** (`internal/metrics`): Full instrumentation including SMS counters, modem state, signal RSSI, tunnel reconnect rate, AT command latency histogram, and queue depth gauge.
- **Structured logging**: JSON-formatted logs via `log/slog` with journald integration; configurable level and format.
- **Modem reconnect**: Automatic exponential-backoff reconnection per modem worker with jitter.
- **SMS deduplication** (`internal/dedup`): PDU hash-based dedup prevents duplicate delivery on reconnect replay.
- **Retry backoff** (`internal/backoff`): Generic exponential backoff with configurable base, ceiling, and jitter.
- **Docker Compose dev environment**: `docker-compose.yml` wires sms-gate with modem-emu for zero-hardware local development.
- **Systemd service unit** (`deployments/go-sms-gate.service`): Hardened unit with `DynamicUser`, `ProtectSystem=strict`, and `RestartSec` for production deployments.
- **CI pipeline** (`.github/workflows/ci.yml`): Lint, test-race, and cross-compile matrix (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64) on every push and PR.
- **Webhook relay** (`internal/webhook`): Optional outbound HTTP POST relay for SMS_RECEIVED events to local services.
- **Signal strength tracking**: Periodic `AT+CSQ` polling with Prometheus gauge export per ICCID.
- **E2E smoke test** (`e2e/smoke_test.go`): Integration test harness for live modem-emu sessions.

### Changed

- Module path updated to `github.com/signalroute/sms-gate`.
- `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`) replaces `mattn/go-sqlite3`; enables clean cross-compilation to ARM without a C toolchain.
- Config file schema rewritten: top-level sections `gateway`, `tunnel`, `buffer`, `modems`, `health`, `metrics` replace the flat v1 schema.
- Environment variable overrides now take precedence over config file values without requiring `${VAR}` syntax in YAML.

### Fixed

- SQLite-before-SIM-delete ordering: the buffer row is WAL-committed before `AT+CMGD` deletes the PDU from SIM storage, eliminating the rare message-loss window present in v1.
- Worker goroutine leak on modem disconnect: workers now drain their inbound channel before exiting.
- Race condition in tunnel reconnect path: `sync.Mutex` guards the send channel reference during reconnection.

---

## [1.x] — Legacy

The v1 series is no longer maintained. Users should migrate to v2.0.0.

[Unreleased]: https://github.com/signalroute/sms-gate/compare/main...HEAD
