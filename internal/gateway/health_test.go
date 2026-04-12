// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/signalroute/sms-gate/internal/metrics"
	"github.com/signalroute/sms-gate/internal/modem"
	"github.com/signalroute/sms-gate/internal/tunnel"
)

// newTestGateway builds a minimal Gateway suitable for unit-testing handlers.
func newTestGateway(t *testing.T) *Gateway {
	t.Helper()
	promReg := prometheus.NewRegistry()
	m := metrics.New(promReg)
	mgr := tunnel.NewManager(tunnel.ManagerConfig{
		GatewayID:    "test",
		AgentVersion: "test",
		URL:          "ws://localhost:9999",
		Token:        "test",
		Logger:       slog.Default(),
		Metrics:      m,
	})
	return &Gateway{
		metrics:   m,
		reg:       modem.NewRegistry(),
		mgr:       mgr,
		promReg:   promReg,
		startTime: time.Now(),
	}
}

func TestHealthHandler_Disconnected(t *testing.T) {
	gw := newTestGateway(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	gw.healthHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Status != "degraded" {
		t.Errorf("want status=degraded, got %q", resp.Status)
	}
	if resp.Version == "" {
		t.Error("version must not be empty")
	}
	if resp.Uptime == "" {
		t.Error("uptime must not be empty")
	}
}

func TestHealthHandler_MethodNotAllowed(t *testing.T) {
	gw := newTestGateway(t)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	gw.healthHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}
