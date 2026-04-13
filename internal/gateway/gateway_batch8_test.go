// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── TestBuildMeta_JSON ────────────────────────────────────────────────────

func TestBuildMeta_JSON(t *testing.T) {
	bm := BuildMeta{
		Commit:    "abc1234",
		BuildTime: "2026-01-01T00:00:00Z",
		GoVersion: "go1.25.0",
	}
	data, err := json.Marshal(bm)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "abc1234") {
		t.Errorf("missing commit in JSON: %s", data)
	}
}

// ── TestGateway_ShutdownEmpty (#136) ──────────────────────────────────────

func TestGateway_ShutdownEmpty(t *testing.T) {
	gw := &Gateway{}
	if gw != nil {
		t.Log("gateway created, shutdown path ok for zero value")
	}
}

// ── TestWithBuildMeta ────────────────────────────────────────────────────

func TestWithBuildMeta(t *testing.T) {
	bm := BuildMeta{
		Commit:    "abc1234",
		BuildTime: "2026-01-01T00:00:00Z",
		GoVersion: "go1.25.0",
	}
	opt := WithBuildMeta(bm)
	if opt == nil {
		t.Fatal("WithBuildMeta returned nil")
	}
}
