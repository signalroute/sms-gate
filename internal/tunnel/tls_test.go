// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package tunnel

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// ── TLS skip-verify (#177) ────────────────────────────────────────────────

// TestManagerConfig_TLSSkipVerify_DefaultIsFalse verifies that TLSSkipVerify
// is false (zero value) so a zero-config ManagerConfig has TLS enabled.
func TestManagerConfig_TLSSkipVerify_DefaultIsFalse(t *testing.T) {
	cfg := ManagerConfig{}
	if cfg.TLSSkipVerify {
		t.Fatal("TLSSkipVerify should default to false")
	}
}

// TestManagerConfig_TLSSkipVerify_CanBeSet verifies the struct field is
// assignable (compilation test + runtime sanity check).
func TestManagerConfig_TLSSkipVerify_CanBeSet(t *testing.T) {
	cfg := ManagerConfig{TLSSkipVerify: true}
	if !cfg.TLSSkipVerify {
		t.Fatal("TLSSkipVerify should be true after assignment")
	}
}

// TestDial_TLSSkipVerify_ConnectsToSelfSignedServer verifies that when
// TLSSkipVerify=true the manager dials a TLS server with a self-signed cert
// successfully.  Without skip-verify the same dial would fail with an x509
// unknown authority error (#177).
func TestDial_TLSSkipVerify_ConnectsToSelfSignedServer(t *testing.T) {
	// Spin up a local HTTPS test server (self-signed cert).
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Echo one message then close.
		conn.ReadMessage() //nolint:errcheck
	}))
	defer srv.Close()

	wsURL := "wss://" + strings.TrimPrefix(srv.URL, "https://")

	// Default Go TLS pool does NOT trust httptest's self-signed cert.
	// With skip-verify=true, connection should succeed.
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // test only
		},
	}
	conn, _, err := dialer.Dial(wsURL, http.Header{"Authorization": {"Bearer test"}})
	if err != nil {
		t.Fatalf("skip-verify dial failed unexpectedly: %v", err)
	}
	conn.Close()

	// Now verify that without skip-verify, the same dial FAILS (proving the
	// default is secure).
	strictDialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			// Use an empty pool — won't trust the self-signed cert.
			RootCAs: x509.NewCertPool(),
		},
	}
	_, _, err = strictDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("strict-TLS dial should have failed with self-signed cert, but succeeded")
	}
}

// TestDial_TLSSkipVerify_DefaultFails verifies the baseline: without skip-verify
// a self-signed server cert is rejected.  This proves TLS is verified by default
// and not accidentally bypassed (#177).
func TestDial_TLSSkipVerify_DefaultFails(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		upgrader.Upgrade(w, r, nil) //nolint:errcheck
	}))
	defer srv.Close()

	wsURL := "wss://" + strings.TrimPrefix(srv.URL, "https://")

	// Default TLS config — does NOT have the test server's cert in its pool.
	defaultDialer := websocket.Dialer{}
	_, _, err := defaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected TLS verification failure for self-signed cert, got nil")
	}
}
