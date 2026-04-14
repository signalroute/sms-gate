// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// pdu_hardening_test.go: regression tests for bounds-check fixes.
// Covers issues #192 (malformed length field panic), #167 (invalid UDH
// length panic), #6 (truncated AT response) and #168 (unknown GSM7
// extension codes).
package at

import (
	"strings"
	"testing"
)

// ── unpackGSM7 bounds ─────────────────────────────────────────────────────

// TestUnpackGSM7_NCharsClamped verifies that requesting more septets than the
// data can hold does NOT panic and returns at most (len(data)*8/7) chars.
func TestUnpackGSM7_NCharsClamped(t *testing.T) {
	data := []byte{0xAB, 0xCD} // 2 bytes → max 2 septets
	// Request 255 chars — should not panic.
	got := unpackGSM7(data, 255)
	maxExpected := (len(data) * 8) / 7
	if len(got) > maxExpected {
		t.Errorf("unpackGSM7 returned %d chars, expected ≤ %d", len(got), maxExpected)
	}
}

func TestUnpackGSM7_Empty(t *testing.T) {
	got := unpackGSM7([]byte{}, 0)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got len=%d", len(got))
	}
}

func TestUnpackGSM7_ExactFit(t *testing.T) {
	// Pack "Hello" (5 chars = 5 septets = 5 bytes packed)
	chars := encodeGSM7Text("Hello")
	packed := packGSM7(chars)
	unpacked := unpackGSM7(packed, len(chars))
	if len(unpacked) != len(chars) {
		t.Errorf("expected %d chars, got %d", len(chars), len(unpacked))
	}
}

func TestUnpackGSM7_NCharsZero(t *testing.T) {
	got := unpackGSM7([]byte{0xFF, 0xFF, 0xFF}, 0)
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

// ── DecodePDU hardening ───────────────────────────────────────────────────

// buildMinimalDeliverPDU constructs a minimal SMS-DELIVER PDU with controllable
// UDL and UD fields for testing truncation and overflow scenarios.
// SMSC=none, sender=+1234567890, PID=0x00, DCS=0x00 (GSM7), SCTS=zeros.
func buildMinimalDeliverPDU(udl byte, ud []byte) string {
	// SMSC length = 0 (no SMSC)
	// PDU type: MTI=00 (SMS-DELIVER), no UDHI
	// OA: 10 digits, 0x91 international, BCD 21436587F9
	// PID=0x00, DCS=0x00, SCTS=7 zero bytes
	pdu := make([]byte, 0, 19+len(ud))
	pdu = append(pdu,
		0x00,                         // SMSC len = 0
		0x00,                         // PDU type: SMS-DELIVER, no UDHI
		0x0A,                         // OA length: 10 digits
		0x91,                         // OA type: international
		0x21, 0x43, 0x65, 0x87, 0xF9, // BCD "+12345678F9" → +1234567890 (approx)
		0x00,                                     // PID
		0x00,                                     // DCS: GSM7
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // SCTS (7 bytes, zeros = invalid date but OK for test)
		udl, // UDL
	)
	pdu = append(pdu, ud...)
	return bytesToHex(pdu)
}

func hexNibble(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + n - 10
}

func bytesToHex(b []byte) string {
	const hexChars = "0123456789ABCDEF"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexChars[v>>4]
		out[i*2+1] = hexChars[v&0xF]
	}
	return string(out)
}

// TestDecodePDU_MalformedUDL_NoPanic verifies that a PDU with UDL claiming
// more characters than the UD bytes hold does not panic (issue #192).
func TestDecodePDU_MalformedUDL_NoPanic(t *testing.T) {
	// UDL=200 but UD only has 3 packed bytes (≈3 chars).
	pduHex := buildMinimalDeliverPDU(200, []byte{0xE8, 0x32, 0x9B})
	sms, err := DecodePDU(pduHex)
	// Should NOT panic; error is acceptable, as is a partial decode.
	if err != nil {
		t.Logf("DecodePDU returned error (acceptable): %v", err)
		return
	}
	if sms == nil {
		t.Error("expected non-nil SMS or error, got nil SMS with nil error")
	}
}

// TestDecodePDU_TruncatedAtPID_Error verifies that a PDU truncated at the PID
// byte returns an error rather than panicking (issue #6).
func TestDecodePDU_TruncatedAtPID_Error(t *testing.T) {
	// Build a PDU that ends right after the OA digits (no PID/DCS/SCTS/UDL).
	truncated := []byte{
		0x00,       // SMSC len = 0
		0x00,       // PDU type: SMS-DELIVER
		0x04,       // OA: 4 digits
		0x91,       // OA type: international
		0x12, 0x34, // 2 BCD bytes for 4 digits
		// Stops here — no PID, DCS, SCTS, UDL, UD
	}
	_, err := DecodePDU(bytesToHex(truncated))
	if err == nil {
		t.Error("expected error for truncated PDU at PID, got nil")
	}
}

// TestDecodePDU_InvalidUDHLength_Error verifies that a PDU with the UDHI bit
// set but an invalid UDH length byte returns an error (issue #167).
func TestDecodePDU_InvalidUDHLength_Error(t *testing.T) {
	// PDU type with UDHI bit (bit 6) set = 0x40.
	// UDL = 5, UD = [0xFF, ...] where UDH length 0xFF = 255 > len(UD).
	pdu := []byte{
		0x00,       // SMSC len = 0
		0x40,       // PDU type: SMS-DELIVER with UDHI=1
		0x04,       // OA: 4 digits
		0x91,       // OA type
		0x12, 0x34, // BCD
		0x00,                                     // PID
		0x00,                                     // DCS: GSM7
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // SCTS
		0x05,                         // UDL = 5
		0xFF, 0x01, 0x02, 0x03, 0x04, // UD[0]=0xFF → UDH len=255 but only 5 bytes
	}
	_, err := DecodePDU(bytesToHex(pdu))
	if err == nil {
		t.Error("expected error for invalid UDH length, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "UDH") {
		t.Logf("error message: %v", err) // acceptable; any error is fine
	}
}

// TestDecodePDU_UDHI_EmptyUD_Error verifies that UDHI bit set with empty UD
// returns an error (issue #167).
func TestDecodePDU_UDHI_EmptyUD_Error(t *testing.T) {
	pdu := []byte{
		0x00,       // SMSC len = 0
		0x40,       // UDHI set
		0x04,       // OA: 4 digits
		0x91,       // OA type
		0x12, 0x34, // BCD
		0x00,                                     // PID
		0x00,                                     // DCS
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // SCTS
		0x00, // UDL = 0 → empty UD
		// No UD bytes
	}
	_, err := DecodePDU(bytesToHex(pdu))
	if err == nil {
		t.Error("expected error for UDHI with empty UD, got nil")
	}
}

// TestDecodePDU_EmptyPDU_Error verifies that an empty PDU hex returns an error.
func TestDecodePDU_EmptyPDU_Error(t *testing.T) {
	_, err := DecodePDU("")
	if err == nil {
		t.Error("expected error for empty PDU, got nil")
	}
}

// TestDecodePDU_AllZeros_Error verifies that a single zero byte (SMSC=0, no
// more data) is rejected.
func TestDecodePDU_AllZeros_Error(t *testing.T) {
	_, err := DecodePDU("00")
	if err == nil {
		t.Error("expected error for single-byte PDU, got nil")
	}
}

// ── decodeGSM7Text unknown extension ─────────────────────────────────────

// TestDecodeGSM7Text_UnknownExtension verifies that an unknown extension code
// (ESC + unrecognized byte) produces a space rather than silently dropping the
// character (issue #168 related).
func TestDecodeGSM7Text_UnknownExtension(t *testing.T) {
	// Code sequence: [0x1B, 0x01] — ESC followed by unknown code 0x01.
	codes := []byte{0x1B, 0x01}
	got := decodeGSM7Text(codes)
	if got != " " {
		t.Errorf("expected space for unknown extension, got %q", got)
	}
}

// TestDecodeGSM7Text_TruncatedEscapeAtEnd verifies that ESC as the last byte
// is handled gracefully (no panic, empty or partial output).
func TestDecodeGSM7Text_TruncatedEscapeAtEnd(t *testing.T) {
	codes := []byte{0x1B} // bare ESC with no following byte
	got := decodeGSM7Text(codes)
	_ = got // any output (including empty string) is fine; no panic
}

// TestDecodeGSM7Text_AtSymbol verifies the '@' symbol (GSM7 code 0x00) roundtrips.
func TestDecodeGSM7Text_AtSymbol(t *testing.T) {
	original := "@Hello@"
	chars := encodeGSM7Text(original)
	packed := packGSM7(chars)
	unpacked := unpackGSM7(packed, len(chars))
	decoded := decodeGSM7Text(unpacked)
	if decoded != original {
		t.Errorf("'@' roundtrip: got %q, want %q", decoded, original)
	}
}

// TestDecodeGSM7Text_KnownExtensions verifies the full extension table roundtrip.
func TestDecodeGSM7Text_KnownExtensions(t *testing.T) {
	// All characters with known extension codes.
	chars := []rune{'{', '}', '[', ']', '\\', '~', '^', '|', '€', '\f'}
	for _, ch := range chars {
		t.Run(string(ch), func(t *testing.T) {
			codes := encodeGSM7Text(string(ch))
			if len(codes) != 2 {
				t.Fatalf("extension char %q encoded to %d code points, want 2", ch, len(codes))
			}
			if codes[0] != 0x1B {
				t.Errorf("expected ESC prefix (0x1B), got 0x%02X", codes[0])
			}
			decoded := decodeGSM7Text(codes)
			if decoded != string(ch) {
				t.Errorf("roundtrip: got %q, want %q", decoded, string(ch))
			}
		})
	}
}
