// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ── Sentinel errors ────────────────────────────────────────────────────────

var (
	ErrTimeout           = errors.New("AT command timed out")
	ErrATError           = errors.New("AT ERROR")
	ErrClosed            = errors.New("serializer closed")
	ErrSendPrompt        = errors.New("did not receive send prompt '>'")
	ErrMalformedResponse = errors.New("AT malformed response")
)

// ── URC classification ─────────────────────────────────────────────────────

// urcPrefixes lists line prefixes that are ALWAYS unsolicited.
// Lines matching these are forwarded to urcCh regardless of whether a
// command is in-flight.  Per the spec: "+CREG:" is routed as response
// data when a command is pending (disambiguates AT+CREG? response from URC).
var urcPrefixes = []string{
	"+CMTI:",  // new SMS notification — the primary URC we care about
	"+RING",   // incoming call
	"+CLIP:",  // caller ID
	"+CUSD:",  // USSD response
	"+CMT:",   // direct SMS delivery (CNMI mode 2, not used here but handle anyway)
	"+CDSI:",  // delivery status report
	"+CRING:", // extended ring
}

func isURC(line string) bool {
	for _, pfx := range urcPrefixes {
		if strings.HasPrefix(line, pfx) {
			return true
		}
	}
	return false
}

// ── pendingCmd represents an in-flight AT command ─────────────────────────

type pendingCmd struct {
	linesCh chan string // reader goroutine sends response lines here
}

// ── Serializer ─────────────────────────────────────────────────────────────

// Serializer owns a single serial port and provides serialized AT command
// execution with concurrent URC delivery.
//
// Architecture:
//   - One reader goroutine owns all reads from the port.
//   - All writes go through Execute() which holds writeMu.
//   - Only one command may be in-flight at a time (cmdMu).
//   - URCs matching urcPrefixes are forwarded to URCCH regardless of state.
//   - All other lines during a command are forwarded to pendingCmd.linesCh.
type Serializer struct {
	port io.ReadWriteCloser

	writeMu sync.Mutex  // serialize port writes
	cmdMu   sync.Mutex  // protect pending
	pending *pendingCmd

	// URCCH delivers unsolicited lines to the modem worker.
	URCCH chan string // buffered, size 64

	// ObserveLatency, when non-nil, is called after every Execute call with
	// the command string and round-trip duration.  The worker wires this to
	// the smsgate_at_command_duration_seconds histogram.
	ObserveLatency func(cmd string, dur time.Duration)

	closeOnce sync.Once
	closed    chan struct{}
	log       *slog.Logger
}

// NewSerializer creates a Serializer and starts its reader goroutine.
func NewSerializer(port io.ReadWriteCloser, log *slog.Logger) *Serializer {
	s := &Serializer{
		port:   port,
		URCCH:  make(chan string, 64),
		closed: make(chan struct{}),
		log:    log,
	}
	go s.reader()
	return s
}

// reader is the sole goroutine that reads from the serial port.
// It classifies each line and routes it appropriately.
func (s *Serializer) reader() {
	sc := bufio.NewScanner(s.port)
	// Increase the per-line buffer to handle large AT responses (#70).
	sc.Buffer(make([]byte, 0, 4096), 256*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}

		if s.log != nil {
			s.log.Debug("← recv", "line", line)
		}

		if isURC(line) {
			select {
			case s.URCCH <- line:
			default:
				// URC buffer full; drop oldest and push new.
				select {
				case <-s.URCCH:
				default:
				}
				select {
				case s.URCCH <- line:
				default:
				}
			}
			continue
		}

		// Route to pending command if one is in-flight.
		s.cmdMu.Lock()
		p := s.pending
		s.cmdMu.Unlock()

		if p != nil {
			select {
			case p.linesCh <- line:
			default:
				// Response buffer full; should not happen with size 64.
			}
		}
		// If no pending command and not a URC, discard (echo or unexpected).
	}
	// Scanner ended — port closed.
	close(s.closed)
}

// Execute sends an AT command and waits for the final OK/ERROR response.
// It is safe to call from multiple goroutines but commands are serialized.
func (s *Serializer) Execute(cmd string, timeout time.Duration) ([]string, error) {
	select {
	case <-s.closed:
		return nil, ErrClosed
	default:
	}

	p := &pendingCmd{linesCh: make(chan string, 64)}

	s.cmdMu.Lock()
	s.pending = p
	s.cmdMu.Unlock()

	defer func() {
		s.cmdMu.Lock()
		s.pending = nil
		s.cmdMu.Unlock()
	}()

	// Send the command.
	s.writeMu.Lock()
	_, err := fmt.Fprintf(s.port, "%s\r\n", cmd)
	s.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write AT command: %w", err)
	}

	start := time.Now()

	// Collect response lines until OK/ERROR/timeout.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var lines []string
	for {
		select {
		case line := <-p.linesCh:
			switch {
			case line == "OK":
				if fn := s.ObserveLatency; fn != nil {
					fn(cmd, time.Since(start))
				}
				return lines, nil
			case line == "ERROR":
				if fn := s.ObserveLatency; fn != nil {
					fn(cmd, time.Since(start))
				}
				return lines, ErrATError
			case strings.HasPrefix(line, "+CME ERROR:"):
				if fn := s.ObserveLatency; fn != nil {
					fn(cmd, time.Since(start))
				}
				code := strings.TrimSpace(strings.TrimPrefix(line, "+CME ERROR:"))
				return lines, fmt.Errorf("CME ERROR %s", code)
			case strings.HasPrefix(line, "+CMS ERROR:"):
				if fn := s.ObserveLatency; fn != nil {
					fn(cmd, time.Since(start))
				}
				code := strings.TrimSpace(strings.TrimPrefix(line, "+CMS ERROR:"))
				return lines, fmt.Errorf("CMS ERROR %s", code)
			default:
				lines = append(lines, line)
			}
		case <-timer.C:
			// The modem did not respond in time.  Close the serializer so the
			// reader goroutine unblocks (it is currently blocked in sc.Scan()
			// waiting for bytes that will never arrive).  Without this, the
			// reader leaks permanently on every modem that stops responding
			// (#165).  After Close() any future Execute call returns ErrClosed.
			s.Close()
			return nil, ErrTimeout
		case <-s.closed:
			return nil, ErrClosed
		}
	}
}

// ExecuteSend handles the two-phase AT+CMGS flow:
//  1. Send AT+CMGS=<len>\r\n  → modem replies with "> " prompt
//  2. Send <PDU>\x1A          → modem replies with +CMGS: <mr>\r\nOK
//
// Returns the message reference number on success.
func (s *Serializer) ExecuteSend(pduHex string, pduLen int, timeout time.Duration) (int, error) {
	select {
	case <-s.closed:
		return 0, ErrClosed
	default:
	}

	p := &pendingCmd{linesCh: make(chan string, 64)}

	s.cmdMu.Lock()
	s.pending = p
	s.cmdMu.Unlock()

	defer func() {
		s.cmdMu.Lock()
		s.pending = nil
		s.cmdMu.Unlock()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Phase 1: send AT+CMGS=<len>
	s.writeMu.Lock()
	_, err := fmt.Fprintf(s.port, "AT+CMGS=%d\r\n", pduLen)
	s.writeMu.Unlock()
	if err != nil {
		return 0, fmt.Errorf("write CMGS command: %w", err)
	}

	// Wait for the ">" prompt.
	// The prompt arrives as "> " (two bytes); the scanner may deliver it
	// as a bare ">" line since there's no trailing newline on the prompt.
	// We wait up to 5s for the prompt.
	promptTimer := time.NewTimer(5 * time.Second)
	defer promptTimer.Stop()

	gotPrompt := false
	for !gotPrompt {
		select {
		case line := <-p.linesCh:
			if strings.HasPrefix(line, ">") {
				gotPrompt = true
			} else if line == "ERROR" || strings.HasPrefix(line, "+CME") {
				return 0, fmt.Errorf("CMGS rejected before prompt: %s", line)
			}
		case <-promptTimer.C:
			s.Close()
			return 0, ErrSendPrompt
		case <-s.closed:
			return 0, ErrClosed
		}
	}

	// Phase 2: send PDU + Ctrl+Z
	s.writeMu.Lock()
	_, err = fmt.Fprintf(s.port, "%s\x1A", pduHex)
	s.writeMu.Unlock()
	if err != nil {
		return 0, fmt.Errorf("write PDU: %w", err)
	}

	// Read the +CMGS: <mr> response followed by OK.
	var mr int
	for {
		select {
		case line := <-p.linesCh:
			switch {
			case strings.HasPrefix(line, "+CMGS:"):
				fmt.Sscanf(strings.TrimPrefix(line, "+CMGS:"), " %d", &mr)
			case line == "OK":
				return mr, nil
			case line == "ERROR":
				return 0, ErrATError
			case strings.HasPrefix(line, "+CME ERROR:"):
				code := strings.TrimSpace(strings.TrimPrefix(line, "+CME ERROR:"))
				return 0, fmt.Errorf("CME ERROR %s", code)
			case strings.HasPrefix(line, "+CMS ERROR:"):
				code := strings.TrimSpace(strings.TrimPrefix(line, "+CMS ERROR:"))
				return 0, fmt.Errorf("CMS ERROR %s", code)
			}
		case <-timer.C:
			s.Close()
			return 0, ErrTimeout
		case <-s.closed:
			return 0, ErrClosed
		}
	}
}

// Close shuts down the serializer and closes the underlying port.
func (s *Serializer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.port.Close()
	})
	return err
}

// ── Convenience wrappers for common AT commands ───────────────────────────

const (
	TimeoutPing   = 2 * time.Second
	TimeoutStd    = 5 * time.Second
	TimeoutSend   = 60 * time.Second
	TimeoutReset  = 30 * time.Second
	TimeoutRadio  = 15 * time.Second
)

// Ping sends bare AT and returns nil if OK.
func (s *Serializer) Ping() error {
	_, err := s.Execute("AT", TimeoutPing)
	return err
}

// DisableEcho sends ATE0.
func (s *Serializer) DisableEcho() error {
	_, err := s.Execute("ATE0", TimeoutPing)
	return err
}

// SetPDUMode sends AT+CMGF=0.
func (s *Serializer) SetPDUMode() error {
	_, err := s.Execute("AT+CMGF=0", TimeoutPing)
	return err
}

// EnableCMTIURCs sends AT+CNMI=2,1,0,0,0.
func (s *Serializer) EnableCMTIURCs() error {
	_, err := s.Execute("AT+CNMI=2,1,0,0,0", TimeoutPing)
	return err
}

// ReadCapabilities reads modem capabilities via AT+GCAP.
// Returns the raw capability string, e.g. "+GCAP: +CGSM,+DS,+ES".
func (s *Serializer) ReadCapabilities() (string, error) {
	lines, err := s.Execute("AT+GCAP", TimeoutStd)
	if err != nil {
		return "", err
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "+GCAP:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "+GCAP:")), nil
		}
	}
	return "", fmt.Errorf("no +GCAP in response: %w", ErrMalformedResponse)
}

// ReadICCID reads the SIM ICCID. Tries AT+CCID? then AT+ICCID? for compatibility.
func (s *Serializer) ReadICCID() (string, error) {
	for _, cmd := range []string{"AT+CCID?", "AT+ICCID?", "AT+QCCID"} {
		lines, err := s.Execute(cmd, TimeoutStd)
		if err != nil {
			continue
		}
		for _, l := range lines {
			// Response formats: "+CCID: 894..." or bare "894..."
			l = strings.TrimSpace(l)
			l = strings.TrimPrefix(l, "+CCID:")
			l = strings.TrimPrefix(l, "+ICCID:")
			l = strings.TrimSpace(l)
			if len(l) >= 19 {
				return strings.ToUpper(strings.ReplaceAll(l, " ", "")), nil
			}
		}
	}
	return "", fmt.Errorf("could not read ICCID: %w", ErrMalformedResponse)
}

// ReadIMSI reads the IMSI via AT+CIMI.
func (s *Serializer) ReadIMSI() (string, error) {
	lines, err := s.Execute("AT+CIMI", TimeoutStd)
	if err != nil {
		return "", err
	}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) == 15 {
			return l, nil
		}
	}
	return "", fmt.Errorf("could not read IMSI: %w", ErrMalformedResponse)
}

// ReadOperator reads the current operator string via AT+COPS?.
func (s *Serializer) ReadOperator() (string, error) {
	lines, err := s.Execute("AT+COPS?", TimeoutStd)
	if err != nil {
		return "", err
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "+COPS:") {
			// +COPS: 0,0,"Telekom.de",7
			parts := strings.Split(l, "\"")
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}
	return "unknown", nil
}

// SignalQuality reads RSSI via AT+CSQ. Returns RSSI in dBm.
func (s *Serializer) SignalQuality() (int, error) {
	lines, err := s.Execute("AT+CSQ", TimeoutStd)
	if err != nil {
		return 0, err
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "+CSQ:") {
			var csq, ber int
			fmt.Sscanf(strings.TrimPrefix(l, "+CSQ:"), " %d,%d", &csq, &ber)
			if csq == 99 {
				return -113, nil // unknown / not detectable
			}
			return -113 + csq*2, nil
		}
	}
	return 0, fmt.Errorf("no +CSQ in response: %w", ErrMalformedResponse)
}

// RegistrationStatus queries AT+CREG? and returns the stat value:
//
//	0 = not registered, not searching
//	1 = registered, home network
//	2 = searching
//	3 = registration denied
//	5 = registered, roaming
func (s *Serializer) RegistrationStatus() (int, error) {
	lines, err := s.Execute("AT+CREG?", TimeoutStd)
	if err != nil {
		return 0, err
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "+CREG:") {
			parts := strings.Split(strings.TrimPrefix(l, "+CREG:"), ",")
			if len(parts) >= 2 {
				var stat int
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &stat)
				return stat, nil
			}
			// +CREG: <stat> (no n parameter)
			var stat int
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(l, "+CREG:")), "%d", &stat)
			return stat, nil
		}
	}
	return 0, fmt.Errorf("no +CREG in response: %w", ErrMalformedResponse)
}

// StorageStatus reads SIM storage used/total via AT+CPMS?.
// Returns (used, total, error).
func (s *Serializer) StorageStatus() (int, int, error) {
	lines, err := s.Execute("AT+CPMS?", TimeoutStd)
	if err != nil {
		return 0, 0, err
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "+CPMS:") {
			// +CPMS: "SM",3,30,"SM",3,30,"SM",3,30
			l = strings.TrimPrefix(l, "+CPMS:")
			var used, total int
			// Skip the quoted storage name
			parts := strings.Split(l, ",")
			if len(parts) >= 3 {
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &used)
				fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &total)
				return used, total, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("could not parse +CPMS response: %w", ErrMalformedResponse)
}

// ReadSMS reads a single SMS at the given index. Returns raw PDU hex string.
// Handles multi-line responses (#1) and empty storage slots (#59).
func (s *Serializer) ReadSMS(index int) (string, error) {
	lines, err := s.Execute(fmt.Sprintf("AT+CMGR=%d", index), TimeoutStd)
	if err != nil {
		return "", err
	}

	// Empty storage: modem returns just OK with no +CMGR header.
	if len(lines) == 0 {
		return "", fmt.Errorf("empty storage slot %d: %w", index, ErrMalformedResponse)
	}

	// Find the +CMGR: header and collect all subsequent lines as PDU data.
	for i, l := range lines {
		if strings.HasPrefix(l, "+CMGR:") {
			if i+1 >= len(lines) {
				return "", fmt.Errorf("no PDU after +CMGR header: %w", ErrMalformedResponse)
			}
			// Concatenate all remaining lines — some modems split long PDUs.
			var pdu strings.Builder
			for _, part := range lines[i+1:] {
				pdu.WriteString(strings.TrimSpace(part))
			}
			hex := pdu.String()
			if hex == "" {
				return "", fmt.Errorf("empty PDU in CMGR response: %w", ErrMalformedResponse)
			}
			return hex, nil
		}
	}
	return "", fmt.Errorf("no +CMGR header in response: %w", ErrMalformedResponse)
}

// DeleteSMS deletes the SMS at the given index.
func (s *Serializer) DeleteSMS(index int) error {
	_, err := s.Execute(fmt.Sprintf("AT+CMGD=%d", index), TimeoutStd)
	return err
}

// DeleteAllSMS issues AT+CMGD=1,4 to delete all stored messages.
func (s *Serializer) DeleteAllSMS() error {
	_, err := s.Execute("AT+CMGD=1,4", TimeoutStd)
	return err
}

// RadioOff sends AT+CFUN=0 (radio disable).
func (s *Serializer) RadioOff() error {
	_, err := s.Execute("AT+CFUN=0", TimeoutStd*2)
	return err
}

// RadioOn sends AT+CFUN=1 (radio enable).
func (s *Serializer) RadioOn() error {
	_, err := s.Execute("AT+CFUN=1", TimeoutRadio)
	return err
}

// HardReset sends AT+CFUN=1,1 (full hardware reset).
func (s *Serializer) HardReset() error {
	_, err := s.Execute("AT+CFUN=1,1", TimeoutReset)
	return err
}

// ParseCMTI extracts the SMS index from a +CMTI URC.
// +CMTI: "SM",<index>
func ParseCMTI(urc string) (int, error) {
	// +CMTI: "SM",5  or  +CMTI: "ME",5
	parts := strings.Split(urc, ",")
	if len(parts) < 2 {
		return 0, fmt.Errorf("malformed CMTI: %q: %w", urc, ErrMalformedResponse)
	}
	var idx int
	_, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &idx)
	if err != nil {
		return 0, fmt.Errorf("parse CMTI index: %w", err)
	}
	// SIM storage indices are non-negative and typically 0–999.
	if idx < 0 || idx > 999 {
		return 0, fmt.Errorf("CMTI index %d out of range [0,999]: %w", idx, ErrMalformedResponse)
	}
	return idx, nil
}

// ParseCREG extracts the registration stat from a +CREG URC.
// +CREG: <stat>  or  +CREG: <n>,<stat>,...
func ParseCREG(urc string) int {
	body := strings.TrimPrefix(urc, "+CREG:")
	parts := strings.Split(strings.TrimSpace(body), ",")
	// If two or more parts, stat is the second; if one part, stat is the first.
	statStr := strings.TrimSpace(parts[0])
	if len(parts) >= 2 {
		statStr = strings.TrimSpace(parts[1])
	}
	var stat int
	fmt.Sscanf(statStr, "%d", &stat)
	return stat
}
