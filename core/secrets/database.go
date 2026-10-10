// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"go.mau.fi/util/dbutil"
)

// Purposes of the derived keys.
const (
	// PurposeLoginMetadata seals bridgev2's user_login.metadata column.
	PurposeLoginMetadata = "user_login.metadata"
	// PurposeMatrixPickleKey is the pickle key of the Matrix accounts'
	// encryption keys.
	PurposeMatrixPickleKey = "matrix/pickle-key"
	purposeKeyCheck        = "key-check"
)

// ErrWrongKey is returned when the database was written with another key.
var ErrWrongKey = errors.New("the database was written with another key: its sessions cannot be read")

// keyCheckText is sealed into the database on first use, so that a wrong
// key is detected at start instead of when a session is read.
const keyCheckText = "musubee key check"

// CheckDatabase makes sure that the database was written with this key, and
// records it on first use. The database must be open without sealing, or
// with it: the check value is in its own table.
func CheckDatabase(ctx context.Context, db *dbutil.Database, k Key) error {
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS musubee_key_check (
		id          INTEGER PRIMARY KEY CHECK (id = 1),
		check_value TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("creating the key check: %w", err)
	}
	s := NewSealer(k, purposeKeyCheck)
	var stored string
	err := db.QueryRow(ctx, "SELECT check_value FROM musubee_key_check WHERE id=1").Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = db.Exec(ctx, "INSERT INTO musubee_key_check (id, check_value) VALUES (1, $1)", s.Seal([]byte(keyCheckText)))
		if err != nil {
			return fmt.Errorf("recording the key check: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("reading the key check: %w", err)
	}
	if plain, err := s.Open(stored); err != nil || string(plain) != keyCheckText {
		return ErrWrongKey
	}
	return nil
}

// SealPlainLogins seals the sessions written before sealing, in a database
// opened with sealing (sqlite.OpenSealed), and returns how many it sealed.
// When it sealed any, it also rewrites the database file, so that the plain
// values do not stay in its free pages or its write-ahead log.
func SealPlainLogins(ctx context.Context, db *dbutil.Database) (int, error) {
	var exists bool
	if err := db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='user_login')").Scan(&exists); err != nil {
		return 0, err
	} else if !exists {
		return 0, nil
	}
	type login struct{ bridge, id, metadata string }
	var plain []login
	rows, err := db.Query(ctx, "SELECT bridge_id, id, metadata FROM user_login WHERE metadata NOT LIKE $1", SealedPrefix+"%")
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var l login
		if err := rows.Scan(&l.bridge, &l.id, &l.metadata); err != nil {
			_ = rows.Close()
			return 0, err
		}
		plain = append(plain, l)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(plain) == 0 {
		return 0, nil
	}
	err = db.DoTxn(ctx, nil, func(ctx context.Context) error {
		for _, l := range plain {
			// The sealing driver seals the value on its way to SQLite.
			if _, err := db.Exec(ctx, "UPDATE user_login SET metadata=$1 WHERE bridge_id=$2 AND id=$3", l.metadata, l.bridge, l.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("sealing the sessions: %w", err)
	}
	// VACUUM rebuilds the file from the live rows only; the checkpoints empty
	// the write-ahead log before and after.
	for _, q := range []string{"PRAGMA wal_checkpoint(TRUNCATE)", "VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err := db.Exec(ctx, q); err != nil {
			return len(plain), fmt.Errorf("rewriting the database: %w", err)
		}
	}
	return len(plain), nil
}
