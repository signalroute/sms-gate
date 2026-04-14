// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package safe_test

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/safe"
)

// syncBuffer wraps bytes.Buffer with a mutex so slog writes and test reads are race-free.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Contains(needle []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Contains(b.buf.Bytes(), needle)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testLogger returns a logger writing to a buffer and the buffer itself.
func testLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return log, buf
}

func waitForLog(t *testing.T, buf *syncBuffer, needle []byte) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if buf.Contains(needle) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("expected log containing %q, got: %s", needle, buf.String())
}

func TestGo_NoPanic(t *testing.T) {
	log, _ := testLogger()
	done := make(chan struct{})
	safe.Go(log, "normal", func() { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not finish")
	}
}

func TestGo_PanicRecovered(t *testing.T) {
	log, buf := testLogger()
	done := make(chan struct{})
	safe.Go(log, "panicky", func() {
		panic("boom") // defer close(done) must run AFTER the recover logger
	})
	// Give recover() time to log before we check.
	safe.Go(log, "closer", func() { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not finish after panic")
	}
	waitForLog(t, buf, []byte("goroutine panic"))
	// Process stays alive (we're still here).
	if !buf.Contains([]byte("goroutine panic")) {
		t.Errorf("expected panic log entry, got: %s", buf.String())
	}
	if !buf.Contains([]byte("panicky")) {
		t.Errorf("expected goroutine name in log, got: %s", buf.String())
	}
}

func TestGo_PanicLogsStack(t *testing.T) {
	log, buf := testLogger()
	safe.Go(log, "stacky", func() { panic("stack test") })
	waitForLog(t, buf, []byte("stack"))
	if !buf.Contains([]byte("stack")) {
		t.Errorf("expected stack trace in log, got: %s", buf.String())
	}
}

func TestGoWithWaitGroup_NoPanic(t *testing.T) {
	log, _ := testLogger()
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	safe.GoWithWaitGroup(log, "wg-normal", &wg, func() { close(done) })
	wg.Wait()
	select {
	case <-done:
	default:
		t.Fatal("goroutine did not run")
	}
}

func TestGoWithWaitGroup_PanicCallsDone(t *testing.T) {
	log, buf := testLogger()
	var wg sync.WaitGroup
	wg.Add(1)
	safe.GoWithWaitGroup(log, "wg-panic", &wg, func() { panic("wg boom") })

	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()

	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("wg.Wait() did not return after panic recovery")
	}
	waitForLog(t, buf, []byte("wg-panic"))
	if !buf.Contains([]byte("wg-panic")) {
		t.Errorf("panic goroutine name not logged: %s", buf.String())
	}
}

func TestGoWithWaitGroup_PanicDoesNotKillProcess(t *testing.T) {
	log, _ := testLogger()
	var wg sync.WaitGroup
	const n = 10
	wg.Add(n)
	for i := 0; i < n; i++ {
		safe.GoWithWaitGroup(log, "concurrent-panic", &wg, func() { panic("concurrent") })
	}
	wg.Wait()
	// If we reach here, no panic escaped.
}

func TestGo_MultipleGoroutines(t *testing.T) {
	log, _ := testLogger()
	var mu sync.Mutex
	results := make([]string, 0, 4)

	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		n := name
		safe.Go(log, n, func() {
			defer wg.Done()
			mu.Lock()
			results = append(results, n)
			mu.Unlock()
		})
	}
	wg.Wait()
	if len(results) != 4 {
		t.Errorf("expected 4 results, got %d", len(results))
	}
}
