// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/secrets"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

// openWithKey opens a core whose master key comes from the application, as
// on Android.
func openWithKey(t *testing.T, dir, key string) *Core {
	t.Helper()
	c, err := tryOpenWithKey(dir, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func tryOpenWithKey(dir, key string) (*Core, error) {
	cfg, err := json.Marshal(Config{DataDir: dir, LogLevel: "debug", EchoDelayMS: int(echoDelay / time.Millisecond), DatabaseKey: key})
	if err != nil {
		return nil, err
	}
	return Open(cfg)
}

func randomKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, secrets.KeySize)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// storedMetadata reads the logins' metadata as stored in the file, without
// opening the seals.
func storedMetadata(t *testing.T, dir string) []string {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(dir, "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(context.Background(), "SELECT metadata FROM user_login")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func waitConnected(t *testing.T, c *Core, account string) {
	t.Helper()
	waitEvent(t, c, "the account connected", func(e rawEvent) bool {
		a := decode[api.AccountEvent](t, e.Data).Account
		return e.Type == api.EventAccountUpdated && a.AccountID == account && a.State == api.AccountStateConnected
	})
}

// TestSessionsAreSealed checks the acceptance of T2.3 with the echo network:
// the logins' sessions are sealed in the file, the core reads them back
// after a restart, and another key cannot.
func TestSessionsAreSealed(t *testing.T) {
	dir, key := t.TempDir(), randomKey(t)
	c := openWithKey(t, dir, key)
	account := login(t, c)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	stored := storedMetadata(t, dir)
	if len(stored) != 1 || !secrets.IsSealed(stored[0]) {
		t.Fatalf("stored metadata = %q", stored)
	}
	if _, err := os.Stat(filepath.Join(dir, secrets.KeyFileName)); !os.IsNotExist(err) {
		t.Errorf("the core wrote a key file although the application gave the key: %v", err)
	}

	c = openWithKey(t, dir, key)
	waitConnected(t, c, account)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	// Another key is refused at start, before any session is read; so is
	// the core's own key file, which is another key.
	for _, other := range []string{randomKey(t), ""} {
		if c, err := tryOpenWithKey(dir, other); err == nil {
			_ = c.Close()
			t.Errorf("the core opened with another key (%t)", other != "")
		} else if !strings.Contains(err.Error(), secrets.ErrWrongKey.Error()) {
			t.Errorf("another key: %v", err)
		}
	}
	for _, bad := range []string{"not base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if c, err := tryOpenWithKey(t.TempDir(), bad); err == nil {
			_ = c.Close()
			t.Errorf("the core opened with the key %q", bad)
		} else if strings.Contains(err.Error(), bad) {
			t.Errorf("the error quotes the key: %v", err)
		}
	}
}

// TestCoreKeyFile checks the key the core keeps itself when the
// application gives none: DPAPI on Windows.
func TestCoreKeyFile(t *testing.T) {
	dir := t.TempDir()
	c := openDir(t, dir)
	account := login(t, c)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, secrets.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Protection secrets.Protection `json:"protection"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if want := secrets.ProtectionNone; runtime.GOOS == "windows" && f.Protection != secrets.ProtectionDPAPI || runtime.GOOS != "windows" && f.Protection != want {
		t.Errorf("protection = %s on %s", f.Protection, runtime.GOOS)
	}
	if stored := storedMetadata(t, dir); len(stored) != 1 || !secrets.IsSealed(stored[0]) {
		t.Fatalf("stored metadata = %q", stored)
	}
	c = openDir(t, dir)
	waitConnected(t, c, account)
}

// TestPlainSessionsAreSealedAtStart checks the migration of a database
// written before sealing: its sessions are sealed at start, and no copy of
// them stays in the file.
func TestPlainSessionsAreSealedAtStart(t *testing.T) {
	dir, key := t.TempDir(), randomKey(t)
	c := openWithKey(t, dir, key)
	account := login(t, c)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	// Write the session in clear, as the core did before T2.3.
	marker := make([]byte, 8)
	_, _ = rand.Read(marker)
	plainMarker := "plain-session-" + hex.EncodeToString(marker)
	db, err := sqlite.Open(filepath.Join(dir, "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.Exec(ctx, "UPDATE user_login SET metadata=json_object('marker', $1)", plainMarker); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if !fileContains(t, dir, plainMarker) {
		t.Fatal("the test did not write the plain session")
	}

	c = openWithKey(t, dir, key)
	waitConnected(t, c, account)
	if stored := storedMetadata(t, dir); len(stored) != 1 || !secrets.IsSealed(stored[0]) {
		t.Fatalf("stored metadata after the migration = %q", stored)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if fileContains(t, dir, plainMarker) {
		t.Error("the plain session stays in the database files")
	}
}

// fileContains reports whether the database or its write-ahead log contain
// a text.
func fileContains(t *testing.T, dir, text string) bool {
	t.Helper()
	for _, name := range []string{"core.db", "core.db-wal"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.Contains(string(data), text) {
			return true
		}
	}
	return false
}
