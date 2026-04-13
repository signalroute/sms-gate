# modem-emu Integration Guide

[modem-emu](https://github.com/signalroute/modem-emu) is a virtual AT modem
that emulates a real GSM modem over a PTY pair. It is the recommended way to
develop and test sms-gate without physical hardware.

## Quick Start

```bash
# Start modem-emu (creates a PTY pair)
docker run --rm -d --name modem-emu \
  -v /dev:/dev --privileged \
  ghcr.io/signalroute/modem-emu:latest

# modem-emu logs the PTY path on startup
docker logs modem-emu
# output: PTY master: /dev/pts/3

# Point sms-gate at the slave PTY
modems:
  - port: /dev/pts/4     # slave end
    baud: 115200
```

## Docker Compose

The easiest setup uses the provided `docker-compose.yml`:

```yaml
services:
  modem-emu:
    image: ghcr.io/signalroute/modem-emu:latest
    privileged: true
    volumes:
      - /dev:/dev

  sms-gate:
    build: .
    depends_on:
      - modem-emu
    volumes:
      - /dev:/dev
      - ./config.yaml:/etc/sms-gate/config.yaml:ro
```

## Supported AT Commands

modem-emu supports the full command set used by sms-gate's init sequence:

| Command | Purpose | Response |
|---|---|---|
| `AT` | Ping | `OK` |
| `ATE0` | Disable echo | `OK` |
| `AT+CMGF=0` | Set PDU mode | `OK` |
| `AT+CNMI=2,1,0,0,0` | Enable CMTI URCs | `OK` |
| `AT+CPIN?` | SIM status | `+CPIN: READY` |
| `AT+CSQ` | Signal strength | `+CSQ: 20,0` (configurable) |
| `AT+CREG?` | Registration | `+CREG: 0,1` |
| `AT+CGSN` | IMEI | Fixed test IMEI |
| `AT+CIMI` | IMSI | Fixed test IMSI |
| `AT+CCID` | ICCID | Fixed test ICCID |
| `AT+GCAP` | Capabilities | `+GCAP: +CGSM,+DS` |
| `AT+CPMS?` | Storage status | `+CPMS: "ME",0,50,"ME",0,50,"ME",0,50` |
| `AT+CMGS=N` | Send SMS (PDU) | `+CMGS: <ref>` |
| `AT+CMGR=N` | Read SMS | PDU response |
| `AT+CMGD=N` | Delete SMS | `OK` |
| `AT+CMGD=1,4` | Delete all SMS | `OK` |

## Injecting Inbound SMS

modem-emu can inject inbound SMS via its control API:

```bash
curl -X POST http://modem-emu:8080/inject \
  -H "Content-Type: application/json" \
  -d '{"from": "+15551234567", "body": "Hello from test"}'
```

This causes modem-emu to emit a `+CMTI: "ME",<index>` URC, which sms-gate
picks up and reads via `AT+CMGR`.

## E2E Testing

Run the full E2E suite with:

```bash
make e2e
```

This starts modem-emu + sms-gate in Docker, runs the Go E2E tests, and tears
down the containers.
