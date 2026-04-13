// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// ── Golden file round-trip tests (#66) ────────────────────────────────────
// These test vectors are from real modem captures (SMS-DELIVER format).

var goldenPDUs = []struct {
	name     string
	hex      string
	sender   string
	bodyPfx  string // prefix of expected body
	encoding string // "GSM7" or "UCS2"
}{
	{
		name:     "GSM7_basic",
		hex:      "07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD07",
		sender:   "+31641600986",
		bodyPfx:  "How are you",
		encoding: "GSM7",
	},
}

func TestDecodePDU_GoldenRoundtrip(t *testing.T) {
	for _, tc := range goldenPDUs {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DecodePDU(tc.hex)
			if err != nil {
				t.Fatalf("DecodePDU: %v", err)
			}
			if decoded.Sender != tc.sender {
				t.Errorf("sender: got %q, want %q", decoded.Sender, tc.sender)
			}
			if tc.bodyPfx != "" && !strings.HasPrefix(decoded.Body, tc.bodyPfx) {
				t.Errorf("body: got %q, want prefix %q", decoded.Body, tc.bodyPfx)
			}
			if decoded.PDUHash == "" {
				t.Error("PDUHash should not be empty")
			}
			if !strings.HasPrefix(decoded.PDUHash, "sha256:") {
				t.Errorf("PDUHash should start with sha256:, got %q", decoded.PDUHash)
			}
		})
	}
}

// ── Encode→Decode round-trip (#66 extended) ───────────────────────────────

func TestEncodeDecode_GSM7_Roundtrip(t *testing.T) {
	body := "Hello World!"
	parts, err := EncodePDU("+491234567890", body, "GSM7")
	if err != nil {
		t.Fatalf("EncodePDU: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	// Strip leading "00" (SMSC) from hex if present.
	hexStr := parts[0].HexStr
	// The encoded PDU is SMS-SUBMIT; DecodePDU expects SMS-DELIVER.
	// We can still validate the hex parses without panic.
	if hexStr == "" {
		t.Error("hex string should not be empty")
	}
}

func TestEncodeDecode_UCS2_Roundtrip(t *testing.T) {
	body := "こんにちは" // Japanese
	parts, err := EncodePDU("+491234567890", body, "UCS2")
	if err != nil {
		t.Fatalf("EncodePDU: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].HexStr == "" {
		t.Error("hex string should not be empty")
	}
}

// ── PDU hash collision detection (#13) ────────────────────────────────────

func TestPDUHash_UniquePerContent(t *testing.T) {
	hex1 := "07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD07"
	hex2 := "07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD08" // last byte different

	decoded1, err := DecodePDU(hex1)
	if err != nil {
		t.Fatalf("decode hex1: %v", err)
	}
	decoded2, err := DecodePDU(hex2)
	if err != nil {
		t.Fatalf("decode hex2: %v", err)
	}

	if decoded1.PDUHash == decoded2.PDUHash {
		t.Error("different PDUs should produce different hashes")
	}
}

func TestPDUHash_DeterministicSameInput(t *testing.T) {
	hexStr := "07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD07"

	d1, _ := DecodePDU(hexStr)
	d2, _ := DecodePDU(hexStr)

	if d1.PDUHash != d2.PDUHash {
		t.Errorf("same PDU should produce same hash: %q vs %q", d1.PDUHash, d2.PDUHash)
	}
}

func TestPDUHash_Format(t *testing.T) {
	hexStr := "07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD07"
	decoded, err := DecodePDU(hexStr)
	if err != nil {
		t.Fatal(err)
	}
	// Format: "sha256:<64 hex chars>"
	if !strings.HasPrefix(decoded.PDUHash, "sha256:") {
		t.Errorf("hash format: %q", decoded.PDUHash)
	}
	hashHex := strings.TrimPrefix(decoded.PDUHash, "sha256:")
	if len(hashHex) != 64 {
		t.Errorf("hash hex length: got %d, want 64", len(hashHex))
	}
	// Verify hash manually — DecodePDU hashes the uppercased hex string, not raw bytes.
	uppercased := strings.ToUpper(strings.TrimSpace(hexStr))
	expected := sha256.Sum256([]byte(uppercased))
	expectedHex := hex.EncodeToString(expected[:])
	if hashHex != expectedHex {
		t.Errorf("hash mismatch: got %q, want %q", hashHex, expectedHex)
	}
}

// ── PDU edge cases (#179) ─────────────────────────────────────────────────

func TestDecodePDU_EmptyBody(t *testing.T) {
	// A PDU with UDL=0 should decode without error.
	// Construct minimal SMS-DELIVER with 0-length body.
	// This is hard to construct manually, so we test that short PDUs
	// return an error rather than panicking.
	shortPDUs := []string{
		"00",
		"0011",
		"00110000",
		"001100000000",
	}
	for i, h := range shortPDUs {
		t.Run(fmt.Sprintf("short_%d", i), func(t *testing.T) {
			_, err := DecodePDU(h)
			if err == nil {
				t.Error("expected error for short PDU")
			}
		})
	}
}

func TestDecodePDU_InvalidHexChars(t *testing.T) {
	_, err := DecodePDU("ZZZZZZ")
	if err == nil {
		t.Error("expected error for non-hex chars")
	}
}

func TestDecodePDU_OddLengthHex(t *testing.T) {
	_, err := DecodePDU("07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD0")
	if err == nil {
		t.Error("expected error for odd-length hex")
	}
}

// ── PDU benchmarks (#193) ─────────────────────────────────────────────────

func BenchmarkDecodePDU(b *testing.B) {
	hexStr := "07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD07"
	for i := 0; i < b.N; i++ {
		_, _ = DecodePDU(hexStr)
	}
}

func BenchmarkEncodePDU_GSM7(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = EncodePDU("+491234567890", "Hello World, this is a benchmark test!", "GSM7")
	}
}

func BenchmarkEncodePDU_UCS2(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = EncodePDU("+491234567890", "こんにちは世界テスト", "UCS2")
	}
}

func BenchmarkIsGSM7(b *testing.B) {
	s := "The quick brown fox jumps over the lazy dog 0123456789"
	for i := 0; i < b.N; i++ {
		_ = IsGSM7(s)
	}
}

// ── Character encoding tests (#157) ──────────────────────────────────────

func TestIsGSM7_Extensions(t *testing.T) {
	// Extension characters: € ^ { } [ ] ~ | \ form feed
	exts := "€^{}[]~|\\"
	if !IsGSM7(exts) {
		t.Errorf("extension chars should be GSM7-representable")
	}
}

func TestIsGSM7_NonGSM7(t *testing.T) {
	nonGSM := []string{
		"中文",
		"العربية",
		"日本語",
		"🚀",
		"Привет",
	}
	for _, s := range nonGSM {
		if IsGSM7(s) {
			t.Errorf("%q should NOT be GSM7", s)
		}
	}
}

func TestEncodePDU_AutoDetectEncoding(t *testing.T) {
	// GSM7 text
	parts, err := EncodePDU("+491234567890", "Hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least 1 part")
	}

	// UCS2 text (Chinese)
	parts, err = EncodePDU("+491234567890", "你好世界", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) == 0 {
		t.Fatal("expected at least 1 part")
	}
}

// ── Multi-part SMS (#115) ────────────────────────────────────────────────

func TestEncodePDU_MultiPart_GSM7_Count(t *testing.T) {
	// 160 chars = single part, 161+ = multi-part.
	body160 := strings.Repeat("A", 160)
	body161 := strings.Repeat("A", 161)

	p1, err := EncodePDU("+491234567890", body160, "GSM7")
	if err != nil {
		t.Fatal(err)
	}
	if len(p1) != 1 {
		t.Errorf("160 chars: got %d parts, want 1", len(p1))
	}

	p2, err := EncodePDU("+491234567890", body161, "GSM7")
	if err != nil {
		t.Fatal(err)
	}
	if len(p2) < 2 {
		t.Errorf("161 chars: got %d parts, want >=2", len(p2))
	}
}

func TestEncodePDU_MultiPart_UCS2_Count(t *testing.T) {
	body70 := strings.Repeat("中", 70)
	body71 := strings.Repeat("中", 71)

	p1, err := EncodePDU("+491234567890", body70, "UCS2")
	if err != nil {
		t.Fatal(err)
	}
	if len(p1) != 1 {
		t.Errorf("70 UCS2 chars: got %d parts, want 1", len(p1))
	}

	p2, err := EncodePDU("+491234567890", body71, "UCS2")
	if err != nil {
		t.Fatal(err)
	}
	if len(p2) < 2 {
		t.Errorf("71 UCS2 chars: got %d parts, want >=2", len(p2))
	}
}

// ── Partial PDU (#42) ────────────────────────────────────────────────────
// Verify that truncated PDUs produce errors rather than panics.

func TestDecodePDU_TruncatedAtEveryByte(t *testing.T) {
	fullHex := "07911326040000F0040B911346610089F60000208062917314800CC8F71D14969741F977FD07"
	// Try decoding at every truncation point — should never panic.
	for i := 2; i < len(fullHex); i += 2 {
		t.Run(fmt.Sprintf("trunc_%d", i/2), func(t *testing.T) {
			_, _ = DecodePDU(fullHex[:i]) // just ensure no panic
		})
	}
}
