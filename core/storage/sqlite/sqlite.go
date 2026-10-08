// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sqlite opens the core's SQLite database.
//
// It uses modernc.org/sqlite, a pure-Go build of SQLite, so that the core
// compiles without cgo on every platform (see docs/ADR/0009-bridgev2-in-process.md).
package sqlite

import (
	"database/sql"
	"fmt"
	"net/url"

	"go.mau.fi/util/dbutil"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// Open opens (and creates if needed) the SQLite database at path, with the
// settings bridgev2 expects: foreign keys enforced, write-ahead logging, and
// immediate write transactions so that concurrent writers wait for each other
// instead of failing with SQLITE_BUSY.
func Open(path string) (*dbutil.Database, error) {
	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "synchronous(NORMAL)")
	params.Add("_pragma", "busy_timeout(10000)")
	params.Set("_txlock", "immediate")
	raw, err := sql.Open("sqlite", path+"?"+params.Encode())
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
