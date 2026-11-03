// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package matrixtest

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/backup"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
	"github.com/quentinemusee/musubee/infra/coretest"
)

// setUpRecovery does what alice's other Matrix app does when she turns on
// secure backup: secret storage with a recovery key, cross-signing keys,
// and a key backup whose key is in secret storage. It returns the
// recovery key.
func setUpRecovery(t *testing.T, ctx context.Context, localpart, password string) string {
	t.Helper()
	cli, err := env.Login(ctx, localpart, password)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "other-app.db"))
	if err != nil {
		t.Fatal(err)
	}
	helper, err := cryptohelper.NewCryptoHelper(cli, []byte("alice's other app"), db)
	if err != nil {
		t.Fatal(err)
	}
	if err = helper.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Close() })
	mach := helper.Machine()
	recoveryKey, _, err := mach.GenerateAndUploadCrossSigningKeys(ctx, func(uia *mautrix.RespUserInteractive) interface{} {
		return &mautrix.ReqUIAuthLogin{
			BaseAuthData: mautrix.BaseAuthData{Type: mautrix.AuthTypePassword, Session: uia.Session},
			User:         cli.UserID.String(),
			Password:     password,
		}
	}, "")
	if err != nil {
		t.Fatalf("setting up cross-signing: %v", err)
	}
	backupKey, err := backup.NewMegolmBackupKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cli.CreateKeyBackupVersion(ctx, &mautrix.ReqRoomKeysVersionCreate[backup.MegolmAuthData]{
		Algorithm: id.KeyBackupAlgorithmMegolmBackupV1,
		AuthData:  backup.MegolmAuthData{PublicKey: id.Ed25519(base64.RawStdEncoding.EncodeToString(backupKey.PublicKey().Bytes()))},
	}); err != nil {
		t.Fatalf("creating the key backup: %v", err)
	}
	keyID, keyData, err := mach.SSSS.GetDefaultKeyData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ssssKey, err := keyData.VerifyRecoveryKey(keyID, recoveryKey)
	if err != nil {
		t.Fatal(err)
	}
	secret := base64.StdEncoding.EncodeToString(backupKey.Bytes())
	if err = mach.SSSS.SetEncryptedAccountData(ctx, event.AccountDataMegolmBackupKey, []byte(secret), ssssKey); err != nil {
		t.Fatalf("storing the backup key: %v", err)
	}
	return recoveryKey
}

// TestMatrixRecoveryKey verifies the core's device with alice's recovery
// key (T2.28, docs/ADR/0044): a wrong key is refused; the right one signs
// the device, restores the backup, and the room keys the core receives
// afterwards go to the backup.
func TestMatrixRecoveryKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	run := coretest.RandomHex(t)
	aliceLocal, alicePassword := "alice-r-"+run, "alice password "+run
	aliceID, err := env.RegisterUser(ctx, aliceLocal, alicePassword, false)
	if err != nil {
		t.Fatal(err)
	}
	bobLocal, bobPassword := "bob-r-"+run, "bob password "+run
	if _, err = env.RegisterUser(ctx, bobLocal, bobPassword, false); err != nil {
		t.Fatal(err)
	}
	recoveryKey := setUpRecovery(t, ctx, aliceLocal, alicePassword)
	bob := newPeer(t, ctx, bobLocal, bobPassword)

	dataDir := t.TempDir()
	c := coretest.Open(t, embedded.Config{DataDir: dataDir, LogLevel: "debug"})
	events := coretest.Pump(t, c)
	step := startLogin(t, c)
	coretest.Call(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{
		"server": env.HomeserverURL, "username": aliceLocal, "password": alicePassword,
	}}, &step)
	account := step.AccountID
	waitConnected(t, ctx, events, account)
	var accounts api.AccountsListResult
	coretest.Call(t, c, api.CommandAccountsList, nil, &accounts)
	if v := accounts.Accounts[0].Verification; v != api.DeviceVerificationUnverified {
		t.Errorf("verification before = %q, want unverified", v)
	}

	// A wrong key: refused, and nothing changes.
	wrong := coretest.TryCall(t, c, api.CommandAccountsVerify, api.AccountsVerifyParams{AccountID: account, RecoveryKey: "EsTj 3yST y93F SLpB jJsz eAXc 2XzA ygD3 w69H fGaN TKBj jXEd"}, nil)
	if wrong == nil || wrong.Code != api.ErrorCodeInvalidParams {
		t.Fatalf("a wrong recovery key: %+v", wrong)
	}

	var verified api.AccountsVerifyResult
	coretest.Call(t, c, api.CommandAccountsVerify, api.AccountsVerifyParams{AccountID: account, RecoveryKey: recoveryKey}, &verified)
	if !verified.BackupRestored {
		t.Errorf("the backup was not restored: %+v", verified)
	}
	coretest.Call(t, c, api.CommandAccountsList, nil, &accounts)
	if v := accounts.Accounts[0].Verification; v != api.DeviceVerificationVerified {
		t.Errorf("verification after = %q, want verified", v)
	}
	// bob's client sees the core's device signed by alice's self-signing
	// key: the server holds the signature.
	keys, err := bob.cli.QueryKeys(ctx, &mautrix.ReqQueryKeys{DeviceKeys: mautrix.DeviceKeysRequest{aliceID: {}}})
	if err != nil {
		t.Fatal(err)
	}
	signed := false
	for _, device := range keys.DeviceKeys[aliceID] {
		for signer := range device.Signatures[aliceID] {
			// A device's own signature is ed25519:DEVICEID; a cross-signing one
			// is ed25519 of the self-signing key.
			if !strings.HasSuffix(string(signer), string(device.DeviceID)) {
				signed = true
			}
		}
	}
	if !signed {
		t.Error("no device of alice is cross-signed on the server")
	}

	// A room key the core receives after goes to the backup.
	room := bob.createDirect(t, ctx, aliceID)
	events.Wait(t, ctx, "the conversation with bob", time.Minute, func(e coretest.Event) bool {
		return e.Type == api.EventConversationUpdated
	})
	text := "after the backup " + run
	bob.send(t, ctx, room, text)
	events.Wait(t, ctx, "bob's message", time.Minute, func(e coretest.Event) bool {
		var data api.MessageEvent
		return e.Type == api.EventMessageAdded && e.Decode(t, &data) && data.Message.Text == text
	})
	other, err := env.Login(ctx, aliceLocal, alicePassword)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	for {
		version, err := other.GetKeyBackupLatestVersion(ctx)
		if err == nil && version.Count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no room key in the backup: %+v (%v)", version, err)
		}
		time.Sleep(time.Second)
	}

	// The recovery key reached neither the log nor the database.
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	compact := strings.ReplaceAll(recoveryKey, " ", "")
	for _, name := range []string{"core.log", "core.db", "core.db-wal"} {
		data, err := os.ReadFile(filepath.Join(dataDir, name))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), recoveryKey) || strings.Contains(string(data), compact) {
			t.Errorf("%s holds the recovery key", name)
		}
	}
}
