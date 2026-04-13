// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

//go:build e2e

// Package e2e contains end-to-end smoke tests for the SMS gateway.
//
// These tests require a running gateway instance. They are skipped automatically
// when the required environment variables are not set.
//
// Required environment variables:
//
//	E2E_CLOUD_URL  base URL of the gateway HTTP server (e.g. http://localhost:9200)
//	E2E_API_KEY    API key for the Authorization header
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// baseURL returns the gateway base URL from E2E_CLOUD_URL or skips the test.
func baseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("E2E_CLOUD_URL")
	if u == "" {
		t.Skip("E2E_CLOUD_URL not set — skipping e2e test")
	}
	return u
}

// apiKey returns the API key from E2E_API_KEY or skips the test.
func apiKey(t *testing.T) string {
	t.Helper()
	k := os.Getenv("E2E_API_KEY")
	if k == "" {
		t.Skip("E2E_API_KEY not set — skipping e2e test")
	}
	return k
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// TestHealth verifies that GET /health returns HTTP 200.
func TestHealth(t *testing.T) {
	base := baseURL(t)
	url := fmt.Sprintf("%s/health", base)

	resp, err := httpClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health: expected 200, got %d", resp.StatusCode)
	}
}

// TestSendSMS_Accepted verifies that POST /api/v1/sms/send returns 200 or 202.
func TestSendSMS_Accepted(t *testing.T) {
	base := baseURL(t)
	key := apiKey(t)

	payload := map[string]string{
		"iccid": "000000000000000",
		"to":    "+15550000000",
		"body":  "e2e smoke test",
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/api/v1/sms/send", base)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := httpClient().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		t.Errorf("POST /api/v1/sms/send: expected 200 or 202, got %d", resp.StatusCode)
	}
}
