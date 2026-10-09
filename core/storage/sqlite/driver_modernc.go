// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !(android || musubee_cgo_sqlite)

package sqlite

import (
	"net/url"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// Driver names the SQLite build compiled in: modernc.org/sqlite, SQLite
// transpiled to Go, which needs no C toolchain.
const Driver = "modernc"

const driverName = "sqlite"

func dataSourceName(path string) string {
	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "synchronous(NORMAL)")
	params.Add("_pragma", "busy_timeout(10000)")
	params.Set("_txlock", "immediate")
	return path + "?" + params.Encode()
}
