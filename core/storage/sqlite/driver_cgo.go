// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build android || musubee_cgo_sqlite

package sqlite

import (
	"net/url"

	_ "github.com/mattn/go-sqlite3" // registers the "sqlite3" database/sql driver
)

// Driver names the SQLite build compiled in: github.com/mattn/go-sqlite3,
// the C amalgamation built with cgo.
//
// Android uses it: modernc.org/libc runs musl's code on linux/amd64, and
// musl makes system calls that Android's seccomp filter forbids on x86_64
// (lstat, stat, access, ...), which kills the process (docs/ADR/0011).
// Android builds need cgo anyway, for the JNI entry points. The
// musubee_cgo_sqlite tag selects this driver on other platforms, to test it.
const Driver = "mattn"

const driverName = "sqlite3"

func dataSourceName(path string) string {
	params := url.Values{}
	params.Set("_foreign_keys", "1")
	params.Set("_journal_mode", "WAL")
	params.Set("_synchronous", "NORMAL")
	params.Set("_busy_timeout", "10000")
	params.Set("_txlock", "immediate")
	return path + "?" + params.Encode()
}
