// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

//go:build !pprof

package gateway

import "net/http"

// registerPprof is a no-op when the "pprof" build tag is not set.
func registerPprof(_ *http.ServeMux) {}
