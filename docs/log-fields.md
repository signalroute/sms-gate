# Log Fields Reference

sms-gate uses Go's `log/slog` structured logger. Every log entry includes
contextual key-value fields. This document lists the standard fields.

## Common Fields

| Field | Type | Description |
|---|---|---|
| `port` | string | Serial port path (e.g. `/dev/ttyUSB0`) |
| `baud` | int | Serial baud rate (e.g. `115200`) |
| `iccid` | string | SIM ICCID returned by AT+CCID |
| `imei` | string | Modem IMEI returned by AT+CGSN |
| `imsi` | string | SIM IMSI returned by AT+CIMI |
| `worker` | string | Worker identifier (`port@baud`) |

## AT Command Fields

| Field | Type | Description |
|---|---|---|
| `cmd` | string | AT command sent (e.g. `AT+CMGS`) |
| `raw` | string | Raw AT response line |
| `duration` | duration | Time taken for AT command execution |
| `timeout` | duration | Configured timeout for the command |
| `err` | string | Error message on AT failure |

## SMS Fields

| Field | Type | Description |
|---|---|---|
| `to` | string | Destination phone number (E.164) |
| `from` | string | Sender phone number |
| `pdu_len` | int | PDU byte length |
| `msg_ref` | int | CMGS message reference |
| `sms_index` | int | CMTI storage index |
| `body_len` | int | Decoded SMS body length |

## Tunnel Fields

| Field | Type | Description |
|---|---|---|
| `tunnel_url` | string | WebSocket tunnel endpoint URL |
| `reconnect` | int | Reconnection attempt counter |
| `backoff` | duration | Current backoff delay |
| `pending_acks` | int | Number of messages awaiting ACK |
| `outbox_len` | int | Current tunnel outbox length |

## Metrics / Health Fields

| Field | Type | Description |
|---|---|---|
| `signal_rssi` | int | Signal strength (0–31, 99=unknown) |
| `reg_status` | int | AT+CREG registration status code |
| `buffer_count` | int | SQLite buffer pending count |
| `version` | string | Binary version string |

## Config Fields

| Field | Type | Description |
|---|---|---|
| `config_path` | string | Path to loaded config file |
| `dry_run` | bool | True when `--dry-run` flag is set |
| `modem_count` | int | Number of configured modems |

## Error Context

| Field | Type | Description |
|---|---|---|
| `err` | string | Error message |
| `attempt` | int | Retry attempt number |
| `state` | string | Worker FSM state on error |
| `recovered` | bool | Whether the worker recovered |
