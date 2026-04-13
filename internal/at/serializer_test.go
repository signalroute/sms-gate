// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── mockPort simulates a modem's serial port ──────────────────────────────
//
// Read blocks until lines are queued via feed() or the port is closed.
// Write discards all data (we don't validate command echoes in these tests).

type mockPort struct {
	mu      sync.Mutex
	buf     []byte
	lineCh  chan string
	closeCh chan struct{}
	once    sync.Once
}

func newMockPort() *mockPort {
	return &mockPort{
		lineCh:  make(chan string, 64),
		closeCh: make(chan struct{}),
	}
}

// feed queues lines to be delivered to the serializer's reader goroutine.
// Each line is sent with a trailing \r\n, as a real modem would.
func (p *mockPort) feed(lines ...string) {
	for _, l := range lines {
		p.lineCh <- l + "\r\n"
	}
}

func (p *mockPort) Read(dst []byte) (int, error) {
	p.mu.Lock()
	if len(p.buf) > 0 {
		n := copy(dst, p.buf)
		p.buf = p.buf[n:]
		p.mu.Unlock()
		return n, nil
	}
	p.mu.Unlock()

	select {
	case line := <-p.lineCh:
		p.mu.Lock()
		p.buf = append(p.buf, []byte(line)...)
		n := copy(dst, p.buf)
		p.buf = p.buf[n:]
		p.mu.Unlock()
		return n, nil
	case <-p.closeCh:
		return 0, io.EOF
	}
}

func (p *mockPort) Write(src []byte) (int, error) {
	return len(src), nil
}

func (p *mockPort) Close() error {
	p.once.Do(func() { close(p.closeCh) })
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────

// newTestSerializer creates a Serializer + its underlying mock port.
func newTestSerializer(t *testing.T) (*Serializer, *mockPort) {
	t.Helper()
	port := newMockPort()
	s := NewSerializer(port, nil)
	t.Cleanup(func() {
		port.Close()
		s.Close()
	})
	return s, port
}

// ── Execute tests ─────────────────────────────────────────────────────────

func TestExecute_OK(t *testing.T) {
	s, port := newTestSerializer(t)

	// Queue the modem response before sending the command,
	// since the reader goroutine may race to consume.
	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("OK")
	}()

	lines, err := s.Execute("AT", 2*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected no intermediate lines, got %v", lines)
	}
}

func TestExecute_WithResponseLines(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("+CSQ: -71,0", "OK")
	}()

	lines, err := s.Execute("AT+CSQ", 2*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(lines) != 1 || lines[0] != "+CSQ: -71,0" {
		t.Errorf("lines: got %v", lines)
	}
}

func TestExecute_ERROR(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("ERROR")
	}()

	_, err := s.Execute("AT+FAKECMD", 2*time.Second)
	if !errors.Is(err, ErrATError) {
		t.Errorf("expected ErrATError, got %v", err)
	}
	var atErr *ATError
	if !errors.As(err, &atErr) {
		t.Fatalf("expected *ATError, got %T", err)
	}
	if atErr.Cmd != "AT+FAKECMD" {
		t.Errorf("cmd: got %q, want AT+FAKECMD", atErr.Cmd)
	}
}

func TestExecute_CMEError(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("+CME ERROR: 10")
	}()

	_, err := s.Execute("AT+CLCK", 2*time.Second)
	if err == nil {
		t.Fatal("expected CME error, got nil")
	}
	var atErr *ATError
	if !errors.As(err, &atErr) {
		t.Fatalf("expected *ATError, got %T", err)
	}
	if atErr.Cmd != "AT+CLCK" {
		t.Errorf("cmd: got %q", atErr.Cmd)
	}
	if !strings.Contains(atErr.Raw, "+CME ERROR: 10") {
		t.Errorf("raw: got %q", atErr.Raw)
	}
}

func TestExecute_Timeout(t *testing.T) {
	s, _ := newTestSerializer(t)

	// Feed nothing — should time out.
	start := time.Now()
	_, err := s.Execute("AT", 100*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got %v", err)
	}
	if elapsed < 90*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Errorf("timeout duration suspicious: %v", elapsed)
	}
}

func TestExecute_AfterClose(t *testing.T) {
	s, port := newTestSerializer(t)
	port.Close()
	s.Close()

	// Give the reader goroutine time to notice EOF.
	time.Sleep(50 * time.Millisecond)

	_, err := s.Execute("AT", 500*time.Millisecond)
	if err != ErrClosed {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

// ── URC routing ───────────────────────────────────────────────────────────

func TestURC_DeliveredWhileIdle(t *testing.T) {
	s, port := newTestSerializer(t)

	port.feed(`+CMTI: "SM",7`)

	select {
	case urc := <-s.URCCH:
		if urc != `+CMTI: "SM",7` {
			t.Errorf("urc: got %q", urc)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for URC")
	}
}

func TestURC_DeliveredDuringCommand(t *testing.T) {
	s, port := newTestSerializer(t)

	// Interleave: start a command, then a URC arrives, then the OK.
	go func() {
		time.Sleep(20 * time.Millisecond)
		port.feed(`+CMTI: "SM",3`) // URC mid-command
		time.Sleep(10 * time.Millisecond)
		port.feed("+COPS: 0,0,\"Telekom.de\",7", "OK")
	}()

	lines, err := s.Execute("AT+COPS?", 2*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(lines) != 1 {
		t.Errorf("expected 1 response line, got %d: %v", len(lines), lines)
	}

	// URC must have been routed to URCCH, not mixed with response lines.
	select {
	case urc := <-s.URCCH:
		if urc != `+CMTI: "SM",3` {
			t.Errorf("urc: got %q", urc)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("URC not delivered to URCCH")
	}
}

func TestURC_MultipleURCsBuffered(t *testing.T) {
	s, port := newTestSerializer(t)

	// Send 5 URCs quickly.
	for i := 0; i < 5; i++ {
		port.feed(`+CMTI: "SM",` + string(rune('0'+i)))
	}

	received := 0
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case <-s.URCCH:
			received++
			if received == 5 {
				return
			}
		case <-timeout:
			t.Errorf("received only %d/5 URCs before timeout", received)
			return
		}
	}
}

// ── Convenience wrappers ──────────────────────────────────────────────────

func TestSignalQuality(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("+CSQ: 18,0", "OK")
	}()

	rssi, err := s.SignalQuality()
	if err != nil {
		t.Fatalf("SignalQuality: %v", err)
	}
	// AT+CSQ returns 18 → dBm = -113 + 18*2 = -77.
	if rssi != -77 {
		t.Errorf("RSSI: got %d, want -77", rssi)
	}
}

func TestSignalQuality_Unknown(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		port.feed("+CSQ: 99,0", "OK") // 99 = not detectable
	}()

	rssi, err := s.SignalQuality()
	if err != nil {
		t.Fatalf("SignalQuality: %v", err)
	}
	if rssi != -113 {
		t.Errorf("RSSI unknown: got %d, want -113", rssi)
	}
}

func TestRegistrationStatus(t *testing.T) {
	cases := []struct {
		response string
		want     int
	}{
		{"+CREG: 0,1", 1},  // two-field (AT+CREG=2 mode)
		{"+CREG: 1", 1},    // one-field (AT+CREG=0 mode)
		{"+CREG: 0,3", 3},  // registration denied
		{"+CREG: 5", 5},    // roaming
	}
	for _, tc := range cases {
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
			if stat != tc.want {
				t.Errorf("stat: got %d, want %d", stat, tc.want)
			}
		})
	}
}

func TestReadICCID(t *testing.T) {
	s, port := newTestSerializer(t)

	go func() {
		time.Sleep(10 * time.Millisecond)
		// First command AT+CCID? returns the ICCID.
		port.feed("+CCID: 89490200001234567890", "OK")
	}()

	iccid, err := s.ReadICCID()
	if err != nil {
		t.Fatalf("ReadICCID: %v", err)
	}
	if iccid != "89490200001234567890" {
		t.Errorf("ICCID: got %q", iccid)
	}
}

// ── isURC classification ──────────────────────────────────────────────────

func TestIsURC(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{`+CMTI: "SM",5`, true},
		{"+RING", true},
		{"+CLIP: ...", true},
		{"+CRING: VOICE", true},
		// These must NOT be classified as URCs (they are +CREG responses to a query).
		{"+CREG: 1", false},
		{"+CREG: 0,1,\"1A2B\",\"0A\"", false},
		// Regular response lines.
		{"OK", false},
		{"ERROR", false},
		{"+CSQ: 18,0", false},
		{"+CCID: 89490200001234567890", false},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := isURC(tc.line)
			if got != tc.want {
				t.Errorf("isURC(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}
