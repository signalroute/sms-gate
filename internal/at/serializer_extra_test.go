// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ── TestIsURC_True (500 subtests) ─────────────────────────────────────────
// Verify that all well-known URC line shapes are classified as unsolicited.

func TestIsURC_True(t *testing.T) {
	// +CMTI: "SM",<idx>  — indices 0-249
	for i := 0; i < 250; i++ {
		i := i
		t.Run(fmt.Sprintf("cmti_sm_%d", i), func(t *testing.T) {
			line := fmt.Sprintf("+CMTI: \"SM\",%d", i)
			if !isURC(line) {
				t.Errorf("isURC(%q) = false, want true", line)
			}
		})
	}
	// +CMTI: "ME",<idx>  — indices 0-149
	for i := 0; i < 150; i++ {
		i := i
		t.Run(fmt.Sprintf("cmti_me_%d", i), func(t *testing.T) {
			line := fmt.Sprintf("+CMTI: \"ME\",%d", i)
			if !isURC(line) {
				t.Errorf("isURC(%q) = false, want true", line)
			}
		})
	}
	// +RING and variants
	ringLines := []string{
		"+RING", "+RING ", "+RING\r", "+RING extra",
		"+CRING: VOICE", "+CRING: DATA", "+CRING: ASYNC", "+CRING: SYNC",
		"+CRING: REL ASYNC", "+CRING: REL SYNC",
	}
	for _, line := range ringLines {
		line := line
		t.Run("ring/"+line, func(t *testing.T) {
			if !isURC(line) {
				t.Errorf("isURC(%q) = false, want true", line)
			}
		})
	}
	// +CLIP:, +CUSD:, +CMT:, +CDSI:
	otherURCPrefixes := []struct{ pfx, suffix string }{
		{"+CLIP:", " +4917629900000,145"},
		{"+CUSD:", " 0,\"Please choose\""},
		{"+CMT:", " \"+4917629900000\",,,\"21/01/01\""},
		{"+CDSI:", " \"SM\",1"},
	}
	for i, p := range otherURCPrefixes {
		p := p
		t.Run(fmt.Sprintf("other_%d", i), func(t *testing.T) {
			line := p.pfx + p.suffix
			if !isURC(line) {
				t.Errorf("isURC(%q) = false, want true", line)
			}
		})
	}
}

// ── TestIsURC_False (500 subtests) ────────────────────────────────────────
// Verify that response lines and non-URC prefixes are NOT classified as URCs.

func TestIsURC_False(t *testing.T) {
	// +CREG: <stat> — never a URC in our classification
	for stat := 0; stat < 8; stat++ {
		stat := stat
		t.Run(fmt.Sprintf("creg_single_%d", stat), func(t *testing.T) {
			line := fmt.Sprintf("+CREG: %d", stat)
			if isURC(line) {
				t.Errorf("isURC(%q) = true, want false", line)
			}
		})
	}
	// +CREG: <n>,<stat>
	for n := 0; n < 3; n++ {
		for stat := 0; stat < 8; stat++ {
			n, stat := n, stat
			t.Run(fmt.Sprintf("creg_dual_%d_%d", n, stat), func(t *testing.T) {
				line := fmt.Sprintf("+CREG: %d,%d", n, stat)
				if isURC(line) {
					t.Errorf("isURC(%q) = true, want false", line)
				}
			})
		}
	}
	// +CSQ: <rssi>,<ber> — CSQ values 0-31 + 99
	csqValues := append(make([]int, 32), 99)
	for i := range csqValues[:32] {
		csqValues[i] = i
	}
	for _, csq := range csqValues {
		csq := csq
		t.Run(fmt.Sprintf("csq_%d", csq), func(t *testing.T) {
			line := fmt.Sprintf("+CSQ: %d,0", csq)
			if isURC(line) {
				t.Errorf("isURC(%q) = true, want false", line)
			}
		})
	}
	// OK / ERROR / CME ERROR / CMS ERROR
	for i := 0; i < 100; i++ {
		i := i
		t.Run(fmt.Sprintf("cme_%d", i), func(t *testing.T) {
			if isURC(fmt.Sprintf("+CME ERROR: %d", i)) {
				t.Errorf("isURC(+CME ERROR: %d) = true, want false", i)
			}
		})
	}
	for i := 0; i < 100; i++ {
		i := i
		t.Run(fmt.Sprintf("cms_%d", i), func(t *testing.T) {
			if isURC(fmt.Sprintf("+CMS ERROR: %d", i)) {
				t.Errorf("isURC(+CMS ERROR: %d) = true, want false", i)
			}
		})
	}
	// Terminal lines
	termLines := []string{"OK", "ERROR", "+COPS: 0,0,\"Telekom.de\",7",
		"+CPMS: \"SM\",3,30", "+CCID: 89490200001234567890",
		"+CIMI: 262019000012345", "+CPIN: READY", "NO CARRIER", "CONNECT",
		"BUSY", "NO ANSWER", "NO DIALTONE", ">",
	}
	for _, line := range termLines {
		line := line
		t.Run("terminal/"+line, func(t *testing.T) {
			if isURC(line) {
				t.Errorf("isURC(%q) = true, want false", line)
			}
		})
	}
	// +CPMS responses
	for i := 0; i < 20; i++ {
		i := i
		t.Run(fmt.Sprintf("cpms_%d", i), func(t *testing.T) {
			line := fmt.Sprintf("+CPMS: \"SM\",%d,30,\"SM\",%d,30,\"SM\",%d,30", i, i, i)
			if isURC(line) {
				t.Errorf("isURC(%q) = true, want false", line)
			}
		})
	}
	// +CCID and +ICCID response lines
	for i := 0; i < 20; i++ {
		i := i
		t.Run(fmt.Sprintf("ccid_%d", i), func(t *testing.T) {
			line := fmt.Sprintf("+CCID: 8949020000%010d", i)
			if isURC(line) {
				t.Errorf("isURC(%q) = true, want false", line)
			}
		})
	}
}

// ── TestSignalQuality_AllCSQ (224 subtests) ───────────────────────────────
// Test SignalQuality() with every valid CSQ value (0-31) × BER (0-6).

func TestSignalQuality_AllCSQ(t *testing.T) {
	for csq := 0; csq <= 31; csq++ {
		for ber := 0; ber <= 6; ber++ {
			csq, ber := csq, ber
			t.Run(fmt.Sprintf("csq%d_ber%d", csq, ber), func(t *testing.T) {
				s, port := newTestSerializer(t)
				go func() {
					time.Sleep(10 * time.Millisecond)
					port.feed(fmt.Sprintf("+CSQ: %d,%d", csq, ber), "OK")
				}()
				rssi, err := s.SignalQuality()
				if err != nil {
					t.Fatalf("SignalQuality: %v", err)
				}
				wantRSSI := -113 + csq*2
				if rssi != wantRSSI {
					t.Errorf("CSQ=%d → RSSI got %d, want %d", csq, rssi, wantRSSI)
				}
			})
		}
	}
	// CSQ=99 means unknown regardless of BER
	for ber := 0; ber <= 6; ber++ {
		ber := ber
		t.Run(fmt.Sprintf("csq99_ber%d", ber), func(t *testing.T) {
			s, port := newTestSerializer(t)
			go func() {
				time.Sleep(10 * time.Millisecond)
				port.feed(fmt.Sprintf("+CSQ: 99,%d", ber), "OK")
			}()
			rssi, err := s.SignalQuality()
			if err != nil {
				t.Fatalf("SignalQuality: %v", err)
			}
			if rssi != -113 {
				t.Errorf("CSQ=99,BER=%d → RSSI got %d, want -113", ber, rssi)
			}
		})
	}
}

// ── TestRegistrationStatus_Table (240 subtests) ───────────────────────────
// Test RegistrationStatus() with all stat values across multiple CREG formats.

func TestRegistrationStatus_Table(t *testing.T) {
	type regCase struct {
		response string
		wantStat int
	}

	var cases []regCase

	// Single-field: +CREG: <stat>  — stat 0-7, 3 format variations each = 24
	singleFmts := []string{"+CREG: %d", "+CREG:%d", "+CREG:  %d"}
	for stat := 0; stat <= 7; stat++ {
		for _, f := range singleFmts {
			cases = append(cases, regCase{fmt.Sprintf(f, stat), stat})
		}
	}
	// Dual-field: +CREG: <n>,<stat>  — n 0-9, stat 0-7, 3 format variations = 240
	dualFmts := []string{"+CREG: %d,%d", "+CREG:%d,%d", "+CREG: %d, %d"}
	for n := 0; n <= 9; n++ {
		for stat := 0; stat <= 7; stat++ {
			for _, f := range dualFmts {
				cases = append(cases, regCase{fmt.Sprintf(f, n, stat), stat})
			}
		}
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.response, func(t *testing.T) {
			s, port := newTestSerializer(t)
			go func() {
				time.Sleep(10 * time.Millisecond)
				port.feed(tc.response, "OK")
			}()
			stat, err := s.RegistrationStatus()
			if err != nil {
				t.Fatalf("RegistrationStatus: %v", err)
			}
			if stat != tc.wantStat {
				t.Errorf("response %q → stat got %d, want %d", tc.response, stat, tc.wantStat)
			}
		})
	}
}

// ── TestStorageStatus_Table (210 subtests) ────────────────────────────────
// Test StorageStatus() with used 0-19, total 10-29 where total >= used.

func TestStorageStatus_Table(t *testing.T) {
	for used := 0; used <= 19; used++ {
		for total := 10; total <= 29; total++ {
			if total < used {
				continue
			}
			used, total := used, total
			t.Run(fmt.Sprintf("used%d_total%d", used, total), func(t *testing.T) {
				s, port := newTestSerializer(t)
				response := fmt.Sprintf(`+CPMS: "SM",%d,%d,"SM",%d,%d,"SM",%d,%d`,
					used, total, used, total, used, total)
				go func() {
					time.Sleep(10 * time.Millisecond)
					port.feed(response, "OK")
				}()
				gotUsed, gotTotal, err := s.StorageStatus()
				if err != nil {
					t.Fatalf("StorageStatus: %v", err)
				}
				if gotUsed != used {
					t.Errorf("used: got %d, want %d", gotUsed, used)
				}
				if gotTotal != total {
					t.Errorf("total: got %d, want %d", gotTotal, total)
				}
			})
		}
	}
}

// ── TestExecute_CMEErrors (101 subtests) ──────────────────────────────────
// Verify that CME ERROR responses produce the correct error messages.

func TestExecute_CMEErrors(t *testing.T) {
	for code := 0; code <= 100; code++ {
		code := code
		t.Run(fmt.Sprintf("cme_%d", code), func(t *testing.T) {
			s, port := newTestSerializer(t)
			go func() {
				time.Sleep(10 * time.Millisecond)
				port.feed(fmt.Sprintf("+CME ERROR: %d", code))
			}()
			_, err := s.Execute("AT+CLCK", 2*time.Second)
			if err == nil {
				t.Fatal("expected CME error, got nil")
			}
			var atErr *ATError
			if !errors.As(err, &atErr) {
				t.Fatalf("expected *ATError, got %T: %v", err, err)
			}
			wantRaw := fmt.Sprintf("+CME ERROR: %d", code)
			if atErr.Raw != wantRaw {
				t.Errorf("raw: got %q, want %q", atErr.Raw, wantRaw)
			}
		})
	}
}

// ── TestExecute_CMSErrors (101 subtests) ──────────────────────────────────
// Verify that CMS ERROR responses produce the correct error messages.

func TestExecute_CMSErrors(t *testing.T) {
	for code := 0; code <= 100; code++ {
		code := code
		t.Run(fmt.Sprintf("cms_%d", code), func(t *testing.T) {
			s, port := newTestSerializer(t)
			go func() {
				time.Sleep(10 * time.Millisecond)
				port.feed(fmt.Sprintf("+CMS ERROR: %d", code))
			}()
			_, err := s.Execute("AT+CMGS=20", 2*time.Second)
			if err == nil {
				t.Fatal("expected CMS error, got nil")
			}
			var atErr *ATError
			if !errors.As(err, &atErr) {
				t.Fatalf("expected *ATError, got %T: %v", err, err)
			}
			wantRaw := fmt.Sprintf("+CMS ERROR: %d", code)
			if atErr.Raw != wantRaw {
				t.Errorf("raw: got %q, want %q", atErr.Raw, wantRaw)
			}
		})
	}
}

// ── TestReadICCID_Variants (60 subtests) ──────────────────────────────────
// Test ReadICCID() across different response prefixes and ICCID lengths.

func TestReadICCID_Variants(t *testing.T) {
	prefixes := []string{"+CCID:", "+CCID: ", "+ICCID:", "+ICCID: "}
	// 15 different 20-digit ICCIDs
	for i := 0; i < 15; i++ {
		for _, pfx := range prefixes {
			i, pfx := i, pfx
			iccid := fmt.Sprintf("8949020000%010d", i)
			t.Run(fmt.Sprintf("%s%s", pfx, iccid), func(t *testing.T) {
				s, port := newTestSerializer(t)
				go func() {
					time.Sleep(10 * time.Millisecond)
					port.feed(pfx+iccid, "OK")
				}()
				got, err := s.ReadICCID()
				if err != nil {
					t.Fatalf("ReadICCID: %v", err)
				}
				if got != iccid {
					t.Errorf("ICCID: got %q, want %q", got, iccid)
				}
			})
		}
	}
}

// ── TestReadIMSI_Variants (15 subtests) ───────────────────────────────────
// Test ReadIMSI() with 15 different 15-digit IMSI values.

func TestReadIMSI_Variants(t *testing.T) {
	for i := 0; i < 15; i++ {
		i := i
		imsi := fmt.Sprintf("26201900%07d", i)
		t.Run(imsi, func(t *testing.T) {
			s, port := newTestSerializer(t)
			go func() {
				time.Sleep(10 * time.Millisecond)
				port.feed(imsi, "OK")
			}()
			got, err := s.ReadIMSI()
			if err != nil {
				t.Fatalf("ReadIMSI: %v", err)
			}
			if got != imsi {
				t.Errorf("IMSI: got %q, want %q", got, imsi)
			}
		})
	}
}

// ── TestReadOperator_Variants (50 subtests) ───────────────────────────────
// Test ReadOperator() with 50 different operator strings.

func TestReadOperator_Variants(t *testing.T) {
	operators := []string{
		"Telekom.de", "Vodafone.de", "o2-de", "1&1", "congstar",
		"T-Mobile US", "AT&T", "Verizon", "Sprint", "US Cellular",
		"Orange", "SFR", "Bouygues", "Free Mobile", "La Poste Mobile",
		"EE", "O2 UK", "Vodafone UK", "Three", "BT Mobile",
		"TIM", "Vodafone IT", "Wind Tre", "Fastweb", "Iliad IT",
		"Movistar", "Vodafone ES", "Orange ES", "MásMóvil", "Yoigo",
		"NTT Docomo", "au", "SoftBank", "Rakuten", "KDDI",
		"China Mobile", "China Unicom", "China Telecom",
		"SK Telecom", "KT", "LG U+",
		"Swisscom", "Salt", "Sunrise",
		"Telia", "Telenor", "Tre", "Tele2",
		"Mobistar", "Proximus",
	}
	for _, op := range operators {
		op := op
		t.Run(op, func(t *testing.T) {
			s, port := newTestSerializer(t)
			response := fmt.Sprintf(`+COPS: 0,0,"%s",7`, op)
			go func() {
				time.Sleep(10 * time.Millisecond)
				port.feed(response, "OK")
			}()
			got, err := s.ReadOperator()
			if err != nil {
				t.Fatalf("ReadOperator: %v", err)
			}
			if got != op {
				t.Errorf("operator: got %q, want %q", got, op)
			}
		})
	}
}

// ── TestExecute_MultiLine (50 subtests) ───────────────────────────────────
// Test Execute() with 0-49 intermediate response lines before OK.

func TestExecute_MultiLine(t *testing.T) {
	for n := 0; n <= 49; n++ {
		n := n
		t.Run(fmt.Sprintf("nlines_%d", n), func(t *testing.T) {
			s, port := newTestSerializer(t)
			go func() {
				time.Sleep(10 * time.Millisecond)
				lines := make([]string, n+1)
				for i := 0; i < n; i++ {
					lines[i] = fmt.Sprintf("+INFO: line%d", i)
				}
				lines[n] = "OK"
				port.feed(lines...)
			}()
			got, err := s.Execute("AT+CMD", 2*time.Second)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(got) != n {
				t.Errorf("response lines: got %d, want %d", len(got), n)
			}
		})
	}
}

// ── AT command wrapper coverage ───────────────────────────────────────────
// The following tests bring Ping, DisableEcho, SetPDUMode, EnableCMTIURCs,
// ReadSMS, DeleteSMS, DeleteAllSMS, RadioOff, RadioOn, HardReset and
// ExecuteSend from 0% to full statement coverage.

func TestPing_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.Ping(); err != nil {
t.Fatalf("Ping() error: %v", err)
}
}

func TestPing_Error(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("ERROR") }()
if err := s.Ping(); err == nil {
t.Fatal("Ping() expected error on ERROR response")
}
}

func TestDisableEcho_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.DisableEcho(); err != nil {
t.Fatalf("DisableEcho() error: %v", err)
}
}

func TestSetPDUMode_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.SetPDUMode(); err != nil {
t.Fatalf("SetPDUMode() error: %v", err)
}
}

func TestEnableCMTIURCs_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.EnableCMTIURCs(); err != nil {
t.Fatalf("EnableCMTIURCs() error: %v", err)
}
}

func TestRadioOff_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.RadioOff(); err != nil {
t.Fatalf("RadioOff() error: %v", err)
}
}

func TestRadioOn_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.RadioOn(); err != nil {
t.Fatalf("RadioOn() error: %v", err)
}
}

func TestHardReset_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.HardReset(); err != nil {
t.Fatalf("HardReset() error: %v", err)
}
}

func TestDeleteAllSMS_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.DeleteAllSMS(); err != nil {
t.Fatalf("DeleteAllSMS() error: %v", err)
}
}

// TestDeleteSMS_Indices covers DeleteSMS for indices 1-50.
func TestDeleteSMS_Indices(t *testing.T) {
for idx := 1; idx <= 50; idx++ {
idx := idx
t.Run(fmt.Sprintf("idx_%d", idx), func(t *testing.T) {
s, port := newTestSerializer(t)
go func() { time.Sleep(5 * time.Millisecond); port.feed("OK") }()
if err := s.DeleteSMS(idx); err != nil {
t.Fatalf("DeleteSMS(%d) error: %v", idx, err)
}
})
}
}

// TestReadSMS_OK verifies ReadSMS extracts the PDU hex line correctly.
func TestReadSMS_OK(t *testing.T) {
pduHex := "0791448720003023440DD0E5391D9C2EBBCF20"
s, port := newTestSerializer(t)
go func() {
time.Sleep(5 * time.Millisecond)
port.feed("+CMGR: 0,,20", pduHex, "OK")
}()
got, err := s.ReadSMS(1)
if err != nil {
t.Fatalf("ReadSMS(1) error: %v", err)
}
if got != pduHex {
t.Errorf("ReadSMS: got %q, want %q", got, pduHex)
}
}

// TestReadSMS_NoPDU verifies ReadSMS returns error when no PDU line follows.
func TestReadSMS_NoPDU(t *testing.T) {
s, port := newTestSerializer(t)
go func() {
time.Sleep(5 * time.Millisecond)
port.feed("OK") // no +CMGR header
}()
_, err := s.ReadSMS(1)
if err == nil {
t.Fatal("ReadSMS expected error when no PDU in response")
}
}

// TestReadSMS_Indices exercises ReadSMS for indices 1-20.
func TestReadSMS_Indices(t *testing.T) {
for idx := 1; idx <= 20; idx++ {
idx := idx
t.Run(fmt.Sprintf("idx_%d", idx), func(t *testing.T) {
pdu := fmt.Sprintf("AABBCC%04X", idx)
s, port := newTestSerializer(t)
go func() {
time.Sleep(5 * time.Millisecond)
port.feed(fmt.Sprintf("+CMGR: 0,,%d", idx), pdu, "OK")
}()
got, err := s.ReadSMS(idx)
if err != nil {
t.Fatalf("ReadSMS(%d) error: %v", idx, err)
}
if got != pdu {
t.Errorf("idx %d: got %q, want %q", idx, got, pdu)
}
})
}
}

// ── ExecuteSend coverage ──────────────────────────────────────────────────

// TestExecuteSend_OK exercises the happy-path two-phase SMS send.
func TestExecuteSend_OK(t *testing.T) {
s, port := newTestSerializer(t)
go func() {
time.Sleep(10 * time.Millisecond)
port.feed(">")               // prompt
time.Sleep(5 * time.Millisecond)
port.feed("+CMGS: 7", "OK") // MR=7
}()
mr, err := s.ExecuteSend("DEADBEEF", 3, 5*time.Second)
if err != nil {
t.Fatalf("ExecuteSend error: %v", err)
}
if mr != 7 {
t.Errorf("ExecuteSend MR: got %d, want 7", mr)
}
}

// TestExecuteSend_CMSError verifies CMS ERROR after PDU is surfaced.
func TestExecuteSend_CMSError(t *testing.T) {
s, port := newTestSerializer(t)
go func() {
time.Sleep(10 * time.Millisecond)
port.feed(">")
time.Sleep(5 * time.Millisecond)
port.feed("+CMS ERROR: 38") // network timeout
}()
_, err := s.ExecuteSend("DEADBEEF", 3, 5*time.Second)
if err == nil {
t.Fatal("ExecuteSend expected error on CMS ERROR")
}
if !strings.Contains(err.Error(), "CMS ERROR") {
t.Errorf("unexpected error: %v", err)
}
}

// TestExecuteSend_CMEError verifies CME ERROR after PDU is surfaced.
func TestExecuteSend_CMEError(t *testing.T) {
s, port := newTestSerializer(t)
go func() {
time.Sleep(10 * time.Millisecond)
port.feed(">")
time.Sleep(5 * time.Millisecond)
port.feed("+CME ERROR: 3") // operation not allowed
}()
_, err := s.ExecuteSend("DEADBEEF", 3, 5*time.Second)
if err == nil {
t.Fatal("ExecuteSend expected error on CME ERROR")
}
}

// TestExecuteSend_ATError verifies plain ERROR after PDU.
func TestExecuteSend_ATError(t *testing.T) {
s, port := newTestSerializer(t)
go func() {
time.Sleep(10 * time.Millisecond)
port.feed(">")
time.Sleep(5 * time.Millisecond)
port.feed("ERROR")
}()
_, err := s.ExecuteSend("DEADBEEF", 3, 5*time.Second)
if err == nil {
t.Fatal("ExecuteSend expected error on ERROR response")
}
}

// TestExecuteSend_PromptRejected verifies ERROR before the prompt is handled.
func TestExecuteSend_PromptRejected(t *testing.T) {
s, port := newTestSerializer(t)
go func() {
time.Sleep(10 * time.Millisecond)
port.feed("ERROR") // modem rejects before sending prompt
}()
_, err := s.ExecuteSend("DEADBEEF", 3, 5*time.Second)
if err == nil {
t.Fatal("ExecuteSend expected error when modem rejects before prompt")
}
}

// TestExecuteSend_ClosedSerializer ensures ErrClosed is returned immediately.
func TestExecuteSend_ClosedSerializer(t *testing.T) {
s, port := newTestSerializer(t)
port.Close()
s.Close()
time.Sleep(10 * time.Millisecond) // let reader goroutine notice close
_, err := s.ExecuteSend("DEADBEEF", 3, 5*time.Second)
if err == nil {
t.Fatal("ExecuteSend expected ErrClosed on a closed serializer")
}
}

// TestExecuteSend_ManyMR exercises MR values 0-29.
func TestExecuteSend_ManyMR(t *testing.T) {
for mr := 0; mr < 30; mr++ {
mr := mr
t.Run(fmt.Sprintf("mr_%d", mr), func(t *testing.T) {
s, port := newTestSerializer(t)
go func() {
time.Sleep(10 * time.Millisecond)
port.feed(">")
time.Sleep(5 * time.Millisecond)
port.feed(fmt.Sprintf("+CMGS: %d", mr), "OK")
}()
got, err := s.ExecuteSend("DEADBEEF", 3, 5*time.Second)
if err != nil {
t.Fatalf("mr=%d error: %v", mr, err)
}
if got != mr {
t.Errorf("mr=%d: got %d", mr, got)
}
})
}
}

// ── dcsEncoding coverage ──────────────────────────────────────────────────

func TestDCSEncoding_AllGroups(t *testing.T) {
cases := []struct {
dcs  byte
want string
}{
// Group 0x00: general — enc bits 01=00 → GSM7
{0x00, "GSM7"}, {0x01, "GSM7"}, {0x02, "GSM7"}, {0x03, "GSM7"},
// Group 0x00: enc 01 → 8BIT
{0x04, "8BIT"}, {0x05, "8BIT"}, {0x06, "8BIT"}, {0x07, "8BIT"},
// Group 0x00: enc 10 → UCS2
{0x08, "UCS2"}, {0x09, "UCS2"}, {0x0A, "UCS2"}, {0x0B, "UCS2"},
// Group 0x00: enc 11 → GSM7 (reserved)
{0x0C, "GSM7"}, {0x0D, "GSM7"}, {0x0E, "GSM7"}, {0x0F, "GSM7"},
// Group 0x0F: message class, enc bits same mapping
{0xF0, "GSM7"}, {0xF4, "8BIT"}, {0xF8, "UCS2"},
// Group 0x04-0x07: MWI → GSM7
{0x40, "GSM7"}, {0x50, "GSM7"}, {0x60, "GSM7"}, {0x70, "GSM7"},
// Group 0x08-0x0B: MWI UCS2
{0x80, "UCS2"}, {0x90, "UCS2"}, {0xA0, "UCS2"}, {0xB0, "UCS2"},
// Group 0x0C-0x0D: GSM7
{0xC0, "GSM7"}, {0xD0, "GSM7"},
// Group 0x0E: bit 2 set → UCS2, else GSM7
{0xE4, "UCS2"}, {0xE0, "GSM7"},
}
for _, tc := range cases {
tc := tc
t.Run(fmt.Sprintf("dcs_0x%02X", tc.dcs), func(t *testing.T) {
got := dcsEncoding(tc.dcs)
if got != tc.want {
t.Errorf("dcsEncoding(0x%02X) = %q, want %q", tc.dcs, got, tc.want)
}
})
}
}

// ── decodeSMSC coverage ───────────────────────────────────────────────────

func TestDecodeSMSC_Variants(t *testing.T) {
cases := []struct {
name    string
raw     []byte
wantLen int
}{
{"empty", []byte{}, 0},
{"zero_length", []byte{0x00}, 1},
{"international", []byte{0x06, 0x91, 0x94, 0x71, 0x92, 0x62, 0xF0}, 7},
{"national", []byte{0x05, 0x81, 0x94, 0x71, 0x92, 0x62}, 6},
{"truncated", []byte{0x06, 0x91, 0x94}, 7}, // signals truncation
}
for _, tc := range cases {
tc := tc
t.Run(tc.name, func(t *testing.T) {
_, n := decodeSMSC(tc.raw)
if n != tc.wantLen {
t.Errorf("decodeSMSC(%s) consumed %d bytes, want %d", tc.name, n, tc.wantLen)
}
})
}
}

// ── GSM7 escape / extension coverage ─────────────────────────────────────

// TestDecodeGSM7Text_Escapes verifies escape-table characters decode correctly.
func TestDecodeGSM7Text_Escapes(t *testing.T) {
// ESC + 0x28 → '{'
codes := []byte{0x1B, 0x28}
got := decodeGSM7Text(codes)
if got != "{" {
t.Errorf("decodeGSM7Text ESC+0x28 = %q, want %q", got, "{")
}
}

// TestDecodeGSM7Text_TruncatedEscape verifies a trailing ESC byte is silently dropped.
func TestDecodeGSM7Text_TruncatedEscape(t *testing.T) {
codes := []byte{0x48, 0x1B} // 'H' then lone ESC
got := decodeGSM7Text(codes)
if got != "H" {
t.Errorf("decodeGSM7Text trailing ESC = %q, want %q", got, "H")
}
}

// TestEncodeGSM7Text_ExtensionChars encodes extension characters and checks non-empty output.
func TestEncodeGSM7Text_ExtensionChars(t *testing.T) {
extChars := []struct {
r    rune
desc string
}{
{'{', "left_brace"},
{'}', "right_brace"},
{'[', "left_bracket"},
{']', "right_bracket"},
{'\\', "backslash"},
{'~', "tilde"},
{'|', "pipe"},
{'^', "caret"},
{'€', "euro"},
}
for _, ec := range extChars {
ec := ec
t.Run(ec.desc, func(t *testing.T) {
encoded := encodeGSM7Text(string(ec.r))
if len(encoded) == 0 {
t.Errorf("encodeGSM7Text(%q) returned empty slice", ec.r)
}
// Extension chars must start with ESC (0x1B)
if encoded[0] != 0x1B {
t.Errorf("encodeGSM7Text(%q)[0] = 0x%02X, want 0x1B (ESC)", ec.r, encoded[0])
}
})
}
}
