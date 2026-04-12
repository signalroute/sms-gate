// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package safe provides panic-recovery helpers for goroutines.
//
// A single unrecovered panic in any goroutine will kill the entire process.
// Use [Go] instead of `go` for every long-lived goroutine that must not bring
// down the gateway when it encounters an unexpected condition.
package safe

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine.  If fn panics, the panic is recovered,
// logged at ERROR level with a full stack trace, and the goroutine exits
// cleanly (i.e. the process stays alive).
//
//	safe.Go(log, "writer", func() { m.writer(ctx, conn, cancel) })
func Go(log *slog.Logger, name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("goroutine panic — recovered",
					"goroutine", name,
					"panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()),
				)
			}
		}()
		fn()
	}()
}

// GoWithWaitGroup is like Go but signals the WaitGroup on completion (including
// after a panic-recovery).  Useful when the caller needs to observe goroutine
// exit via wg.Wait().
//
//	wg.Add(1)
//	safe.GoWithWaitGroup(log, "metrics-srv", &wg, func() { ... })
func GoWithWaitGroup(log *slog.Logger, name string, wg interface{ Done() }, fn func()) {
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Error("goroutine panic — recovered",
					"goroutine", name,
					"panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()),
				)
			}
		}()
		fn()
	}()
}
