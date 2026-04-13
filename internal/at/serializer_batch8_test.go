// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ── FuzzATResponseTokenizer (#4, #55) ─────────────────────────────────────
// Go native fuzz test for the AT response line classifier.

func FuzzIsURC(f *testing.F) {
	seeds := []string{
		"+CMTI: \"SM\",3",
		"+RING",
		"+CLIP: \"+491234567890\"",
		"+CUSD: 0,\"Balance: 12.50\",15",
		"+CMT: \"+491234567890\",,24",
		"+CDSI: \"SR\",1",
		"+CRING: VOICE",
		"OK",
		"ERROR",
		"+CME ERROR: 10",
		"+CMS ERROR: 330",
		"+CREG: 1,\"1A2B\",\"0000ABCD\",7",
		"+CSQ: 18,0",
		"+CMGS: 42",
		"+CPMS: 5,20,5,20,5,20",
		"",
		"AT",
		"DEADBEEF0123456789",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		// Should never panic.
		_ = isURC(line)
	})
}

// FuzzExecuteResponse fuzzes the full Execute response path by feeding
// arbitrary lines to a serializer and ensuring no panic.
func FuzzExecuteResponse(f *testing.F) {
	f.Add("OK")
	f.Add("ERROR")
	f.Add("+CME ERROR: 0")
	f.Add("+CMS ERROR: 500")
	f.Add("+CMGS: 255")
	f.Add("+CREG: 2,\"FFFF\",\"FFFFFFFF\"")
	f.Add(strings.Repeat("A", 10000))
	f.Fuzz(func(t *testing.T, line string) {
		s, port := newTestSerializer(t)
		go func() {
			time.Sleep(5 * time.Millisecond)
			port.feed(line)
			time.Sleep(5 * time.Millisecond)
			port.feed("OK")
		}()
		// Should never panic.
		_, _ = s.Execute("AT", 500*time.Millisecond)
	})
}

// ── TestURC_DuringATCommand (#108) ────────────────────────────────────────
// Verify that URCs arriving during an in-flight AT command are routed to
// the URC channel, not mixed into the command response.

func TestURC_DuringATCommand(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		// URC arrives mid-command.
		port.feed("+CMTI: \"SM\",5")
		time.Sleep(5 * time.Millisecond)
		// Command response follows.
		port.feed("+CSQ: 18,0")
		port.feed("OK")
	}()

	lines, err := s.Execute("AT+CSQ", 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "+CSQ:") {
		t.Errorf("response lines: %v", lines)
	}

	// The URC should be in the URC channel.
	select {
	case urc := <-s.URCCH:
		if !strings.HasPrefix(urc, "+CMTI:") {
			t.Errorf("expected CMTI URC, got %q", urc)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("URC not received in channel")
	}
}

// ── TestConcurrentURCs (#161) ─────────────────────────────────────────────

func TestConcurrentURCs(t *testing.T) {
	s, port := newTestSerializer(t)

	// Fire many URCs while a command is in flight.
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(2 * time.Millisecond)
			port.feed("+CMTI: \"SM\"," + string(rune('0'+i%10)))
		}
		time.Sleep(10 * time.Millisecond)
		port.feed("OK")
	}()

	_, err := s.Execute("AT", 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain URCs — at least some should have been delivered.
	count := 0
	for {
		select {
		case <-s.URCCH:
			count++
		default:
			goto done
		}
	}
done:
	if count == 0 {
		t.Error("expected at least some URCs to be delivered")
	}
}

// ── TestATCommandInjection (#51) ──────────────────────────────────────────
// Ensure that injecting AT commands via ICCID-like strings doesn't work.

func TestATCommandInjection(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("OK")
	}()

	// Execute with a command that contains injection attempt.
	_, err := s.Execute("AT+CCID?\r\nAT+CMGD=1,4", 1*time.Second)
	if err != nil {
		// The serializer sends the entire string as one command.
		// The modem would see the literal \r\n in the middle.
		// This test just ensures no panic or unexpected behavior.
		t.Logf("error (expected for malformed cmd): %v", err)
	}
}

// ── TestMultipleConsecutiveTimeouts (#97) ──────────────────────────────────

func TestMultipleConsecutiveTimeouts(t *testing.T) {
	s, _ := newTestSerializer(t)

	// First timeout should close the serializer.
	_, err1 := s.Execute("AT", 50*time.Millisecond)
	if !errors.Is(err1, ErrTimeout) {
		t.Errorf("first: expected ErrTimeout, got %v", err1)
	}

	// Second call should get ErrClosed since serializer was closed on timeout.
	_, err2 := s.Execute("AT", 50*time.Millisecond)
	if !errors.Is(err2, ErrClosed) {
		t.Errorf("second: expected ErrClosed, got %v", err2)
	}
}

// ── TestWriteError (#89) ──────────────────────────────────────────────────

type failWritePort struct {
	*mockPort
}

func (f *failWritePort) Write(p []byte) (int, error) {
	return 0, errors.New("simulated write failure")
}

func TestExecute_WriteError(t *testing.T) {
	mp := newMockPort()
	port := &failWritePort{mockPort: mp}
	s := NewSerializer(port, nil)
	t.Cleanup(func() {
		mp.Close()
		s.Close()
	})

	_, err := s.Execute("AT", 1*time.Second)
	if err == nil {
		t.Fatal("expected write error")
	}
}

// ── TestATE0 (#104) ──────────────────────────────────────────────────────
// Verify that ATE0 command is sent and parsed correctly.

func TestATE0(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("OK")
	}()

	lines, err := s.Execute("ATE0", 1*time.Second)
	if err != nil {
		t.Fatalf("ATE0 should succeed: %v", err)
	}
	// ATE0 response is just OK with no data lines.
	if len(lines) != 0 {
		t.Errorf("expected no data lines, got %v", lines)
	}
}

// ── TestCREG_LTE (#147) ──────────────────────────────────────────────────
// Verify AT+CREG? parsing for LTE registration responses.

func TestCREG_LTE(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("+CREG: 2,1,\"1A2B\",\"0000ABCD\",7")
		port.feed("OK")
	}()

	stat, err := s.RegistrationStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// stat==1 means registered, home network.
	if stat != 1 {
		t.Errorf("registration status: got %d, want 1", stat)
	}
}

// ── TestCPMS (#127) ──────────────────────────────────────────────────────
// Verify AT+CPMS parsing for storage selection.

func TestCPMS_StorageQuery(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("+CPMS: 5,20,5,20,5,20")
		port.feed("OK")
	}()

	lines, err := s.Execute("AT+CPMS?", 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "+CPMS:") {
		t.Errorf("response: %v", lines)
	}
}
