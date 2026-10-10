// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package coretest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quentinemusee/musubee/core/secrets"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

// CheckSessionsSealed checks that the accounts' sessions are sealed in a
// core's database (docs/ADR/0019): every login's metadata is sealed, and
// neither the database nor its write-ahead log contain any of the given
// texts (a session's field names, a token's prefix). The core may still be
// running.
func CheckSessionsSealed(t *testing.T, dataDir string, texts ...string) {
	t.Helper()
	path := filepath.Join(dataDir, "core.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(t.Context(), "SELECT metadata FROM user_login")
	if err != nil {
		t.Fatal(err)
	}
	logins := 0
	for rows.Next() {
		var metadata string
		if err := rows.Scan(&metadata); err != nil {
			t.Fatal(err)
		}
		logins++
		if !secrets.IsSealed(metadata) {
			// Never print the value: it is a session.
			t.Errorf("a login's metadata is stored in clear (%d bytes)", len(metadata))
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if logins == 0 {
		t.Error("no login in the database")
	}
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, text := range texts {
			if strings.Contains(string(data), text) {
				t.Errorf("core.db%s contains %q", suffix, text)
			}
		}
	}
}
