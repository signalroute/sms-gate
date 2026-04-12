// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes content to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return p
}

// ── Load: happy path ──────────────────────────────────────────────────────

func TestLoad_Minimal(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-test-01
tunnel:
  url: wss://api.example.com/tunnel
  token: secret123
modems:
  - port: /dev/ttyUSB0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Gateway.ID != "gw-test-01" {
		t.Errorf("gateway.id: got %q", cfg.Gateway.ID)
	}
	if cfg.Tunnel.URL != "wss://api.example.com/tunnel" {
		t.Errorf("tunnel.url: got %q", cfg.Tunnel.URL)
	}
	if cfg.Tunnel.Token != "secret123" {
		t.Errorf("tunnel.token: got %q", cfg.Tunnel.Token)
	}
	if len(cfg.Modems) != 1 || cfg.Modems[0].Port != "/dev/ttyUSB0" {
		t.Errorf("modems: %+v", cfg.Modems)
	}
}

func TestLoad_MultipleModems(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-multi
tunnel:
  url: wss://api.example.com/tunnel
  token: tok
modems:
  - port: /dev/ttyUSB0
  - port: /dev/ttyUSB2
  - port: /dev/ttyUSB4
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Modems) != 3 {
		t.Errorf("expected 3 modems, got %d", len(cfg.Modems))
	}
}

// ── Defaults ──────────────────────────────────────────────────────────────

func TestLoad_Defaults(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-defaults
tunnel:
  url: wss://api.example.com/tunnel
  token: tok
modems:
  - port: /dev/ttyUSB0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Gateway defaults
	if cfg.Gateway.LogLevel != "info" {
		t.Errorf("log_level default: got %q, want info", cfg.Gateway.LogLevel)
	}

	// Tunnel defaults
	if cfg.Tunnel.PingIntervalS != 30 {
		t.Errorf("ping_interval_s default: got %d", cfg.Tunnel.PingIntervalS)
	}
	if cfg.Tunnel.PingTimeoutS != 10 {
		t.Errorf("ping_timeout_s default: got %d", cfg.Tunnel.PingTimeoutS)
	}
	if cfg.Tunnel.HeartbeatIntervalS != 60 {
		t.Errorf("heartbeat_interval_s default: got %d", cfg.Tunnel.HeartbeatIntervalS)
	}
	if cfg.Tunnel.ACKTimeoutS != 10 {
		t.Errorf("ack_timeout_s default: got %d", cfg.Tunnel.ACKTimeoutS)
	}
	if cfg.Tunnel.ReconnectBaseS != 1 {
		t.Errorf("reconnect_base_s default: got %d", cfg.Tunnel.ReconnectBaseS)
	}
	if cfg.Tunnel.ReconnectMaxS != 300 {
		t.Errorf("reconnect_max_s default: got %d", cfg.Tunnel.ReconnectMaxS)
	}

	// Buffer defaults
	if cfg.Buffer.DBPath != "./sms.db" {
		t.Errorf("db_path default: got %q", cfg.Buffer.DBPath)
	}
	if cfg.Buffer.RetentionDays != 7 {
		t.Errorf("retention_days default: got %d", cfg.Buffer.RetentionDays)
	}
	if cfg.Buffer.FlushIntervalM != 10 {
		t.Errorf("flush_interval_m default: got %d", cfg.Buffer.FlushIntervalM)
	}

	// Modem defaults
	if cfg.Modems[0].Baud != 115200 {
		t.Errorf("baud default: got %d", cfg.Modems[0].Baud)
	}
	if cfg.Modems[0].RateLimit.PerMin != 3 {
		t.Errorf("rate_limit.per_min default: got %d", cfg.Modems[0].RateLimit.PerMin)
	}
	if cfg.Modems[0].RateLimit.PerHour != 30 {
		t.Errorf("rate_limit.per_hour default: got %d", cfg.Modems[0].RateLimit.PerHour)
	}
	if cfg.Modems[0].RateLimit.PerDay != 200 {
		t.Errorf("rate_limit.per_day default: got %d", cfg.Modems[0].RateLimit.PerDay)
	}

	// Health defaults
	if cfg.Health.KeepaliveIntervalS != 60 {
		t.Errorf("keepalive_interval_s default: got %d", cfg.Health.KeepaliveIntervalS)
	}
	if cfg.Health.SIMCapacityWarnPct != 80 {
		t.Errorf("sim_capacity_warn_pct default: got %d", cfg.Health.SIMCapacityWarnPct)
	}
	if cfg.Health.SIMCapacityPurgePct != 95 {
		t.Errorf("sim_capacity_purge_pct default: got %d", cfg.Health.SIMCapacityPurgePct)
	}

	// Metrics default
	if cfg.Metrics.Addr != ":9200" {
		t.Errorf("metrics.addr default: got %q", cfg.Metrics.Addr)
	}
}

func TestLoad_ExplicitValuesOverrideDefaults(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-override
  log_level: debug
tunnel:
  url: wss://api.example.com/tunnel
  token: tok
  ping_interval_s:       45
  ping_timeout_s:        15
  heartbeat_interval_s: 120
  reconnect_base_s:       2
  reconnect_max_s:       600
buffer:
  db_path: /var/lib/gw/sms.db
  retention_days: 14
  flush_interval_m: 5
health:
  keepalive_interval_s:    30
  sim_capacity_warn_pct:   70
  sim_capacity_purge_pct:  90
metrics:
  addr: 127.0.0.1:9200
modems:
  - port: /dev/ttyUSB0
    baud: 9600
    rate_limit:
      per_min:  10
      per_hour: 100
      per_day:  500
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"log_level", cfg.Gateway.LogLevel, "debug"},
		{"ping_interval_s", cfg.Tunnel.PingIntervalS, 45},
		{"ping_timeout_s", cfg.Tunnel.PingTimeoutS, 15},
		{"heartbeat_interval_s", cfg.Tunnel.HeartbeatIntervalS, 120},
		{"reconnect_base_s", cfg.Tunnel.ReconnectBaseS, 2},
		{"reconnect_max_s", cfg.Tunnel.ReconnectMaxS, 600},
		{"db_path", cfg.Buffer.DBPath, "/var/lib/gw/sms.db"},
		{"retention_days", cfg.Buffer.RetentionDays, 14},
		{"flush_interval_m", cfg.Buffer.FlushIntervalM, 5},
		{"keepalive_interval_s", cfg.Health.KeepaliveIntervalS, 30},
		{"sim_capacity_warn_pct", cfg.Health.SIMCapacityWarnPct, 70},
		{"sim_capacity_purge_pct", cfg.Health.SIMCapacityPurgePct, 90},
		{"metrics.addr", cfg.Metrics.Addr, "127.0.0.1:9200"},
		{"baud", cfg.Modems[0].Baud, 9600},
		{"per_min", cfg.Modems[0].RateLimit.PerMin, 10},
		{"per_hour", cfg.Modems[0].RateLimit.PerHour, 100},
		{"per_day", cfg.Modems[0].RateLimit.PerDay, 500},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

// ── Env var expansion ─────────────────────────────────────────────────────

func TestLoad_EnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_GW_TOKEN", "my_secret_bearer_token")

	path := writeConfig(t, `
gateway:
  id: gw-envtest
tunnel:
  url: wss://api.example.com/tunnel
  token: ${TEST_GW_TOKEN}
modems:
  - port: /dev/ttyUSB0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tunnel.Token != "my_secret_bearer_token" {
		t.Errorf("token after env expansion: got %q", cfg.Tunnel.Token)
	}
}

func TestLoad_EnvVarInURL(t *testing.T) {
	t.Setenv("TEST_GW_HOST", "api.mycompany.com")

	path := writeConfig(t, `
gateway:
  id: gw-envurl
tunnel:
  url: wss://${TEST_GW_HOST}/tunnel
  token: tok
modems:
  - port: /dev/ttyUSB0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tunnel.URL != "wss://api.mycompany.com/tunnel" {
		t.Errorf("URL after expansion: got %q", cfg.Tunnel.URL)
	}
}

func TestLoad_EnvVarMissing_ReturnsError(t *testing.T) {
	// Ensure the variable is definitely not set.
	os.Unsetenv("DEFINITELY_NOT_SET_XYZ_12345")

	path := writeConfig(t, `
gateway:
  id: gw-missing-env
tunnel:
  url: wss://api.example.com/tunnel
  token: ${DEFINITELY_NOT_SET_XYZ_12345}
modems:
  - port: /dev/ttyUSB0
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for unset env var, got nil")
	}
}

// ── Validation ────────────────────────────────────────────────────────────

func TestLoad_Validation_MissingGatewayID(t *testing.T) {
	path := writeConfig(t, `
tunnel:
  url: wss://api.example.com/tunnel
  token: tok
modems:
  - port: /dev/ttyUSB0
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for missing gateway.id")
	}
}

func TestLoad_Validation_MissingTunnelURL(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-test
tunnel:
  token: tok
modems:
  - port: /dev/ttyUSB0
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for missing tunnel.url")
	}
}

func TestLoad_Validation_MissingToken(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-test
tunnel:
  url: wss://api.example.com/tunnel
modems:
  - port: /dev/ttyUSB0
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for missing tunnel.token")
	}
}

func TestLoad_Validation_NoModems(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-test
tunnel:
  url: wss://api.example.com/tunnel
  token: tok
modems: []
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for empty modems list")
	}
}

func TestLoad_Validation_ModemMissingPort(t *testing.T) {
	path := writeConfig(t, `
gateway:
  id: gw-test
tunnel:
  url: wss://api.example.com/tunnel
  token: tok
modems:
  - baud: 115200
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for modem with empty port")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing config file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeConfig(t, `
gateway: {this is: [not: valid yaml
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestLoad_EnvVarOverrides(t *testing.T) {
path := writeConfig(t, `
gateway:
  id: from-file
  log_level: info
tunnel:
  url: ws://from-file
  token: file-token
modems:
  - port: /dev/ttyUSB0
`)
t.Setenv("GATEWAY_ID", "from-env")
t.Setenv("TUNNEL_URL", "ws://from-env")
t.Setenv("TUNNEL_TOKEN", "env-token")
t.Setenv("LOG_LEVEL", "debug")
t.Setenv("LOG_FORMAT", "json")
t.Setenv("METRICS_ADDR", ":9999")

cfg, err := Load(path)
if err != nil {
t.Fatalf("Load: %v", err)
}

checks := []struct{ name, got, want string }{
{"gateway.id", cfg.Gateway.ID, "from-env"},
{"tunnel.url", cfg.Tunnel.URL, "ws://from-env"},
{"tunnel.token", cfg.Tunnel.Token, "env-token"},
{"log_level", cfg.Gateway.LogLevel, "debug"},
{"log_format", cfg.Gateway.LogFormat, "json"},
{"metrics.addr", cfg.Metrics.Addr, ":9999"},
}
for _, c := range checks {
if c.got != c.want {
t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
}
}
}
