# Modem Hardware Guide

This document covers hardware selection, wiring, and compatibility for sms-gate.

## Tested Modems

| Modem | Chipset | Interface | PDU Mode | Multi-SMS | Notes |
|---|---|---|---|---|---|
| SIMCom SIM800L | SIM800 | UART/TTL | ✅ | ✅ | 2G only, cheap, widely available |
| SIMCom SIM7600E | Qualcomm MDM9207 | USB | ✅ | ✅ | 4G LTE Cat-1, recommended |
| SIMCom SIM7600G-H | Qualcomm MDM9207 | USB | ✅ | ✅ | Global 4G variant |
| Quectel EC25 | Qualcomm MDM9207 | USB | ✅ | ✅ | Mini PCIe, excellent Linux support |
| Quectel EG25-G | Qualcomm MDM9207 | USB | ✅ | ✅ | Popular in M.2 form factor |
| Quectel EC200A | Qualcomm | USB | ✅ | ✅ | Budget 4G Cat-1 |
| u-blox SARA-R4 | | USB | ✅ | ✅ | LTE-M / NB-IoT |
| Huawei MU709 | Balong | USB | ✅ | ✅ | Legacy, still common |

## Raspberry Pi Wiring

### USB Modems (recommended)

Most 4G modems expose a `/dev/ttyUSBx` AT command port when connected via USB.
A powered USB hub is recommended for >2 modems.

```
Pi USB port → USB Hub (powered) → Modem 1 (/dev/ttyUSB0, /dev/ttyUSB1, ...)
                                → Modem 2 (/dev/ttyUSB2, /dev/ttyUSB3, ...)
```

The AT command port is typically the **lowest-numbered** ttyUSB device for each
modem. Use `dmesg | grep ttyUSB` after plugging in to confirm.

### UART Modems (SIM800L)

For TTL-level UART modems on Raspberry Pi GPIO:

```
SIM800L TX  → Pi GPIO15 (RXD)
SIM800L RX  → Pi GPIO14 (TXD)
SIM800L GND → Pi GND
SIM800L VCC → External 3.7–4.2V supply (NOT the Pi 3.3V rail)
```

> **Warning**: SIM800L requires 2A peak current. Always use a separate power
> supply. The Pi's 3.3V rail cannot handle the GSM transmit bursts.

Configure in `config.yaml`:

```yaml
modems:
  - port: /dev/ttyAMA0   # or /dev/serial0
    baud: 115200
```

## udev Rules (Stable Port Names)

USB device paths can change across reboots. Use udev rules for stable naming:

```bash
# /etc/udev/rules.d/99-sms-modems.rules

# SIM7600 on USB port 1.2
SUBSYSTEM=="tty", ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="9001", \
  ATTRS{devpath}=="1.2", SYMLINK+="modem-sim7600"

# EC25 on USB port 1.3
SUBSYSTEM=="tty", ATTRS{idVendor}=="2c7c", ATTRS{idProduct}=="0125", \
  ATTRS{devpath}=="1.3", SYMLINK+="modem-ec25"
```

Then configure sms-gate with the symlink paths:

```yaml
modems:
  - port: /dev/modem-sim7600
    baud: 115200
  - port: /dev/modem-ec25
    baud: 115200
```

Reload rules: `sudo udevadm control --reload-rules && sudo udevadm trigger`

## Common Vendor USB IDs

| Vendor | Product | VID:PID |
|---|---|---|
| SIMCom SIM7600 | AT port | `1e0e:9001` |
| Quectel EC25 | AT port | `2c7c:0125` |
| Quectel EG25-G | AT port | `2c7c:0125` |
| Quectel EC200A | AT port | `2c7c:6026` |
| Huawei MU709 | AT port | `12d1:1001` |

## Troubleshooting

### Modem not detected

1. Check `dmesg | tail -20` for USB enumeration errors
2. Verify kernel module: `lsmod | grep option` (for `usb_serial_option`)
3. Try `minicom -D /dev/ttyUSB0 -b 115200` and type `AT` — should respond `OK`

### SIM not ready

1. Verify SIM is inserted correctly (check `AT+CPIN?` → should be `READY`)
2. Check signal strength: `AT+CSQ` (value 10–31 is usable, 99 = no signal)
3. Verify APN if needed: `AT+CGDCONT?`

### High AT command latency

1. Reduce baud rate to 9600 and test stability
2. Check for RF interference near antenna
3. Verify SIM is not locked (PIN required)
4. Use shorter USB cables (under 1m)
