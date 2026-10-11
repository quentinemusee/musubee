// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build musubee_signal

package signal

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"go.mau.fi/mautrix-signal/pkg/libsignalgo"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/store"
	"go.mau.fi/util/dbutil"

	"github.com/quentinemusee/musubee/core/secrets"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

// The device's columns, all sealed: they hold the account's keys and
// password.
var deviceColumns = []string{
	"aci_identity_key_pair", "pni_identity_key_pair", "password", "master_key",
	"account_record", "account_entropy_pool", "ephemeral_backup_key", "media_root_backup_key",
}

// The store, on the core's SQLite database, seals with the master key: the
// raw values are sealed, they open with the key, and not with another one.
func TestStoreSealedWithTheKey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	key := secrets.NewKey()
	container, db := openStore(t, path, key)

	aci, err := libsignalgo.GenerateIdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pni, err := libsignalgo.GenerateIdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	data := &store.DeviceData{
		ACIIdentityKeyPair: aci,
		PNIIdentityKeyPair: pni,
		ACI:                uuid.New(),
		PNI:                uuid.New(),
		DeviceID:           2,
		Password:           "test-password",
		MasterKey:          bytes.Repeat([]byte{7}, 32),
		AccountEntropyPool: "test-account-entropy-pool",
		EphemeralBackupKey: libsignalgo.GenerateRandomBackupKey(),
		MediaRootBackupKey: libsignalgo.GenerateRandomBackupKey(),
	}
	if err = container.PutDevice(ctx, data); err != nil {
		t.Fatal(err)
	}
	for _, column := range deviceColumns {
		var raw []byte
		if err = db.QueryRow(ctx, fmt.Sprintf("SELECT %s FROM signalmeow_device", column)).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		// account_record is NULL: the device has none.
		if raw != nil && !secrets.IsSealed(string(raw)) {
			t.Errorf("%s is stored unsealed", column)
		}
	}
	device, err := container.DeviceByACI(ctx, data.ACI)
	if err != nil {
		t.Fatal(err)
	}
	if device.Password != data.Password || !bytes.Equal(device.MasterKey, data.MasterKey) ||
		device.AccountEntropyPool != data.AccountEntropyPool {
		t.Error("the device does not read back as written")
	}
	_ = db.Close()

	// Another key does not open the store.
	other, _ := openStore(t, path, secrets.NewKey())
	if _, err = other.DeviceByACI(ctx, data.ACI); err == nil {
		t.Error("the store opened with another key")
	}
}

func TestStoreSealer(t *testing.T) {
	s := newStoreSealer(secrets.NewKey())
	sealed := s.Seal(store.PurposeSessionRecord, []byte("record"))
	if !secrets.IsSealed(string(sealed)) {
		t.Fatalf("sealed value %q has no prefix", sealed)
	}
	if plain, err := s.Open(store.PurposeSessionRecord, sealed); err != nil || string(plain) != "record" {
		t.Errorf("Open: %q, %v", plain, err)
	}
	// A value sealed for one column does not open for another.
	if _, err := s.Open(store.PurposePreKey, sealed); err == nil {
		t.Error("a session record opened as a pre-key")
	}
	// An empty value opens empty, not nil (NULL in the store).
	plain, err := s.Open(store.PurposeEventPlaintext, s.Seal(store.PurposeEventPlaintext, []byte{}))
	if err != nil || plain == nil || len(plain) != 0 {
		t.Errorf("empty value: %v, %v", plain, err)
	}
	// A plain value is refused.
	if _, err = s.Open(store.PurposeSessionRecord, []byte("record")); err == nil {
		t.Error("a plain value opened")
	}
}

func openStore(t *testing.T, path string, key secrets.Key) (*store.Container, *dbutil.Database) {
	t.Helper()
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	container := store.NewStore(db, dbutil.NoopLogger)
	container.SetSealer(newStoreSealer(key))
	if err = container.Upgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	return container, db
}
