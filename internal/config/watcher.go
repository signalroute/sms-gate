// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package config

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// signalNotify and signalStop are vars so tests can replace them without
// touching real OS signal machinery.
var signalNotify = func(ch chan<- os.Signal, sigs ...os.Signal) {
	signal.Notify(ch, sigs...)
}

var signalStop = func(ch chan<- os.Signal) {
	signal.Stop(ch)
}

// WatchReload listens for SIGHUP and reloads the config file at path on each
// signal. If parsing succeeds, apply is called with the new *GatewayConfig.
// Errors are logged but do not crash the watcher. WatchReload returns when ctx
// is canceled.
func WatchReload(ctx context.Context, path string, apply func(*GatewayConfig)) error {
	ch := make(chan os.Signal, 1)
	signalNotify(ch, syscall.SIGHUP)
	defer signalStop(ch)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			cfg, err := Load(path)
			if err != nil {
				slog.Error("config reload failed", "err", err)
				continue
			}
			apply(cfg)
		}
	}
}
