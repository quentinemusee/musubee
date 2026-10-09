// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sqlite opens the core's SQLite database.
//
// It uses modernc.org/sqlite, a pure-Go build of SQLite, so that the core
// compiles without cgo (see docs/ADR/0009-bridgev2-in-process.md), except
// on Android, which uses github.com/mattn/go-sqlite3 (see Driver).
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.mau.fi/util/dbutil"
)

// Settings of the connection pool; see Open.
const (
	MaxIdleConns    = 8
	ConnMaxIdleTime = 5 * time.Minute
)

// Open opens (and creates if needed) the SQLite database at path, with the
// settings bridgev2 expects: foreign keys enforced, write-ahead logging, and
// immediate write transactions so that concurrent writers wait for each other
// instead of failing with SQLITE_BUSY.
//
// The pool keeps up to MaxIdleConns connections open between queries:
// opening a connection opens the database and its write-ahead log again,
// which costs about a millisecond on Windows, and with database/sql's
// default of 2 the core reopened connections on almost every message
// (docs/ADR/0012). Idle connections close after ConnMaxIdleTime, to give
// their page caches back when the application is idle.
func Open(path string) (*dbutil.Database, error) {
	raw, err := sql.Open(driverName, dataSourceName(path))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	if err = raw.PingContext(context.Background()); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	raw.SetMaxIdleConns(MaxIdleConns)
	raw.SetConnMaxIdleTime(ConnMaxIdleTime)
	db, err := dbutil.NewWithDB(raw, "sqlite3")
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return db, nil
}
