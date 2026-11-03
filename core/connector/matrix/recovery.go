// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/backup"
	"maunium.net/go/mautrix/crypto/ssss"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Key backup and recovery (docs/ADR/0044): the user's recovery key, which
// their Matrix app gave them when they set up secure backup, verifies
// Musubee's device and opens the account's key backup; while a trusted
// backup is known, the room keys Musubee receives go to it.
//
// The recovery key is never stored nor logged: what it opens (the
// cross-signing keys and the backup key) is kept by the crypto store,
// pickled with a key derived from the core's master key (ADR 0019).

var (
	// ErrNoRecovery: the account has no secure backup set up (no default
	// secret storage key).
	ErrNoRecovery = errors.New("this Matrix account has no secure backup set up")
	// ErrWrongRecoveryKey: the key is malformed, or not this account's.
	ErrWrongRecoveryKey = errors.New("this is not the recovery key of this Matrix account")
)

// backupBatch is how many room keys go to the backup in one request.
const backupBatch = 100

// recovery serializes the uploads to the key backup, and remembers whether
// the device is verified.
type recovery struct {
	mu        sync.Mutex
	uploading bool
	// checked once the device's verification was read; verified then.
	checked, verified bool
}

// DeviceVerified tells whether the device is verified (signed by the
// account's cross-signing keys), as last read; known is false before.
func (c *client) DeviceVerified() (verified, known bool) {
	c.recovery.mu.Lock()
	defer c.recovery.mu.Unlock()
	return c.recovery.verified, c.recovery.checked
}

// checkVerified reads whether the device is verified, and remembers it.
func (c *client) checkVerified(ctx context.Context) {
	verified, err := c.Verified(ctx)
	if err != nil {
		c.login.Log.Warn().Err(err).Msg("Failed to read whether this device is verified")
		return
	}
	c.recovery.mu.Lock()
	c.recovery.checked, c.recovery.verified = true, verified
	c.recovery.mu.Unlock()
}

// Verified tells whether the device is signed by the account's
// cross-signing keys, as other clients see it.
func (c *client) Verified(ctx context.Context) (bool, error) {
	c.lock.Lock()
	initialized := c.initialized
	c.lock.Unlock()
	if !initialized {
		return false, errNotReady
	}
	mach := c.crypto.Machine()
	keys, err := mach.GetOwnCrossSigningPublicKeys(ctx)
	if err != nil || keys == nil || keys.SelfSigningKey == "" {
		// No cross-signing on the account: nothing can sign the device.
		return false, nil
	}
	return mach.CryptoStore.IsKeySignedBy(ctx, c.self, mach.OwnIdentity().SigningKey, c.self, keys.SelfSigningKey)
}

// Verify verifies the device with the account's recovery key, then
// restores the room keys of its latest backup. It reports whether a backup
// was restored (an account may have secret storage without a backup).
func (c *client) Verify(ctx context.Context, recoveryKey string) (bool, error) {
	if err := c.isReady(); err != nil {
		return false, err
	}
	mach := c.crypto.Machine()
	keyID, keyData, err := mach.SSSS.GetDefaultKeyData(ctx)
	if errors.Is(err, ssss.ErrNoDefaultKeyID) || errors.Is(err, mautrix.MNotFound) {
		return false, ErrNoRecovery
	} else if err != nil {
		return false, fmt.Errorf("reading the secret storage: %w", err)
	}
	key, err := keyData.VerifyRecoveryKey(keyID, recoveryKey)
	if errors.Is(err, ssss.ErrInvalidRecoveryKey) || errors.Is(err, ssss.ErrIncorrectSSSSKey) {
		return false, ErrWrongRecoveryKey
	} else if err != nil && !errors.Is(err, ssss.ErrUnverifiableKey) {
		return false, fmt.Errorf("checking the recovery key: %w", err)
	}
	// What VerifyWithRecoveryKey does, with the key opened once.
	if err = mach.FetchCrossSigningKeysFromSSSS(ctx, key); err != nil {
		return false, fmt.Errorf("reading the cross-signing keys: %w", err)
	}
	if err = mach.SignOwnDevice(ctx, mach.OwnIdentity()); err != nil {
		return false, fmt.Errorf("signing this device: %w", err)
	}
	if err = mach.SignOwnMasterKey(ctx); err != nil {
		return false, fmt.Errorf("signing the master key: %w", err)
	}
	c.recovery.mu.Lock()
	c.recovery.checked, c.recovery.verified = true, true
	c.recovery.mu.Unlock()
	return c.restoreBackup(ctx, mach, key)
}

// restoreBackup reads the backup key from secret storage, keeps it, and
// imports the room keys of the latest backup, after checking that the
// backup is the key's (DownloadAndStoreLatestKeyBackup does).
func (c *client) restoreBackup(ctx context.Context, mach *crypto.OlmMachine, key *ssss.Key) (bool, error) {
	secret, err := mach.SSSS.GetDecryptedAccountData(ctx, event.AccountDataMegolmBackupKey, key)
	if errors.Is(err, mautrix.MNotFound) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("reading the backup key: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(string(secret))
	if err != nil {
		if raw, err = base64.RawStdEncoding.DecodeString(string(secret)); err != nil {
			return false, fmt.Errorf("decoding the backup key: %w", err)
		}
	}
	backupKey, err := backup.MegolmBackupKeyFromBytes(raw)
	if err != nil {
		return false, fmt.Errorf("reading the backup key: %w", err)
	}
	if err = mach.CryptoStore.PutSecret(ctx, id.SecretMegolmBackupV1, base64.StdEncoding.EncodeToString(raw)); err != nil {
		return false, fmt.Errorf("keeping the backup key: %w", err)
	}
	version, err := mach.DownloadAndStoreLatestKeyBackup(ctx, backupKey)
	if err != nil {
		return false, fmt.Errorf("restoring the key backup: %w", err)
	}
	if version == "" {
		return false, nil
	}
	if err = mach.SetKeyBackupVersion(ctx, version); err != nil {
		return false, err
	}
	c.uploadKeysLater(ctx)
	return true, nil
}

// backupKey returns the kept backup key, or nil without one.
func (c *client) backupKey(ctx context.Context, mach *crypto.OlmMachine) (*backup.MegolmBackupKey, error) {
	secret, err := mach.CryptoStore.GetSecret(ctx, id.SecretMegolmBackupV1)
	if err != nil || secret == "" {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return nil, err
	}
	return backup.MegolmBackupKeyFromBytes(raw)
}

// uploadKeysLater uploads the room keys missing from the backup in the
// background, once at a time: it runs after each sync.
func (c *client) uploadKeysLater(ctx context.Context) {
	c.recovery.mu.Lock()
	if c.recovery.uploading {
		c.recovery.mu.Unlock()
		return
	}
	c.recovery.uploading = true
	c.recovery.mu.Unlock()
	go func() {
		defer func() {
			c.recovery.mu.Lock()
			c.recovery.uploading = false
			c.recovery.mu.Unlock()
		}()
		if err := c.uploadKeys(context.WithoutCancel(ctx)); err != nil {
			// Counts and errors only: never a key.
			c.login.Log.Warn().Err(err).Msg("Failed to upload room keys to the key backup")
		}
	}()
}

// uploadKeys sends the room keys that the backup's current version lacks,
// encrypted to the backup's public key, in batches. Nothing happens
// without a trusted backup (a version and its key, from Verify).
func (c *client) uploadKeys(ctx context.Context) error {
	if c.crypto == nil || c.isReady() != nil {
		return nil
	}
	mach := c.crypto.Machine()
	version := mach.KeyBackupVersion()
	if version == "" {
		return nil
	}
	backupKey, err := c.backupKey(ctx, mach)
	if err != nil || backupKey == nil {
		return err
	}
	store, ok := mach.CryptoStore.(*crypto.SQLCryptoStore)
	if !ok {
		return nil
	}
	sessions, err := store.GetGroupSessionsWithoutKeyBackupVersion(ctx, version).AsList()
	if err != nil {
		return err
	}
	for start := 0; start < len(sessions); start += backupBatch {
		batch := sessions[start:min(start+backupBatch, len(sessions))]
		req := &mautrix.ReqKeyBackup{Rooms: map[id.RoomID]mautrix.ReqRoomKeyBackup{}}
		for _, igs := range batch {
			data, err := sessionBackupData(backupKey, igs)
			if err != nil {
				return err
			}
			room := req.Rooms[igs.RoomID]
			if room.Sessions == nil {
				room.Sessions = map[id.SessionID]mautrix.ReqKeyBackupData{}
			}
			room.Sessions[igs.ID()] = *data
			req.Rooms[igs.RoomID] = room
		}
		if _, err = c.cli.PutKeysInBackup(ctx, version, req); err != nil {
			// A newer backup version (made in another client) refuses
			// uploads: stop until the user verifies again.
			if errors.Is(err, mautrix.MWrongRoomKeysVersion) {
				return mach.SetKeyBackupVersion(ctx, "")
			}
			return err
		}
		for _, igs := range batch {
			if err = store.SetGroupSessionKeyBackupVersion(ctx, igs.ID(), version); err != nil {
				return err
			}
		}
	}
	if len(sessions) > 0 {
		c.login.Log.Debug().Int("count", len(sessions)).Msg("Uploaded room keys to the key backup")
	}
	return nil
}

// sessionBackupData is a room key as the backup holds it: the session
// exported from its first known index, encrypted to the backup's key.
func sessionBackupData(backupKey *backup.MegolmBackupKey, igs *crypto.InboundGroupSession) (*mautrix.ReqKeyBackupData, error) {
	firstIndex := igs.Internal.FirstKnownIndex()
	exported, err := igs.Internal.Export(firstIndex)
	if err != nil {
		return nil, fmt.Errorf("exporting a room key: %w", err)
	}
	chain := igs.ForwardingChains
	if chain == nil {
		chain = []string{}
	}
	encrypted, err := backup.EncryptSessionData(backupKey, backup.MegolmSessionData{
		Algorithm:          id.AlgorithmMegolmV1,
		ForwardingKeyChain: chain,
		SenderClaimedKeys:  backup.SenderClaimedKeys{Ed25519: igs.SigningKey},
		SenderKey:          igs.SenderKey,
		SessionKey:         string(exported),
		SharedHistory:      igs.SharedHistory,
	})
	if err != nil {
		return nil, fmt.Errorf("encrypting a room key: %w", err)
	}
	raw, err := json.Marshal(encrypted)
	if err != nil {
		return nil, err
	}
	return &mautrix.ReqKeyBackupData{
		FirstMessageIndex: int(firstIndex),
		ForwardedCount:    len(chain),
		IsVerified:        false,
		SessionData:       raw,
	}, nil
}
