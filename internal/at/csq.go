// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"fmt"
	"strings"
)

// ParseCSQ converts a raw AT+CSQ response line (e.g. "+CSQ: 18,0") to RSSI in
// dBm using the standard formula: RSSI = -113 + 2*CSQ.
//
// The special CSQ value 99 means "not detectable" and maps to -113 dBm
// (matching the existing SignalQuality() behaviour).
//
// Returns an error if the line does not start with "+CSQ:" or contains an
// unparseable value.
func ParseCSQ(line string) (int, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "+CSQ:") {
		return 0, fmt.Errorf("at: ParseCSQ: expected line starting with \"+CSQ:\", got %q", line)
	}

	var csq, ber int
	value := strings.TrimSpace(strings.TrimPrefix(line, "+CSQ:"))
	n, err := fmt.Sscanf(value, "%d,%d", &csq, &ber)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("at: ParseCSQ: cannot parse CSQ value from %q: %w", line, err)
	}

	if csq < 0 || (csq > 31 && csq != 99) {
		return 0, fmt.Errorf("at: ParseCSQ: CSQ value %d out of range [0,31] or 99", csq)
	}

	if csq == 99 {
		return -113, nil // unknown / not detectable
	}
	return -113 + csq*2, nil
}
