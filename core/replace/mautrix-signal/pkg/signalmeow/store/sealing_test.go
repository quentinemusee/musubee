// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Musubee: this file is not part of mautrix-signal (see MUSUBEE.md at the root
// of this copy). It checks that every secret column is sealed in the
// database, on a real SQLite database and with real libsignal objects.

package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/util/dbutil"

	"go.mau.fi/mautrix-signal/pkg/libsignalgo"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/protobuf/backuppb"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/protobuf/signalpb"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/types"
)

// testSealer marks each value with its purpose, so the test can recognise
// a sealed value in the raw rows and a value opened for the wrong column.
// It is not encryption: Musubee's real sealer is AES-256-GCM (core/secrets).
type testSealer struct{}

func testPrefix(purpose string) string { return "test-sealed:" + purpose + ":" }

func (testSealer) Seal(purpose string, plain []byte) []byte {
	return []byte(testPrefix(purpose) + hex.EncodeToString(plain))
}

func (testSealer) Open(purpose string, sealed []byte) ([]byte, error) {
	rest, ok := strings.CutPrefix(string(sealed), testPrefix(purpose))
	if !ok {
		return nil, errors.New("not sealed for this column")
	}
	return hex.DecodeString(rest)
}

func openTestStore(t *testing.T) (*Container, *dbutil.Database) {
	t.Helper()
	db, err := dbutil.NewWithDialect("file:"+filepath.Join(t.TempDir(), "signal.db")+"?_foreign_keys=on", "sqlite3")
	require.NoError(t, err)
	db.RawDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	container := NewStore(db, dbutil.NoopLogger)
	require.NoError(t, container.Upgrade(context.Background()))
	return container, db
}

func newTestDevice(t *testing.T) *DeviceData {
	t.Helper()
	aci, err := libsignalgo.GenerateIdentityKeyPair()
	require.NoError(t, err)
	pni, err := libsignalgo.GenerateIdentityKeyPair()
	require.NoError(t, err)
	return &DeviceData{
		ACIIdentityKeyPair: aci,
		PNIIdentityKeyPair: pni,
		ACIRegistrationID:  1234,
		PNIRegistrationID:  5678,
		ACI:                uuid.New(),
		PNI:                uuid.New(),
		DeviceID:           2,
		Number:             "+15555550100",
		Password:           "test-password",
		MasterKey:          bytes.Repeat([]byte{7}, 32),
		AccountRecord:      &signalpb.AccountRecord{GivenName: "Test"},
		AccountEntropyPool: "test-account-entropy-pool",
		EphemeralBackupKey: libsignalgo.GenerateRandomBackupKey(),
		MediaRootBackupKey: libsignalgo.GenerateRandomBackupKey(),
	}
}

// fillStore writes a value in every sealed column through the store's own
// methods, and returns what reading it back must give.
func fillStore(t *testing.T, ctx context.Context, container *Container) *Device {
	t.Helper()
	data := newTestDevice(t)
	require.NoError(t, container.PutDevice(ctx, data))
	device, err := container.DeviceByACI(ctx, data.ACI)
	require.NoError(t, err)
	require.NotNil(t, device)

	// The device's own keys and password.
	assertSameKeyPair(t, data.ACIIdentityKeyPair, device.ACIIdentityKeyPair)
	assertSameKeyPair(t, data.PNIIdentityKeyPair, device.PNIIdentityKeyPair)
	assert.Equal(t, data.Number, device.Number)
	assert.Equal(t, data.Password, device.Password)
	assert.Equal(t, data.MasterKey, device.MasterKey)
	assert.Equal(t, data.AccountRecord.GetGivenName(), device.AccountRecord.GetGivenName())
	assert.Equal(t, data.AccountEntropyPool, device.AccountEntropyPool)
	assert.Equal(t, data.EphemeralBackupKey.Slice(), device.EphemeralBackupKey.Slice())
	assert.Equal(t, data.MediaRootBackupKey.Slice(), device.MediaRootBackupKey.Slice())

	// A session with another account, from its pre-key bundle.
	ourAddress, err := libsignalgo.NewACIServiceID(data.ACI).Address(uint(data.DeviceID))
	require.NoError(t, err)
	theirACI := uuid.New()
	theirAddress, err := libsignalgo.NewACIServiceID(theirACI).Address(1)
	require.NoError(t, err)
	bundle := newTestBundle(t)
	require.NoError(t, libsignalgo.ProcessPreKeyBundle(ctx, bundle, theirAddress, ourAddress, device.ACISessionStore, device.ACIIdentityStore))
	session, err := device.ACISessionStore.LoadSession(ctx, theirAddress)
	require.NoError(t, err)
	require.NotNil(t, session)
	hasState, err := session.HasCurrentState()
	require.NoError(t, err)
	assert.True(t, hasState)

	// Our pre-keys.
	private, err := libsignalgo.GeneratePrivateKey()
	require.NoError(t, err)
	preKey, err := libsignalgo.NewPreKeyRecordFromPrivateKey(11, private)
	require.NoError(t, err)
	require.NoError(t, device.ACIPreKeyStore.StorePreKey(ctx, 11, preKey))
	signedPreKey, err := libsignalgo.NewSignedPreKeyRecordFromPrivateKey(12, time.UnixMilli(42000), private, []byte("signature"))
	require.NoError(t, err)
	require.NoError(t, device.ACIPreKeyStore.StoreSignedPreKey(ctx, 12, signedPreKey))
	kyberKeyPair, err := libsignalgo.KyberKeyPairGenerate()
	require.NoError(t, err)
	kyberPreKey, err := libsignalgo.NewKyberPreKeyRecord(13, time.UnixMilli(42000), kyberKeyPair, []byte("signature"))
	require.NoError(t, err)
	require.NoError(t, device.ACIPreKeyStore.StoreKyberPreKey(ctx, 13, kyberPreKey))
	loadedPreKey, err := device.ACIPreKeyStore.LoadPreKey(ctx, 11)
	require.NoError(t, err)
	assertSameSerialization(t, preKey, loadedPreKey)
	loadedSignedPreKey, err := device.ACIPreKeyStore.LoadSignedPreKey(ctx, 12)
	require.NoError(t, err)
	assertSameSerialization(t, signedPreKey, loadedSignedPreKey)
	loadedKyberPreKey, err := device.ACIPreKeyStore.LoadKyberPreKey(ctx, 13)
	require.NoError(t, err)
	assertSameSerialization(t, kyberPreKey, loadedKyberPreKey)

	// Our sender key for a group.
	distributionID := uuid.New()
	_, err = libsignalgo.NewSenderKeyDistributionMessage(ctx, ourAddress, distributionID, device.SenderKeyStore)
	require.NoError(t, err)
	senderKey, err := device.SenderKeyStore.LoadSenderKey(ctx, ourAddress, distributionID)
	require.NoError(t, err)
	require.NotNil(t, senderKey)

	// A contact's profile key, alone and with the rest of the contact.
	profileKey := libsignalgo.ProfileKey(bytes.Repeat([]byte{9}, libsignalgo.ProfileKeyLength))
	require.NoError(t, device.RecipientStore.StoreProfileKey(ctx, theirACI, profileKey))
	loadedProfileKey, err := device.RecipientStore.LoadProfileKey(ctx, theirACI)
	require.NoError(t, err)
	require.NotNil(t, loadedProfileKey)
	assert.Equal(t, profileKey, *loadedProfileKey)
	contact, err := device.RecipientStore.LoadAndUpdateRecipient(ctx, theirACI, uuid.Nil, func(r *types.Recipient) (bool, error) {
		r.Profile.Name = "Contact"
		return true, nil
	})
	require.NoError(t, err)
	assert.Equal(t, profileKey, contact.Profile.Key)
	reloaded, err := device.sqlStore.LoadRecipientByACI(ctx, theirACI)
	require.NoError(t, err)
	assert.Equal(t, profileKey, reloaded.Profile.Key)

	// A message's content in the event buffer.
	hash := [32]byte{1}
	require.NoError(t, device.EventBuffer.PutBufferedEvent(ctx, hash, []byte("message content"), 1000))
	event, err := device.EventBuffer.GetBufferedEvent(ctx, hash)
	require.NoError(t, err)
	assert.Equal(t, []byte("message content"), event.Plaintext)

	// A backup with two groups, a chat in the second, and a message. Storing
	// a group's backup recipient also stores its master key.
	firstKey, firstID := newTestGroup(t, 1)
	secondKey, secondID := newTestGroup(t, 2)
	require.NoError(t, device.BackupStore.AddBackupRecipient(ctx, groupRecipient(1, firstKey)))
	require.NoError(t, device.BackupStore.AddBackupRecipient(ctx, groupRecipient(2, secondKey)))
	require.NoError(t, device.BackupStore.AddBackupChat(ctx, &backuppb.Chat{Id: 20, RecipientId: 2}))
	require.NoError(t, device.BackupStore.AddBackupChatItem(ctx, &backuppb.ChatItem{ChatId: 20, AuthorId: 2, DateSent: 1000}))
	require.NoError(t, device.BackupStore.RecalculateChatCounts(ctx))
	masterKey, err := device.GroupStore.MasterKeyFromGroupIdentifier(ctx, secondID)
	require.NoError(t, err)
	assert.Equal(t, types.SerializedGroupMasterKey(base64.StdEncoding.EncodeToString(secondKey)), masterKey)
	chat, err := device.BackupStore.GetBackupChatByGroupID(ctx, secondID)
	require.NoError(t, err)
	require.NotNil(t, chat)
	assert.Equal(t, uint64(20), chat.Id)
	assert.Equal(t, 1, chat.TotalMessages)
	noChat, err := device.BackupStore.GetBackupChatByGroupID(ctx, firstID)
	require.NoError(t, err)
	assert.Nil(t, noChat)
	backupRecipient, err := device.BackupStore.GetBackupRecipient(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, secondKey, backupRecipient.GetGroup().GetMasterKey())
	items, err := device.BackupStore.GetBackupChatItems(ctx, 20, time.Time{}, false, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, uint64(1000), items[0].DateSent)
	return device
}

func TestSealedColumns(t *testing.T) {
	ctx := context.Background()
	container, db := openTestStore(t)
	container.SetSealer(testSealer{})
	fillStore(t, ctx, container)

	for _, purpose := range Purposes {
		values := rawValues(t, db, purpose)
		if assert.NotEmpty(t, values, "%s holds no value: the test does not cover it", purpose) {
			for _, v := range values {
				assert.True(t, strings.HasPrefix(string(v), testPrefix(purpose)), "%s is stored unsealed", purpose)
			}
		}
	}
}

func TestSealedColumnsWithoutSealer(t *testing.T) {
	// Without a sealer, the store keeps upstream's behaviour.
	ctx := context.Background()
	container, db := openTestStore(t)
	device := fillStore(t, ctx, container)

	assert.Equal(t, [][]byte{[]byte(device.Password)}, rawValues(t, db, PurposeDevicePassword))
	for _, purpose := range Purposes {
		for _, v := range rawValues(t, db, purpose) {
			assert.False(t, strings.HasPrefix(string(v), "test-sealed:"), "%s is sealed without a sealer", purpose)
		}
	}
}

func TestSealedColumnRefusesUnsealedValue(t *testing.T) {
	// A value written in clear (or sealed for another column) is an error
	// under a sealer, never read as it is.
	ctx := context.Background()
	container, db := openTestStore(t)
	data := newTestDevice(t)
	require.NoError(t, container.PutDevice(ctx, data))

	container.SetSealer(testSealer{})
	_, err := container.DeviceByACI(ctx, data.ACI)
	require.Error(t, err)
	assert.Contains(t, err.Error(), PurposeDeviceACIIdentityKeyPair)

	require.NoError(t, container.PutDevice(ctx, data))
	_, err = db.Exec(ctx, "UPDATE signalmeow_device SET password=master_key")
	require.NoError(t, err)
	_, err = container.DeviceByACI(ctx, data.ACI)
	require.Error(t, err)
	assert.Contains(t, err.Error(), PurposeDevicePassword)
}

func TestClearedEventPlaintextStaysNull(t *testing.T) {
	// The event buffer marks a handled event by clearing its plaintext, and
	// deletes old events by testing for NULL: sealing must keep NULL.
	ctx := context.Background()
	container, db := openTestStore(t)
	container.SetSealer(testSealer{})
	data := newTestDevice(t)
	require.NoError(t, container.PutDevice(ctx, data))
	device, err := container.DeviceByACI(ctx, data.ACI)
	require.NoError(t, err)

	hash := [32]byte{2}
	require.NoError(t, device.EventBuffer.PutBufferedEvent(ctx, hash, []byte("message content"), 1000))
	require.NoError(t, device.EventBuffer.ClearBufferedEventPlaintext(ctx, hash))
	event, err := device.EventBuffer.GetBufferedEvent(ctx, hash)
	require.NoError(t, err)
	assert.Nil(t, event.Plaintext)
	require.NoError(t, device.EventBuffer.DeleteBufferedEventsOlderThan(ctx, time.Now().Add(time.Hour)))
	assert.Empty(t, rawValues(t, db, "signalmeow_event_buffer.ciphertext_hash"))
}

// rawValues returns the values of a column ("table.column") that are not
// NULL, as stored.
func rawValues(t *testing.T, db *dbutil.Database, tableColumn string) [][]byte {
	t.Helper()
	table, column, ok := strings.Cut(tableColumn, ".")
	require.True(t, ok, tableColumn)
	rows, err := db.Query(context.Background(), fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL", column, table, column))
	require.NoError(t, err)
	values, err := dbutil.NewRowIterWithError(rows, func(row dbutil.Scannable) (v []byte, err error) {
		err = row.Scan(&v)
		return
	}, err).AsList()
	require.NoError(t, err)
	return values
}

type serializable interface{ Serialize() ([]byte, error) }

func assertSameSerialization(t *testing.T, want, got serializable) {
	t.Helper()
	require.NotNil(t, got)
	wantBytes, err := want.Serialize()
	require.NoError(t, err)
	gotBytes, err := got.Serialize()
	require.NoError(t, err)
	assert.Equal(t, wantBytes, gotBytes)
}

func assertSameKeyPair(t *testing.T, want, got *libsignalgo.IdentityKeyPair) {
	t.Helper()
	assertSameSerialization(t, want, got)
}

// newTestBundle returns another account's pre-key bundle.
func newTestBundle(t *testing.T) *libsignalgo.PreKeyBundle {
	t.Helper()
	identity, err := libsignalgo.GenerateIdentityKeyPair()
	require.NoError(t, err)
	preKey, err := libsignalgo.GeneratePrivateKey()
	require.NoError(t, err)
	preKeyPublic, err := preKey.GetPublicKey()
	require.NoError(t, err)
	signedPreKey, err := libsignalgo.GeneratePrivateKey()
	require.NoError(t, err)
	signedPreKeyPublic, err := signedPreKey.GetPublicKey()
	require.NoError(t, err)
	signedPreKeyBytes, err := signedPreKeyPublic.Serialize()
	require.NoError(t, err)
	signedPreKeySignature, err := identity.GetPrivateKey().Sign(signedPreKeyBytes)
	require.NoError(t, err)
	kyberPreKey, err := libsignalgo.KyberKeyPairGenerate()
	require.NoError(t, err)
	kyberPreKeyPublic, err := kyberPreKey.GetPublicKey()
	require.NoError(t, err)
	kyberPreKeyBytes, err := kyberPreKeyPublic.Serialize()
	require.NoError(t, err)
	kyberPreKeySignature, err := identity.GetPrivateKey().Sign(kyberPreKeyBytes)
	require.NoError(t, err)
	identityKey, err := libsignalgo.NewIdentityKeyFromPublicKey(identity.GetPublicKey())
	require.NoError(t, err)
	bundle, err := libsignalgo.NewPreKeyBundle(
		4321, 1,
		21, preKeyPublic,
		22, signedPreKeyPublic, signedPreKeySignature,
		23, kyberPreKeyPublic, kyberPreKeySignature,
		identityKey,
	)
	require.NoError(t, err)
	return bundle
}

func newTestGroup(t *testing.T, seed byte) ([]byte, types.GroupIdentifier) {
	t.Helper()
	key := bytes.Repeat([]byte{seed}, libsignalgo.GroupMasterKeyLength)
	id, err := libsignalgo.GroupMasterKey(key).GroupIdentifier()
	require.NoError(t, err)
	return key, types.BytesToGroupIdentifier(id)
}

func groupRecipient(id uint64, masterKey []byte) *backuppb.Recipient {
	return &backuppb.Recipient{
		Id:          id,
		Destination: &backuppb.Recipient_Group{Group: &backuppb.Group{MasterKey: masterKey}},
	}
}
