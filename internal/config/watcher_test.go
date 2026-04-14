// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package config

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const minimalCfg = `
gateway:
  id: gw-reload-test
tunnel:
  url: wss://api.example.com/tunnel
  token: secret123
modems:
  - port: /dev/ttyUSB0
`

const updatedCfg = `
gateway:
  id: gw-reload-updated
  log_level: debug
tunnel:
  url: wss://api.example.com/tunnel
  token: secret123
modems:
  - port: /dev/ttyUSB0
`

func writeWatcherConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("writeWatcherConfig: %v", err)
	}
	return p
}

// fakeNotify captures the signal channel so tests can inject signals manually.
func fakeNotify(out chan<- chan<- os.Signal) func(chan<- os.Signal, ...os.Signal) {
	return func(ch chan<- os.Signal, _ ...os.Signal) {
		out <- ch
	}
}

func waitForSignalChannel(t *testing.T, ready <-chan chan<- os.Signal) chan<- os.Signal {
	t.Helper()

	select {
	case ch := <-ready:
		return ch
	case <-time.After(time.Second):
		t.Fatal("signalNotify was not called")
		return nil
	}
}

func TestWatchReload_SIGHUPTriggersReload(t *testing.T) {
	path := writeWatcherConfig(t, minimalCfg)

	sigChReady := make(chan chan<- os.Signal, 1)
	origNotify := signalNotify
	origStop := signalStop
	t.Cleanup(func() { signalNotify = origNotify; signalStop = origStop })

	signalNotify = fakeNotify(sigChReady)
	signalStop = func(_ chan<- os.Signal) {}

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		WatchReload(ctx, path, func(_ *GatewayConfig) { //nolint:errcheck
			called.Add(1)
			cancel() // stop after first successful reload
		})
	}()

	sigCh := waitForSignalChannel(t, sigChReady)

	// Update the config file then send SIGHUP via the fake channel.
	if err := os.WriteFile(path, []byte(updatedCfg), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sigCh <- syscall.SIGHUP

	<-done
	if called.Load() != 1 {
		t.Errorf("expected apply to be called once, got %d", called.Load())
	}
}

func TestWatchReload_BadConfigDoesNotCrash(t *testing.T) {
	path := writeWatcherConfig(t, minimalCfg)

	sigChReady := make(chan chan<- os.Signal, 1)
	origNotify := signalNotify
	origStop := signalStop
	t.Cleanup(func() { signalNotify = origNotify; signalStop = origStop })

	signalNotify = fakeNotify(sigChReady)
	signalStop = func(_ chan<- os.Signal) {}

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		WatchReload(ctx, path, func(_ *GatewayConfig) {
			called.Add(1)
		})
	}()

	sigCh := waitForSignalChannel(t, sigChReady)

	// Write invalid YAML (missing required fields).
	if err := os.WriteFile(path, []byte("gateway:\n  id: ''\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sigCh <- syscall.SIGHUP

	// Give the watcher time to process the bad config.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if called.Load() != 0 {
		t.Errorf("apply must not be called on bad config, got %d calls", called.Load())
	}
}

func TestWatchReload_CtxCancellationStops(t *testing.T) {
	path := writeWatcherConfig(t, minimalCfg)

	origNotify := signalNotify
	origStop := signalStop
	t.Cleanup(func() { signalNotify = origNotify; signalStop = origStop })

	signalNotify = func(_ chan<- os.Signal, _ ...os.Signal) {}
	signalStop = func(_ chan<- os.Signal) {}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		WatchReload(ctx, path, func(_ *GatewayConfig) {}) //nolint:errcheck
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WatchReload did not return after context cancellation")
	}
}

func TestWatchReload_MultipleReloads(t *testing.T) {
	path := writeWatcherConfig(t, minimalCfg)

	sigChReady := make(chan chan<- os.Signal, 1)
	origNotify := signalNotify
	origStop := signalStop
	t.Cleanup(func() { signalNotify = origNotify; signalStop = origStop })

	signalNotify = fakeNotify(sigChReady)
	signalStop = func(_ chan<- os.Signal) {}

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		WatchReload(ctx, path, func(_ *GatewayConfig) {
			if called.Add(1) >= 3 {
				cancel()
			}
		})
	}()

	sigCh := waitForSignalChannel(t, sigChReady)

	for i := 0; i < 3; i++ {
		sigCh <- syscall.SIGHUP
	}

	<-done
	if called.Load() < 3 {
		t.Errorf("expected at least 3 reloads, got %d", called.Load())
	}
}
