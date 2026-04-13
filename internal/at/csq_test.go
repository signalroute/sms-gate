// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at_test

import (
	"testing"

	"github.com/signalroute/sms-gate/internal/at"
)

// TestParseCSQ_ValidValues verifies correct dBm conversion for standard CSQ values.
func TestParseCSQ_ValidValues(t *testing.T) {
	cases := []struct {
		line    string
		wantDBm int
	}{
		{"+CSQ: 0,0", -113},  // minimum
		{"+CSQ: 1,0", -111},
		{"+CSQ: 18,0", -77},  // -113 + 18*2 = -77
		{"+CSQ: 31,0", -51},  // maximum measurable
		{"+CSQ: 99,0", -113}, // not detectable → -113
		{"+CSQ: 0,7", -113},  // BER=7 (unused) still works
		{"+CSQ:18,0", -77},   // no space after colon
		{"  +CSQ: 10,1  ", -93}, // leading/trailing whitespace
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.line, func(t *testing.T) {
			got, err := at.ParseCSQ(tc.line)
			if err != nil {
				t.Fatalf("ParseCSQ(%q) error: %v", tc.line, err)
			}
			if got != tc.wantDBm {
				t.Errorf("ParseCSQ(%q) = %d dBm, want %d", tc.line, got, tc.wantDBm)
			}
		})
	}
}

// TestParseCSQ_InvalidPrefix verifies that lines without "+CSQ:" return an error.
func TestParseCSQ_InvalidPrefix(t *testing.T) {
	cases := []string{
		"",
		"OK",
		"ERROR",
		"+CREG: 1",
		"CSQ: 18,0", // missing leading '+'
	}
	for _, line := range cases {
		line := line
		t.Run(line, func(t *testing.T) {
			_, err := at.ParseCSQ(line)
			if err == nil {
				t.Errorf("ParseCSQ(%q) expected error, got nil", line)
			}
		})
	}
}

// TestParseCSQ_OutOfRange verifies that out-of-range CSQ values return an error.
func TestParseCSQ_OutOfRange(t *testing.T) {
	cases := []string{
		"+CSQ: -1,0",
		"+CSQ: 32,0",  // 32 is not a valid CSQ value
		"+CSQ: 98,0",  // 98 is not a valid CSQ value
		"+CSQ: 100,0", // above 99
	}
	for _, line := range cases {
		line := line
		t.Run(line, func(t *testing.T) {
			_, err := at.ParseCSQ(line)
			if err == nil {
				t.Errorf("ParseCSQ(%q) expected error for out-of-range value, got nil", line)
			}
		})
	}
}

// TestParseCSQ_AllValidCSQValues exercises every valid CSQ value (0–31 and 99).
func TestParseCSQ_AllValidCSQValues(t *testing.T) {
	for csq := 0; csq <= 31; csq++ {
		csq := csq
		t.Run("", func(t *testing.T) {
			line := formatCSQ(csq, 0)
			got, err := at.ParseCSQ(line)
			if err != nil {
				t.Fatalf("CSQ=%d: unexpected error: %v", csq, err)
			}
			want := -113 + csq*2
			if got != want {
				t.Errorf("CSQ=%d → got %d dBm, want %d", csq, got, want)
			}
		})
	}

	// CSQ=99 → -113 dBm
	t.Run("csq99", func(t *testing.T) {
		got, err := at.ParseCSQ("+CSQ: 99,0")
		if err != nil {
			t.Fatalf("CSQ=99: unexpected error: %v", err)
		}
		if got != -113 {
			t.Errorf("CSQ=99 → got %d dBm, want -113", got)
		}
	})
}

func formatCSQ(csq, ber int) string {
	return "+CSQ: " + itoa(csq) + "," + itoa(ber)
}

// itoa is a minimal int-to-string for the test file to avoid importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
