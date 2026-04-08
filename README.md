# go-sms-gate

A production-grade, headless Go daemon that multiplexes multiple 4G/LTE USB modems behind NAT. Inbound SMS messages (2FA codes, alerts) are pushed in real time to a central Cloud Server over a persistent encrypted WebSocket tunnel. The gateway exposes **zero inbound ports**.

Built against the [v2.0.0 wire protocol specification](docs/spec-v2.0.0.docx).

---

## Architecture

```
┌──────────────────────────── Cloud Server ─────────────────────────────────┐
│  PostgreSQL  ·  REST API  ·  Frontend  ·  WebSocket Server (:wss)         │
└────────────────────────────────┬──────────────────────────────────────────┘
                                 │  WSS (TLS 1.3) — outbound from gateway
┌──────────────────────── Local Gateway ────────────────────────────────────┐
│                                                                            │
│  Tunnel Manager ──── Heartbeat / HELLO / flush offline buffer             │
│       │                                                                    │
│  Task Router ──── map[ICCID → ModemWorker]                                │
│       │                                                                    │
│  Modem Workers ──── AT Serializer ──── /dev/ttyUSBn                       │
│       │                                                                    │
│  SQLite Buffer (WAL) ──── zero-loss SMS persistence during outages        │
└────────────────────────────────────────────────────────────────────────────┘
```

### Key design decisions

| Decision | Rationale |
|---|---|
| Outbound-only WSS | Works behind any NAT/CGNAT without port forwarding or dynamic DNS |
| Single reader goroutine per port | Serial ports are not multiplexable — prevents AT command / URC interleaving |
| SQLite-before-SIM-delete ordering | Message safe in WAL-committed row before the irreversible SIM deletion |
| `modernc.org/sqlite` (pure Go) | `CGO_ENABLED=0` — cross-compiles cleanly to ARM64/ARM32 |
| Sliding-window rate limiter | Three independent windows (per-min / per-hour / per-day) prevent carrier SIM bans |

---

## Requirements

- Go 1.22+
- Linux with USB modem access (`/dev/ttyUSBn`)
- Cloud Server with a WebSocket upgrade endpoint

---

## Quick Start

```bash
# 1. Clone and build for the current host
git clone https://github.com/yanujz/go-sms-gate
cd go-sms-gate
make build

# 2. Cross-compile for Raspberry Pi 4 (ARM64)
make build-arm64

# 3. Run tests
make test

# 4. Copy config and fill in your details
cp configs/config.example.yaml /etc/go-sms-gate/config.yaml
chmod 640 /etc/go-sms-gate/config.yaml
echo "GATEWAY_TOKEN=your_secret_here" > /etc/go-sms-gate/env
chmod 600 /etc/go-sms-gate/env

# 5. Install as a systemd service
sudo make install-service
sudo systemctl enable --now go-sms-gate
```

---

## Configuration

All options are documented in [`configs/config.example.yaml`](configs/config.example.yaml).

**Bearer token** — never put the raw token in `config.yaml`. Use `${ENV_VAR}` substitution and load it from an env file with mode 600:

```yaml
# config.yaml
tunnel:
  token: ${GATEWAY_TOKEN}
```

```bash
# /etc/go-sms-gate/env  (chmod 600)
GATEWAY_TOKEN=your_secret_bearer_token
```

---

## Wire Protocol

All communication over the WSS tunnel uses UTF-8 JSON frames. The full schema reference is in the specification document.

### Inbound Task (Cloud → Gateway)

```json
{
  "type":       "TASK",
  "message_id": "a1b2c3d4-...",
  "ts":         1712345678000,
  "action":     "SEND_SMS",
  "payload": {
    "iccid":    "89490200001234567890",
    "to":       "+4915112345678",
    "body":     "Your OTP is 391827",
    "encoding": "GSM7"
  }
}
```

Available actions: `SEND_SMS`, `REBOOT_MODEM`, `CHECK_SIGNAL`, `DELETE_ALL_SMS`.

### Outbound Event (Gateway → Cloud)

```json
{
  "type":        "SMS_RECEIVED",
  "message_id":  "f3e2d1c0-...",
  "ts":          1712345679123,
  "gateway_id":  "gw-bavaria-01",
  "iccid":       "89490200001234567890",
  "sender":      "+4915198765432",
  "body":        "Your code is 884721",
  "received_at": 1712345679050,
  "pdu_hash":    "sha256:a3f1...",
  "buffer_id":   42
}
```

The cloud **must** acknowledge every `SMS_RECEIVED` with `SMS_DELIVERED_ACK`. Unacknowledged rows are replayed on reconnect; the cloud must deduplicate on `pdu_hash`.

---

## USB Modem Permissions

The service runs under a dynamic user with `DynamicUser=yes`. Grant serial port access via udev:

```
# /etc/udev/rules.d/99-modems.rules
SUBSYSTEM=="tty", ATTRS{idVendor}=="12d1", MODE="0660", GROUP="dialout"
```

```bash
udevadm control --reload-rules && udevadm trigger
```

---

## Observability

### Logs

Structured JSON to journald:

```bash
journalctl -u go-sms-gate -f --output=json | jq .
```

### Prometheus metrics

Exposed on `127.0.0.1:9101/metrics` (loopback only):

| Metric | Type | Labels |
|---|---|---|
| `smsgate_sms_received_total` | Counter | `iccid` |
| `smsgate_sms_delivered_total` | Counter | `iccid` |
| `smsgate_sms_pending_count` | Gauge | — |
| `smsgate_sms_sent_total` | Counter | `iccid`, `status` |
| `smsgate_modem_signal_rssi` | Gauge | `iccid` |
| `smsgate_modem_state` | Gauge | `iccid` |
| `smsgate_tunnel_state` | Gauge | — |
| `smsgate_tunnel_reconnects_total` | Counter | — |
| `smsgate_at_cmd_duration_ms` | Histogram | `command` |

---

## Zero-Downtime Deployment

```bash
# Atomic binary swap — in-flight SMS are not lost (SQLite buffer survives restart)
make deploy HOST=pi@192.168.1.42 ARCH=arm64
```

The `deploy` target builds, copies, and restarts the service in one step. On restart the Tunnel Manager reconnects, sends HELLO with the new `agent_version`, and flushes any PENDING rows from the offline buffer automatically.

---

## License

GPL-3.0-or-later — see [LICENSE](LICENSE).
