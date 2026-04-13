// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

//go:build pprof

package gateway

import (
	"net/http"
	"net/http/pprof"
)

// registerPprof adds /debug/pprof/* endpoints to the metrics mux.
// This file is only compiled when the "pprof" build tag is set:
//
//	go build -tags pprof ./cmd/gateway
func registerPprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
