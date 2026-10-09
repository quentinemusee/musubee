// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sqlite opens the core's SQLite database.
//
// It uses modernc.org/sqlite, a pure-Go build of SQLite, so that the core
// compiles without cgo (see docs/ADR/0009-bridgev2-in-process.md), except
// on Android, which uses github.com/mattn/go-sqlite3 (see Driver).
package sqlite

import (
	"database/sql"
	"fmt"

	"go.mau.fi/util/dbutil"
)

// Open opens (and creates if needed) the SQLite database at path, with the
// settings bridgev2 expects: foreign keys enforced, write-ahead logging, and
// immediate write transactions so that concurrent writers wait for each other
// instead of failing with SQLITE_BUSY.
func Open(path string) (*dbutil.Database, error) {
	raw, err := sql.Open(driverName, dataSourceName(path))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	if err = raw.Ping(); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	db, err := dbutil.NewWithDB(raw, "sqlite3")
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return db, nil
}
