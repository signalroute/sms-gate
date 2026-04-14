// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// ── Package-level test data ────────────────────────────────────────────────

var testSenders = func() []string {
	s := make([]string, 50)
	for i := range s {
		s[i] = fmt.Sprintf("+491234%05d", i)
	}
	return s
}()

var testGSM7Bodies = func() []string {
	b := make([]string, 100)
	for i := range b {
		n := i + 1
		if n > 160 {
			n = 160
		}
		b[i] = strings.Repeat("A", n)
	}
	return b
}()

var testUCS2Bodies = func() []string {
	b := make([]string, 10)
	for i := range b {
		n := (i + 1) * 7 // 7, 14, 21, … 70 chars
		if n < 1 {
			n = 1
		}
		b[i] = strings.Repeat("Ä", n)
	}
	return b
}()

// ── TestEncodePDU_Table (50 × 100 = 5000 subtests) ────────────────────────

func TestEncodePDU_Table(t *testing.T) {
	for _, sender := range testSenders {
		sender := sender
		for _, body := range testGSM7Bodies {
			body := body
			name := sender + "/" + strconv.Itoa(len(body))
			t.Run(name, func(t *testing.T) {
				parts, err := EncodePDU(sender, body, "GSM7")
				if err != nil {
					t.Fatalf("EncodePDU(%q, body[%d], GSM7) error: %v", sender, len(body), err)
				}
				if len(parts) < 1 {
					t.Fatalf("expected >= 1 part, got 0")
				}
				if len(parts[0].HexStr)%2 != 0 {
					t.Errorf("HexStr length %d is odd", len(parts[0].HexStr))
				}
				if parts[0].Length <= 0 {
					t.Errorf("Length = %d, want > 0", parts[0].Length)
				}
			})
		}
	}
}

// ── TestEncodePDU_UCS2_Table (50 × 10 = 500 subtests) ────────────────────

func TestEncodePDU_UCS2_Table(t *testing.T) {
	for _, sender := range testSenders {
		sender := sender
		for _, body := range testUCS2Bodies {
			body := body
			name := sender + "/" + strconv.Itoa(len(body))
			t.Run(name, func(t *testing.T) {
				parts, err := EncodePDU(sender, body, "UCS2")
				if err != nil {
					t.Fatalf("EncodePDU(%q, body[%d], UCS2) error: %v", sender, len(body), err)
				}
				if len(parts) < 1 {
					t.Fatalf("expected >= 1 part, got 0")
				}
				if len(parts[0].HexStr)%2 != 0 {
					t.Errorf("HexStr length %d is odd", len(parts[0].HexStr))
				}
				if parts[0].Length <= 0 {
					t.Errorf("Length = %d, want > 0", parts[0].Length)
				}
			})
		}
	}
}

// ── TestBCDAddress_Comprehensive (200 subtests) ───────────────────────────

func TestBCDAddress_Comprehensive(t *testing.T) {
	// 100 international numbers
	for i := 0; i < 100; i++ {
		i := i
		number := fmt.Sprintf("+%015d", int64(i)+49000000000000)
		t.Run("intl/"+number, func(t *testing.T) {
			numDigits, addrType, data := encodeBCDAddress(number)
			got := decodeBCDAddress(data, numDigits, addrType)
			if got != number {
				t.Errorf("roundtrip(%q) = %q", number, got)
			}
			if addrType != 0x91 {
				t.Errorf("international addrType = 0x%02X, want 0x91", addrType)
			}
		})
	}
	// 100 domestic numbers
	for i := 0; i < 100; i++ {
		i := i
		number := fmt.Sprintf("0%010d", int64(i)+1000000000)
		t.Run("domestic/"+number, func(t *testing.T) {
			numDigits, addrType, data := encodeBCDAddress(number)
			got := decodeBCDAddress(data, numDigits, addrType)
			if got != number {
				t.Errorf("roundtrip(%q) = %q", number, got)
			}
			if addrType != 0x81 {
				t.Errorf("domestic addrType = 0x%02X, want 0x81", addrType)
			}
		})
	}
}

// ── TestParseCMTI_Comprehensive (500 subtests) ────────────────────────────

func TestParseCMTI_Comprehensive(t *testing.T) {
	for i := 0; i < 500; i++ {
		i := i
		t.Run(fmt.Sprintf("idx_%d", i), func(t *testing.T) {
			urc := fmt.Sprintf("+CMTI: \"SM\",%d", i)
			idx, err := ParseCMTI(urc)
			if err != nil {
				t.Fatalf("ParseCMTI(%q) error: %v", urc, err)
			}
			if idx != i {
				t.Errorf("ParseCMTI(%q) = %d, want %d", urc, idx, i)
			}
		})
	}
}

// ── TestParseCREG_Table (200+ subtests) ───────────────────────────────────

func TestParseCREG_Table(t *testing.T) {
	stats := []int{0, 1, 2, 3, 4, 5, 6, 7}
	ns := []int{0, 1, 2, 3, 4}

	// Single-field: "+CREG: <stat>" – 8 stats × 3 format variants = 24
	singleFormats := []string{"+CREG: %d", "+CREG:%d", "+CREG:  %d"}
	for _, stat := range stats {
		for _, format := range singleFormats {
			stat, format := stat, format
			urc := fmt.Sprintf(format, stat)
			name := fmt.Sprintf("single/stat%d/fmt%q", stat, format)
			t.Run(name, func(t *testing.T) {
				got := ParseCREG(urc)
				if got != stat {
					t.Errorf("ParseCREG(%q) = %d, want %d", urc, got, stat)
				}
			})
		}
	}

	// Dual-field: "+CREG: <n>,<stat>" – 8 stats × 5 ns × 6 format variants = 240
	dualFormats := []string{
		"+CREG: %d,%d",
		"+CREG:%d,%d",
		"+CREG: %d, %d",
		"+CREG:  %d,%d",
		"+CREG: %d , %d",
		"+CREG:  %d,  %d",
	}
	for _, stat := range stats {
		for _, n := range ns {
			for _, format := range dualFormats {
				stat, n, format := stat, n, format
				urc := fmt.Sprintf(format, n, stat)
				name := fmt.Sprintf("dual/n%d/stat%d/fmt%q", n, stat, format)
				t.Run(name, func(t *testing.T) {
					got := ParseCREG(urc)
					if got != stat {
						t.Errorf("ParseCREG(%q) = %d, want %d", urc, got, stat)
					}
				})
			}
		}
	}
}

// ── TestGSM7Codec_AllChars (126 subtests, skipping ESC at index 27) ───────

func TestGSM7Codec_AllChars(t *testing.T) {
	for i := 0; i < 128; i++ {
		i := i
		if i == 27 { // ESC – extension table prefix, not a standalone char
			continue
		}
		r := gsm7Charset[i]
		if r == 0 {
			continue
		}
		t.Run(fmt.Sprintf("idx_%d", i), func(t *testing.T) {
			s := string(r)
			codes := encodeGSM7Text(s)
			packed := packGSM7(codes)
			unpacked := unpackGSM7(packed, len(codes))
			got := decodeGSM7Text(unpacked)
			if got != s {
				t.Errorf("GSM7 codec roundtrip idx %d (%q): got %q", i, s, got)
			}
		})
	}
}

// ── TestUCS2Codec_Roundtrip (300 subtests) ────────────────────────────────

func TestUCS2Codec_Roundtrip(t *testing.T) {
	// Build 300 test strings covering Arabic, Greek, CJK, and mixed BMP chars.
	cases := make([]string, 0, 300)

	// Arabic letters U+0620–U+063F (32 chars), single and multi-char strings
	for i := 0; i < 50; i++ {
		r := rune(0x0620 + i%32)
		cases = append(cases, string(r))
	}
	for i := 0; i < 50; i++ {
		r1 := rune(0x0620 + i%32)
		r2 := rune(0x0621 + i%32)
		cases = append(cases, string([]rune{r1, r2, r1}))
	}

	// Greek letters U+0391–U+03C9
	for i := 0; i < 50; i++ {
		r := rune(0x0391 + i%57)
		cases = append(cases, string(r))
	}
	for i := 0; i < 50; i++ {
		r1 := rune(0x0391 + i%57)
		r2 := rune(0x03B1 + i%25)
		cases = append(cases, string([]rune{r1, r2}))
	}

	// CJK unified ideographs U+4E00–U+4E63 (100 chars)
	for i := 0; i < 50; i++ {
		r := rune(0x4E00 + i)
		cases = append(cases, string(r))
	}
	for i := 0; i < 50; i++ {
		r1 := rune(0x4E00 + i)
		r2 := rune(0x4E01 + i%99)
		cases = append(cases, string([]rune{r1, r2, r2, r1}))
	}

	for idx, s := range cases {
		idx, s := idx, s
		t.Run(fmt.Sprintf("case_%d", idx), func(t *testing.T) {
			encoded := encodeUCS2(s)
			got := decodeUCS2(encoded)
			if got != s {
				t.Errorf("UCS2 roundtrip[%d]: input %q, got %q", idx, s, got)
			}
		})
	}
}
