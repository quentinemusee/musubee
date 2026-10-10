// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/util/dbutil"

	"github.com/quentinemusee/musubee/core/secrets"
)

// The queries of bridgev2 v0.31.0 on user_login (bridgev2/database/userlogin.go).
const (
	bridgev2Insert = `
		INSERT INTO user_login (bridge_id, user_mxid, id, remote_name, remote_profile, space_room, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	bridgev2Update = `
		UPDATE user_login SET remote_name=$4, remote_profile=$5, space_room=$6, metadata=$7
		WHERE bridge_id=$1 AND id=$3
	`
	bridgev2Select = `
		SELECT bridge_id, user_mxid, id, remote_name, remote_profile, space_room, metadata FROM user_login
	`
	bridgev2Delete = `DELETE FROM user_login WHERE bridge_id=$1 AND id=$2`
)

func TestSealedArgument(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int
	}{
		{bridgev2Insert, 7},
		{bridgev2Update, 7},
		{bridgev2Select, 0},
		{bridgev2Delete, 0},
		{"UPDATE user_login SET remote_name=$1 WHERE id=$2", 0},
		{"INSERT INTO user_portal (bridge_id, login_id) VALUES ($1, $2)", 0},
		{"UPDATE portal SET metadata=$2 WHERE id=$1", 0},
		{"INSERT INTO user_login (metadata, id) VALUES ($2, $1) ON CONFLICT (id) DO UPDATE SET metadata=excluded.metadata", 2},
		{`insert or replace into "user_login" (id, metadata) values ($1, $2);`, 2},
		{"UPDATE user_login SET metadata = $3 WHERE id=$1", 3},
		// As dbutil passes them to SQLite.
		{"INSERT INTO user_login (id, metadata) VALUES (?1, ?2)", 2},
		{"UPDATE user_login SET remote_name=?4, metadata=?7 WHERE id=?3", 7},
		{"ALTER TABLE user_login ADD COLUMN extra TEXT", 0},
	} {
		got, err := sealedArgument(tc.query)
		if err != nil || got != tc.want {
			t.Errorf("sealedArgument(%q) = %d, %v; want %d", tc.query, got, err, tc.want)
		}
	}
	// Writes that would not be sealed fail rather than store the session in
	// clear.
	for _, query := range []string{
		"UPDATE user_login SET metadata=json_set(metadata, '$.x', 1)",
		"UPDATE user_login SET metadata=?",
		"INSERT INTO user_login (id, metadata) VALUES (?, ?)",
		"INSERT INTO user_login (id, metadata) VALUES ($1, json($2))",
		"INSERT INTO user_login (id, metadata) SELECT id, metadata FROM old_login",
		"INSERT INTO user_login VALUES ($1, $2)",
		"UPDATE user_login SET metadata=$2 WHERE metadata=$1",
		"CREATE TABLE x (a); UPDATE user_login SET metadata=$1",
		"UPDATE user_login SET metadata=$1; UPDATE user_login SET metadata=$2",
	} {
		if got, err := sealedArgument(query); err == nil {
			t.Errorf("sealedArgument(%q) = %d, want an error", query, got)
		}
	}
}

const schema = `CREATE TABLE user_login (
	bridge_id TEXT, user_mxid TEXT, id TEXT, remote_name TEXT, remote_profile TEXT, space_room TEXT,
	metadata TEXT NOT NULL, PRIMARY KEY (bridge_id, id)
)`

func openPair(t *testing.T, key secrets.Key) (sealed, raw *dbutil.Database, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "core.db")
	sealed, err := OpenSealed(path, secrets.NewSealer(key, "user_login.metadata"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sealed.Close() })
	raw, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if _, err := sealed.Exec(t.Context(), schema); err != nil {
		t.Fatal(err)
	}
	return sealed, raw, path
}

func readMetadata(t *testing.T, db *dbutil.Database, id string) (string, error) {
	t.Helper()
	var bridge, mxid, gotID, name, profile, room, metadata string
	err := db.QueryRow(t.Context(), bridgev2Select+" WHERE id=$1", id).Scan(&bridge, &mxid, &gotID, &name, &profile, &room, &metadata)
	return metadata, err
}

func TestSealedColumn(t *testing.T) {
	ctx := t.Context()
	key := secrets.NewKey()
	db, raw, path := openPair(t, key)
	secret := `{"session":{"auth_key":"c2VjcmV0IGtleQ=="}}`

	// Insert through bridgev2's query and the way bridgev2 passes metadata.
	var meta map[string]any
	if err := (dbutil.JSON{Data: &meta}).Scan(secret); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, bridgev2Insert, "telegram", "@me:x", "1", "Me", "", "", dbutil.JSON{Data: meta}); err != nil {
		t.Fatal(err)
	}
	stored, err := readMetadata(t, raw, "1")
	if err != nil || !secrets.IsSealed(stored) || strings.Contains(stored, "auth_key") {
		t.Fatalf("stored metadata = %q, %v", stored, err)
	}
	read, err := readMetadata(t, db, "1")
	if err != nil || read != `{"session":{"auth_key":"c2VjcmV0IGtleQ=="}}` {
		t.Fatalf("read metadata = %q, %v", read, err)
	}

	// Update, through a prepared statement this time, in the form dbutil
	// passes to the driver ($N rewritten as ?N: SQLite reads $7 as a name).
	stmt, err := db.RawDB.PrepareContext(ctx, strings.ReplaceAll(bridgev2Update, "$", "?"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stmt.Close() }()
	if _, err := stmt.ExecContext(ctx, "telegram", "@me:x", "1", "Me", "", "", []byte(`{"updated":true}`)); err != nil {
		t.Fatal(err)
	}
	if stored, _ := readMetadata(t, raw, "1"); !secrets.IsSealed(stored) {
		t.Fatalf("updated metadata stored as %q", stored)
	}
	if read, _ := readMetadata(t, db, "1"); read != `{"updated":true}` {
		t.Fatalf("updated metadata read as %q", read)
	}

	// A value written before sealing is read as it is.
	if _, err := raw.Exec(ctx, bridgev2Insert, "telegram", "@me:x", "2", "Me", "", "", `{"old":true}`); err != nil {
		t.Fatal(err)
	}
	if read, err := readMetadata(t, db, "2"); err != nil || read != `{"old":true}` {
		t.Fatalf("plain metadata read as %q, %v", read, err)
	}

	// Writes the sealer cannot follow fail, and write nothing.
	if _, err := db.Exec(ctx, "UPDATE user_login SET metadata=json_set(metadata, '$.x', 1)"); err == nil {
		t.Error("an unsupported write succeeded")
	}

	// Another key reads an error, never the value.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := OpenSealed(path, secrets.NewSealer(secrets.NewKey(), "user_login.metadata"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if read, err := readMetadata(t, other, "1"); err == nil {
		t.Errorf("another key read %q", read)
	}

	// The file holds no plain session.
	_ = raw.Close()
	_ = other.Close()
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "auth_key") || strings.Contains(string(data), "updated") {
			t.Errorf("core.db%s holds a session in clear", suffix)
		}
	}
}

// TestSealedPoolOptions checks that the sealing pool keeps the settings of
// Open.
func TestSealedPoolOptions(t *testing.T) {
	db, _, _ := openPair(t, secrets.NewKey())
	ctx := context.Background()
	for pragma, want := range map[string]string{"foreign_keys": "1", "journal_mode": "wal", "busy_timeout": "10000"} {
		var got string
		if err := db.QueryRow(ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Errorf("PRAGMA %s = %q (%v), want %q", pragma, got, err, want)
		}
	}
}
