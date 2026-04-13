// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package config

import (
	"os"
	"strings"
	"testing"
)

// ── TestLoad_MissingRequiredFields (#24) ──────────────────────────────────
// Verify that missing required YAML fields produce clear errors.

func TestLoad_MissingGatewayID(t *testing.T) {
	yaml := `
gateway:
  id: ""
tunnel:
  url: wss://cloud.example.com/ws
  token: secret
modems:
  - port: /dev/ttyUSB0
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for empty gateway.id")
	}
	if !strings.Contains(err.Error(), "gateway.id") {
		t.Errorf("error should mention gateway.id: %v", err)
	}
}

func TestLoad_MissingTunnelURL(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
tunnel:
  url: ""
  token: secret
modems:
  - port: /dev/ttyUSB0
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for empty tunnel.url")
	}
}

func TestLoad_MissingToken(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
tunnel:
  url: wss://cloud.example.com/ws
  token: ""
modems:
  - port: /dev/ttyUSB0
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for empty tunnel.token")
	}
}

func TestLoad_NoModems(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
tunnel:
  url: wss://cloud.example.com/ws
  token: secret
modems: []
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for empty modems list")
	}
}

func TestLoad_EmptyModemPort(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
tunnel:
  url: wss://cloud.example.com/ws
  token: secret
modems:
  - port: ""
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for empty modem port")
	}
}

func TestLoad_InvalidPortName(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
tunnel:
  url: wss://cloud.example.com/ws
  token: secret
modems:
  - port: COM3
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for invalid port name")
	}
	if !strings.Contains(err.Error(), "/dev/") && !strings.Contains(err.Error(), "/tmp/") {
		t.Errorf("error should mention /dev/ or /tmp/: %v", err)
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
  log_level: verbose
tunnel:
  url: wss://cloud.example.com/ws
  token: secret
modems:
  - port: /dev/ttyUSB0
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for invalid log_level")
	}
}

func TestLoad_InvalidTunnelScheme(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
tunnel:
  url: http://cloud.example.com/ws
  token: secret
modems:
  - port: /dev/ttyUSB0
`
	f := writeTempYAML(t, yaml)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for non-ws:// tunnel URL")
	}
	if !strings.Contains(err.Error(), "ws://") {
		t.Errorf("error should mention ws://: %v", err)
	}
}

// ── TestLoad_ExtraUnknownFields (#139) ────────────────────────────────────
// YAML with extra fields should still load (Go's yaml.Unmarshal ignores unknowns).

func TestLoad_ExtraUnknownFields(t *testing.T) {
	yaml := `
gateway:
  id: gw-1
  log_level: info
  unknown_field: hello
tunnel:
  url: wss://cloud.example.com/ws
  token: secret
  extra_option: true
modems:
  - port: /dev/ttyUSB0
    baud: 115200
    some_future_field: 42
`
	f := writeTempYAML(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("extra unknown fields should not cause error: %v", err)
	}
	if cfg.Gateway.ID != "gw-1" {
		t.Errorf("gateway.id: got %q", cfg.Gateway.ID)
	}
}

// ── TestLoad_ExpandEnv (#101) ─────────────────────────────────────────────

func TestLoad_ExpandEnv(t *testing.T) {
	os.Setenv("TEST_GATEWAY_ID", "gw-env-1")
	os.Setenv("TEST_TUNNEL_TOKEN", "my-secret-token")
	defer os.Unsetenv("TEST_GATEWAY_ID")
	defer os.Unsetenv("TEST_TUNNEL_TOKEN")

	yaml := `
gateway:
  id: ${TEST_GATEWAY_ID}
tunnel:
  url: wss://cloud.example.com/ws
  token: ${TEST_TUNNEL_TOKEN}
modems:
  - port: /dev/ttyUSB0
`
	f := writeTempYAML(t, yaml)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("expandEnv should work: %v", err)
	}
	if cfg.Gateway.ID != "gw-env-1" {
		t.Errorf("gateway.id: got %q, want gw-env-1", cfg.Gateway.ID)
	}
	if cfg.Tunnel.Token != "my-secret-token" {
		t.Errorf("tunnel.token: got %q", cfg.Tunnel.Token)
	}
}

// ── TestLoad_ValidLogLevels ───────────────────────────────────────────────

func TestLoad_ValidLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			yaml := `
gateway:
  id: gw-1
  log_level: ` + level + `
tunnel:
  url: wss://cloud.example.com/ws
  token: secret
modems:
  - port: /dev/ttyUSB0
`
			f := writeTempYAML(t, yaml)
			_, err := Load(f)
			if err != nil {
				t.Fatalf("log_level %q should be valid: %v", level, err)
			}
		})
	}
}

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}
