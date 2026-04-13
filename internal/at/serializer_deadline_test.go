// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"errors"
	"testing"
	"time"
)

// TestExecute_Timeout_ClosesSerializerAndUnblocksReader verifies that when
// Execute() times out, it closes the serializer so the reader goroutine exits
// instead of leaking on a dead modem (#165).
//
// Mechanism:
//   - Feed the mock port nothing (modem silent).
//   - Execute() returns ErrTimeout.
//   - The fix: Execute() calls s.Close(), which closes the port.
//   - The port's closeCh fires → Read returns io.EOF → sc.Scan() returns false
//     → the reader goroutine calls close(s.closed).
//   - We assert that s.closed is eventually closed (i.e., reader exited).
func TestExecute_Timeout_ClosesSerializerAndUnblocksReader(t *testing.T) {
	s, _ := newTestSerializer(t)

	_, err := s.Execute("AT", 50*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}

	// The reader goroutine must exit within 500ms of the timeout.
	select {
	case <-s.closed:
		// Pass: reader goroutine exited and closed s.closed.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reader goroutine did not exit within 500ms after Execute timeout — goroutine leak (#165)")
	}
}

// TestExecute_Timeout_SubsequentCallReturnsErrClosed verifies that after a
// timeout closes the serializer, any further Execute call returns ErrClosed
// (not ErrTimeout or a hang) so the worker's error-handling path is triggered
// correctly.
func TestExecute_Timeout_SubsequentCallReturnsErrClosed(t *testing.T) {
	// Use a new test serializer WITHOUT t.Cleanup calling Close() again
	// (Close() is idempotent, but let's be explicit).
	port := newMockPort()
	s := NewSerializer(port, nil)

	_, err := s.Execute("AT", 50*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("first Execute: expected ErrTimeout, got %v", err)
	}

	// Wait for the reader to fully close.
	select {
	case <-s.closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reader goroutine did not exit in time")
	}

	_, err2 := s.Execute("AT+CPIN?", 500*time.Millisecond)
	if err2 != ErrClosed {
		t.Fatalf("second Execute after timeout: expected ErrClosed, got %v", err2)
	}
}

// TestExecuteSend_Timeout_ClosesSerializerAndUnblocksReader verifies the same
// close-on-timeout behaviour in ExecuteSend (the two-phase CMGS flow).
func TestExecuteSend_Timeout_ClosesSerializerAndUnblocksReader(t *testing.T) {
	port := newMockPort()
	s := NewSerializer(port, nil)

	// ExecuteSend sends "AT+CMGS=N\r\n" and waits for a "> " prompt — feed nothing.
	_, err := s.ExecuteSend("AABBCC", 1, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Either the prompt wait times out or the AT command wait times out.
	// In both cases the serializer must be closed.
	select {
	case <-s.closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reader goroutine did not exit in time after ExecuteSend timeout (#165)")
	}
}

// TestExecute_ReaderExits_OnClose verifies the baseline: when the caller
// explicitly closes the serializer, the reader goroutine exits within 100ms.
// This is the prerequisite for the timeout fix.
func TestExecute_ReaderExits_OnClose(t *testing.T) {
	port := newMockPort()
	s := NewSerializer(port, nil)

	// Immediately close.
	port.Close()
	s.Close()

	select {
	case <-s.closed:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("reader goroutine did not exit after explicit Close()")
	}
}
