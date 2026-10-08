// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite_test

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

// Go 1.27.0's database/sql can deadlock forever when rows are read and
// closed concurrently (golang/go#81043, fixed in 1.27.1). The core reads
// rows from many goroutines, and the deadlock showed up in our benchmark
// about once every few thousand messages (docs/ADR/0009). go.mod requires
// 1.27.1; this test also catches a build with GOTOOLCHAIN=local on 1.27.0.
func TestToolchainHasTheDatabaseSQLFix(t *testing.T) {
	if v := runtime.Version(); v == "go1.27.0" || v == "go1.27rc1" || v == "go1.27rc2" {
		t.Fatalf("%s has the database/sql deadlock golang/go#81043: use Go 1.27.1 or later", v)
	}
}

func TestOpenSettings(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := t.Context()
	for pragma, want := range map[string]string{
		"foreign_keys": "1",
		"journal_mode": "wal",
		"synchronous":  "1", // NORMAL
		"busy_timeout": "10000",
	} {
		var got string
		if err = db.QueryRow(ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Errorf("PRAGMA %s = %q (%v), want %q", pragma, got, err, want)
		}
	}
}

// TestConcurrentWriters checks that immediate transactions make concurrent
// writers wait for each other instead of failing with SQLITE_BUSY.
func TestConcurrentWriters(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := t.Context()
	if _, err = db.Exec(ctx, "CREATE TABLE counter (n INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "INSERT INTO counter (n) VALUES (0)"); err != nil {
		t.Fatal(err)
	}
	const writers, increments = 8, 50
	var wg sync.WaitGroup
	errs := make(chan error, writers*increments)
	for range writers {
		wg.Go(func() {
			for range increments {
				errs <- db.DoTxn(ctx, nil, func(ctx context.Context) error {
					var n int
					if err := db.QueryRow(ctx, "SELECT n FROM counter").Scan(&n); err != nil {
						return err
					}
					_, err := db.Exec(ctx, "UPDATE counter SET n=$1", n+1)
					return err
				})
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent transaction failed: %v", err)
		}
	}
	var n int
	if err = db.QueryRow(ctx, "SELECT n FROM counter").Scan(&n); err != nil || n != writers*increments {
		t.Errorf("counter = %d (%v), want %d: lost updates", n, err, writers*increments)
	}
}
