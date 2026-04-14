// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package buffer implements the SQLite-backed offline SMS buffer (WAL mode,
// pure-Go modernc.org/sqlite driver, no CGO).
package buffer

import (
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	_ "modernc.org/sqlite"
)

// validICCID matches 19-20 digit ICCID strings.
var validICCID = regexp.MustCompile(`^\d{19,20}$`)

const schemaVersion = 1

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA synchronous  = NORMAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sms_buffer (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    iccid        TEXT    NOT NULL,
    sender       TEXT    NOT NULL,
    body         TEXT    NOT NULL,
    received_at  INTEGER NOT NULL,
    pdu_hash     TEXT    NOT NULL UNIQUE,
    smsc         TEXT,
    status       TEXT    NOT NULL DEFAULT 'PENDING',
    created_at   INTEGER NOT NULL DEFAULT (unixepoch('now') * 1000),
    delivered_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_sms_buffer_status
    ON sms_buffer (status, id);
`

// SMSRecord holds a single row from sms_buffer.
type SMSRecord struct {
	ID         int64
	ICCID      string
	Sender     string
	Body       string
	ReceivedAt int64 // Unix ms
	PDUHash    string
	SMSC       string
	Status     string
}

// Buffer is the offline SMS persistence store.
type Buffer struct {
	db     *sql.DB
	logger *slog.Logger
}

// Open opens (or creates) the SQLite database at the given path.
func Open(path string, logger *slog.Logger) (*Buffer, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Single writer; WAL allows concurrent readers.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	// Schema version tracking (#80).
	var ver int
	if err := db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("query schema version: %w", err)
	}
	if ver == 0 {
		// Fresh database or unversioned — stamp current version.
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set schema version: %w", err)
		}
	} else if ver > schemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf("buffer schema version %d is newer than supported %d — upgrade sms-gate", ver, schemaVersion)
	}

	return &Buffer{db: db, logger: logger}, nil
}

// Insert persists an incoming SMS. Returns the row ID on success.
// If pdu_hash already exists (duplicate PDU), returns (0, nil) — caller
// should proceed to SIM deletion without re-pushing to the cloud.
func (b *Buffer) Insert(iccid, sender, body, pduHash, smsc string, receivedAt int64) (int64, bool, error) {
	if !validICCID.MatchString(iccid) {
		return 0, false, fmt.Errorf("invalid ICCID format: %q", iccid)
	}
	res, err := b.db.Exec(
		`INSERT OR IGNORE INTO sms_buffer (iccid, sender, body, received_at, pdu_hash, smsc)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		iccid, sender, body, receivedAt, pduHash, smsc,
	)
	if err != nil {
		return 0, false, fmt.Errorf("insert sms: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("insert sms: last insert id: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("insert sms: rows affected: %w", err)
	}
	if rows == 0 {
		return 0, true, nil
	}
	return id, false, nil
}

// MarkDelivered sets a row's status to DELIVERED and records the delivery timestamp.
func (b *Buffer) MarkDelivered(id int64) error {
	now := time.Now().UnixMilli()
	_, err := b.db.Exec(
		`UPDATE sms_buffer SET status = 'DELIVERED', delivered_at = ? WHERE id = ?`,
		now, id,
	)
	return err
}

// PendingCount returns the number of PENDING rows.
func (b *Buffer) PendingCount() (int, error) {
	var n int
	err := b.db.QueryRow(`SELECT COUNT(*) FROM sms_buffer WHERE status = 'PENDING'`).Scan(&n)
	return n, err
}

// PendingRows returns all PENDING rows in ascending ID order.
// This is the flush payload used on reconnect.
func (b *Buffer) PendingRows() ([]SMSRecord, error) {
	rows, err := b.db.Query(
		`SELECT id, iccid, sender, body, received_at, pdu_hash, COALESCE(smsc,'')
		 FROM sms_buffer
		 WHERE status = 'PENDING'
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []SMSRecord
	for rows.Next() {
		var r SMSRecord
		r.Status = "PENDING"
		if err := rows.Scan(&r.ID, &r.ICCID, &r.Sender, &r.Body, &r.ReceivedAt, &r.PDUHash, &r.SMSC); err != nil {
			return nil, fmt.Errorf("scan pending row: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// Purge deletes DELIVERED rows older than retentionDays.
func (b *Buffer) Purge(retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).UnixMilli()
	res, err := b.db.Exec(
		`DELETE FROM sms_buffer WHERE status = 'DELIVERED' AND created_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("purge: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("purge: rows affected: %w", err)
	}
	if n > 0 {
		b.logger.Info("buffer: purged delivered rows", "count", n, "retention_days", retentionDays)
	}
	return n, nil
}

// Close closes the database.
func (b *Buffer) Close() error {
	return b.db.Close()
}
