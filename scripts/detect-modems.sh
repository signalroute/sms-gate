#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (C) 2026 Signalroute
#
# detect-modems.sh — Detect /dev/ttyUSB* devices and print modem info.
# Usage: ./scripts/detect-modems.sh [--json]

set -euo pipefail

JSON_OUTPUT=false
if [[ "${1:-}" == "--json" ]]; then
    JSON_OUTPUT=true
fi

# Known USB modem vendor IDs (decimal).
declare -A KNOWN_VENDORS=(
    [12d1]="Huawei"
    [2c7c]="Quectel"
    [1e0e]="Simcom"
    [05c6]="Qualcomm"
    [1199]="Sierra Wireless"
    [0e8d]="MediaTek"
    [2cb7]="Fibocom"
    [1bc7]="Telit"
    [04e8]="Samsung"
    [19d2]="ZTE"
)

detect_ports() {
    local found=0
    local json_entries=()

    for dev in /dev/ttyUSB*; do
        [[ -e "$dev" ]] || continue
        found=$((found + 1))

        local sysdev
        sysdev=$(readlink -f "/sys/class/tty/$(basename "$dev")/device" 2>/dev/null) || true

        local vendor_id="" product_id="" vendor_name="Unknown" driver="" iface=""

        if [[ -n "$sysdev" ]]; then
            # Walk up to the USB device level.
            local usbdev="$sysdev"
            while [[ -n "$usbdev" && ! -f "$usbdev/idVendor" ]]; do
                usbdev=$(dirname "$usbdev")
            done

            if [[ -f "$usbdev/idVendor" ]]; then
                vendor_id=$(cat "$usbdev/idVendor" 2>/dev/null)
                product_id=$(cat "$usbdev/idProduct" 2>/dev/null)
                vendor_name="${KNOWN_VENDORS[$vendor_id]:-Unknown ($vendor_id)}"
            fi

            driver=$(basename "$(readlink -f "$sysdev/driver" 2>/dev/null)" 2>/dev/null) || driver=""
            iface=$(cat "$sysdev/../bInterfaceNumber" 2>/dev/null) || iface=""
        fi

        if $JSON_OUTPUT; then
            json_entries+=("{\"port\":\"$dev\",\"vendor_id\":\"$vendor_id\",\"product_id\":\"$product_id\",\"vendor\":\"$vendor_name\",\"driver\":\"$driver\",\"interface\":\"$iface\"}")
        else
            echo "──────────────────────────────"
            echo "Port:      $dev"
            echo "Vendor:    $vendor_name"
            echo "VID:PID:   ${vendor_id}:${product_id}"
            echo "Driver:    ${driver:-n/a}"
            echo "Interface: ${iface:-n/a}"
        fi
    done

    if $JSON_OUTPUT; then
        local IFS=','
        echo "[${json_entries[*]:-}]"
    elif [[ $found -eq 0 ]]; then
        echo "No /dev/ttyUSB* devices found."
        echo ""
        echo "Troubleshooting:"
        echo "  1. Plug in a USB modem"
        echo "  2. Check dmesg: dmesg | grep -i 'ttyUSB\\|option\\|usb-serial'"
        echo "  3. Load the driver: sudo modprobe option"
        echo "  4. Verify udev rules: ls -la /dev/ttyUSB*"
        exit 1
    else
        echo "──────────────────────────────"
        echo "Total: $found port(s) detected"
    fi
}

detect_ports
