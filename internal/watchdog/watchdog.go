// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package watchdog provides a systemd-compatible software watchdog that
// periodically sends sd_notify keep-alive pings and detects stalled goroutines.
//
// On embedded Linux targets using systemd with WatchdogSec=, the service
// manager kills the process if it stops sending WATCHDOG=1 notifications.
//
// Usage:
//
//	w := watchdog.New(watchdog.Config{
//	    Interval:  5 * time.Second,
//	    Probes:    []watchdog.Probe{tunnelProbe, modemProbe},
//	})
//	go w.Run(ctx)
package watchdog

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"
)

// Probe is a health-check function. It must return nil if the subsystem is
// healthy, or a non-nil error describing the problem. Probes should be
// non-blocking and complete quickly (< 100 ms).
type Probe func(ctx context.Context) error

// Config controls the watchdog behavior.
type Config struct {
	// Interval between watchdog ticks. Should be less than half the
	// systemd WatchdogSec to leave margin for scheduling jitter.
	Interval time.Duration

	// Probes are health checks run on every tick. If any probe returns an
	// error, the watchdog skips the sd_notify ping for that tick, causing
	// systemd to eventually restart the process.
	Probes []Probe

	// Logger for watchdog events. Uses slog.Default() if nil.
	Logger *slog.Logger
}

// Watchdog sends periodic sd_notify(WATCHDOG=1) pings as long as all probes
// pass. If any probe fails, the ping is skipped and the failure is logged.
type Watchdog struct {
	interval time.Duration
	probes   []Probe
	log      *slog.Logger
	notifyFn func() error
}

// New creates a Watchdog. If Config.Interval is zero, it defaults to 5s.
func New(cfg Config) *Watchdog {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Watchdog{
		interval: cfg.Interval,
		probes:   cfg.Probes,
		log:      log,
		notifyFn: sdNotify,
	}
}

// Run starts the watchdog loop. It blocks until ctx is canceled.
func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.log.Info("watchdog started", "interval", w.interval, "probes", len(w.probes))

	for {
		select {
		case <-ctx.Done():
			w.log.Info("watchdog stopped")
			return
		case <-ticker.C:
			if err := w.tick(ctx); err != nil {
				w.log.Warn("watchdog probe failed, skipping notify", "error", err)
				continue
			}
			if err := w.notifyFn(); err != nil {
				w.log.Warn("sd_notify failed", "error", err)
			}
		}
	}
}

func (w *Watchdog) tick(ctx context.Context) error {
	for _, p := range w.probes {
		if err := p(ctx); err != nil {
			return err
		}
	}
	return nil
}

// sdNotify sends WATCHDOG=1 to the systemd notification socket.
// Returns nil (no-op) if NOTIFY_SOCKET is not set.
func sdNotify() error {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return nil
	}

	conn, err := net.Dial("unixgram", sock)
	if err != nil {
		return fmt.Errorf("watchdog: dial %s: %w", sock, err)
	}
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("WATCHDOG=1"))
	return err
}
