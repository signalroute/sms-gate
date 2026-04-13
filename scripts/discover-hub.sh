#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (C) 2026 Signalroute
#
# discover-hub.sh — Discover all serial ports on a USB hub and map to modems.
# Usage: ./scripts/discover-hub.sh [--json]
#
# Walks the USB bus topology to group ttyUSB ports by parent hub.

set -euo pipefail

JSON_OUTPUT=false
if [[ "${1:-}" == "--json" ]]; then
    JSON_OUTPUT=true
fi

declare -A HUB_PORTS  # hub_path -> comma-separated list of ttyUSB ports

for dev in /dev/ttyUSB*; do
    [[ -e "$dev" ]] || continue

    local_name=$(basename "$dev")
    sysdev=$(readlink -f "/sys/class/tty/$local_name/device" 2>/dev/null) || continue

    # Walk up to find the USB device (has idVendor).
    usbdev="$sysdev"
    while [[ -n "$usbdev" && ! -f "$usbdev/idVendor" ]]; do
        usbdev=$(dirname "$usbdev")
    done

    # The parent of the USB device is the hub (or root).
    hub_path=$(dirname "$usbdev")
    hub_name=$(basename "$hub_path")

    if [[ -n "${HUB_PORTS[$hub_name]+x}" ]]; then
        HUB_PORTS[$hub_name]="${HUB_PORTS[$hub_name]},$dev"
    else
        HUB_PORTS[$hub_name]="$dev"
    fi
done

if [[ ${#HUB_PORTS[@]} -eq 0 ]]; then
    if $JSON_OUTPUT; then
        echo "[]"
    else
        echo "No USB modem hubs detected."
    fi
    exit 0
fi

if $JSON_OUTPUT; then
    entries=()
    for hub in "${!HUB_PORTS[@]}"; do
        ports="${HUB_PORTS[$hub]}"
        port_array=$(echo "$ports" | tr ',' '\n' | awk '{printf "\"%s\",", $0}' | sed 's/,$//')
        entries+=("{\"hub\":\"$hub\",\"ports\":[$port_array]}")
    done
    IFS=','
    echo "[${entries[*]}]"
else
    for hub in "${!HUB_PORTS[@]}"; do
        echo "Hub: $hub"
        IFS=',' read -ra ports <<< "${HUB_PORTS[$hub]}"
        for p in "${ports[@]}"; do
            echo "  └── $p"
        done
        echo "  Total: ${#ports[@]} port(s)"
        echo ""
    done
fi
