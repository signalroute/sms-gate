// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// envVarRe matches ${VAR_NAME} substitution tokens.
var envVarRe = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

// GatewayConfig is the top-level configuration structure loaded from config.yaml.
type GatewayConfig struct {
	Gateway GatewaySection `yaml:"gateway"`
	Tunnel  TunnelSection  `yaml:"tunnel"`
	Buffer  BufferSection  `yaml:"buffer"`
	Modems  []ModemConfig  `yaml:"modems"`
	Health  HealthSection  `yaml:"health"`
	Metrics MetricsSection `yaml:"metrics"`
}

type GatewaySection struct {
	ID        string `yaml:"id"`
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
}

type TunnelSection struct {
	URL                  string `yaml:"url"`
	Token                string `yaml:"token"`
	PingIntervalS        int    `yaml:"ping_interval_s"`
	PingTimeoutS         int    `yaml:"ping_timeout_s"`
	HeartbeatIntervalS   int    `yaml:"heartbeat_interval_s"`
	ACKTimeoutS          int    `yaml:"ack_timeout_s"`
	ReconnectBaseS       int    `yaml:"reconnect_base_s"`
	ReconnectMaxS        int    `yaml:"reconnect_max_s"`
	// HandshakeTimeoutS is the WebSocket handshake deadline in seconds (#125).
	HandshakeTimeoutS    int    `yaml:"handshake_timeout_s"`
}

type BufferSection struct {
	DBPath          string `yaml:"db_path"`
	RetentionDays   int    `yaml:"retention_days"`
	FlushIntervalM  int    `yaml:"flush_interval_m"`
}

type ModemConfig struct {
	Port           string          `yaml:"port"`
	Baud           int             `yaml:"baud"`
	RateLimit      RateLimitConfig `yaml:"rate_limit"`
	ExpectedICCID  string          `yaml:"expected_iccid,omitempty"` // optional SIM ICCID guard (#135)
}

type RateLimitConfig struct {
	PerMin  int `yaml:"per_min"`
	PerHour int `yaml:"per_hour"`
	PerDay  int `yaml:"per_day"`
}

type HealthSection struct {
	KeepaliveIntervalS    int `yaml:"keepalive_interval_s"`
	SIMCapacityWarnPct    int `yaml:"sim_capacity_warn_pct"`
	SIMCapacityPurgePct   int `yaml:"sim_capacity_purge_pct"`
	SignalPollIntervalS   int `yaml:"signal_poll_interval_s"`
}

type MetricsSection struct {
	Addr string `yaml:"addr"`
}

// defaults fills in zero values with sensible production defaults.
func defaults(cfg *GatewayConfig) {
	if cfg.Gateway.LogLevel == "" {
		cfg.Gateway.LogLevel = "info"
	}
	if cfg.Gateway.LogFormat == "" {
		cfg.Gateway.LogFormat = "text"
	}
	if cfg.Tunnel.PingIntervalS == 0 {
		cfg.Tunnel.PingIntervalS = 30
	}
	if cfg.Tunnel.PingTimeoutS == 0 {
		cfg.Tunnel.PingTimeoutS = 10
	}
	if cfg.Tunnel.HeartbeatIntervalS == 0 {
		cfg.Tunnel.HeartbeatIntervalS = 60
	}
	if cfg.Tunnel.ACKTimeoutS == 0 {
		cfg.Tunnel.ACKTimeoutS = 10
	}
	if cfg.Tunnel.ReconnectBaseS == 0 {
		cfg.Tunnel.ReconnectBaseS = 1
	}
	if cfg.Tunnel.ReconnectMaxS == 0 {
		cfg.Tunnel.ReconnectMaxS = 300
	}
	if cfg.Tunnel.HandshakeTimeoutS == 0 {
		cfg.Tunnel.HandshakeTimeoutS = 15
	}
	if cfg.Buffer.DBPath == "" {
		cfg.Buffer.DBPath = "./sms.db"
	}
	if cfg.Buffer.RetentionDays == 0 {
		cfg.Buffer.RetentionDays = 7
	}
	if cfg.Buffer.FlushIntervalM == 0 {
		cfg.Buffer.FlushIntervalM = 10
	}
	if cfg.Health.KeepaliveIntervalS == 0 {
		cfg.Health.KeepaliveIntervalS = 60
	}
	if cfg.Health.SIMCapacityWarnPct == 0 {
		cfg.Health.SIMCapacityWarnPct = 80
	}
	if cfg.Health.SIMCapacityPurgePct == 0 {
		cfg.Health.SIMCapacityPurgePct = 95
	}
	if cfg.Health.SignalPollIntervalS == 0 {
		cfg.Health.SignalPollIntervalS = 30
	}
	if cfg.Metrics.Addr == "" {
		cfg.Metrics.Addr = ":9200"
	}
	for i := range cfg.Modems {
		if cfg.Modems[i].Baud == 0 {
			cfg.Modems[i].Baud = 115200
		}
		if cfg.Modems[i].RateLimit.PerMin == 0 {
			cfg.Modems[i].RateLimit.PerMin = 3
		}
		if cfg.Modems[i].RateLimit.PerHour == 0 {
			cfg.Modems[i].RateLimit.PerHour = 30
		}
		if cfg.Modems[i].RateLimit.PerDay == 0 {
			cfg.Modems[i].RateLimit.PerDay = 200
		}
	}
}

// expandEnv replaces ${VAR} tokens with their environment variable values.
// Returns an error if any referenced variable is unset.
func expandEnv(s string) (string, error) {
	var expandErr error
	result := envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		name := envVarRe.FindStringSubmatch(match)[1]
		val, ok := os.LookupEnv(name)
		if !ok {
			expandErr = fmt.Errorf("environment variable %q is not set", name)
			return match
		}
		return val
	})
	return result, expandErr
}

// Load reads and validates the configuration from the given YAML file path.
// Environment variable references in the form ${VAR_NAME} are expanded.
// After loading, direct env vars (e.g. GATEWAY_ID, TUNNEL_URL) override the
// corresponding config file values — no ${VAR} syntax required in the YAML.
func Load(path string) (*GatewayConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Expand env vars before YAML parsing so they work inside quoted strings.
	expanded, err := expandEnv(string(raw))
	if err != nil {
		return nil, fmt.Errorf("config env expansion: %w", err)
	}

	var cfg GatewayConfig
	if err := yaml.NewDecoder(strings.NewReader(expanded)).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	defaults(&cfg)
	applyEnvOverrides(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// applyEnvOverrides reads well-known environment variables and overwrites the
// corresponding fields in cfg.  This lets operators inject secrets (TUNNEL_TOKEN)
// or adjust runtime settings without editing the YAML file.
//
// Precedence: env var > config file value > built-in default.
func applyEnvOverrides(cfg *GatewayConfig) {
	if v := os.Getenv("GATEWAY_ID"); v != "" {
		cfg.Gateway.ID = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Gateway.LogLevel = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Gateway.LogFormat = v
	}
	if v := os.Getenv("TUNNEL_URL"); v != "" {
		cfg.Tunnel.URL = v
	}
	if v := os.Getenv("TUNNEL_TOKEN"); v != "" {
		cfg.Tunnel.Token = v
	}
	if v := os.Getenv("METRICS_ADDR"); v != "" {
		cfg.Metrics.Addr = v
	}
	if v := os.Getenv("SIGNAL_POLL_INTERVAL"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			cfg.Health.SignalPollIntervalS = s
		}
	}
}

func validate(cfg *GatewayConfig) error {
	if cfg.Gateway.ID == "" {
		return fmt.Errorf("gateway.id must not be empty")
	}
	if cfg.Tunnel.URL == "" {
		return fmt.Errorf("tunnel.url must not be empty")
	}
	if !strings.HasPrefix(cfg.Tunnel.URL, "ws://") && !strings.HasPrefix(cfg.Tunnel.URL, "wss://") {
		return fmt.Errorf("tunnel.url must start with ws:// or wss://, got %q", cfg.Tunnel.URL)
	}
	if cfg.Tunnel.Token == "" {
		return fmt.Errorf("tunnel.token must not be empty")
	}
	if len(cfg.Modems) == 0 {
		return fmt.Errorf("at least one modem must be configured")
	}
	for i, m := range cfg.Modems {
		if m.Port == "" {
			return fmt.Errorf("modems[%d].port must not be empty", i)
		}
	}
	return nil
}
