// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Protection says how the master key is kept.
type Protection string

const (
	// ProtectionHost: the embedding application gives the key, from its own
	// secure storage (the Android Keystore).
	ProtectionHost Protection = "host"
	// ProtectionDPAPI: the key file is encrypted by Windows for the current
	// user (CryptProtectData).
	ProtectionDPAPI Protection = "dpapi"
	// ProtectionNone: the key file is plain, protected only by its file
	// permissions. Used where no secure storage is wired yet (macOS, Linux
	// and iOS without a key from the application; docs/ADR/0019).
	ProtectionNone Protection = "none"
	// ProtectionSafeStorage: the key file is encrypted by the desktop app
	// with Electron's safeStorage (the macOS Keychain, or libsecret or
	// KWallet on Linux; docs/ADR/0027), and the app gives the key to the
	// core. The core cannot open such a file itself.
	ProtectionSafeStorage Protection = "electron-safestorage"
)

// ErrKeyHeldByApp is returned by LoadOrCreate for a key file that only the
// desktop app can open: the core must not replace it with a new key, which
// would lose every sealed secret.
var ErrKeyHeldByApp = errors.New("the key file is kept by the desktop app's secure storage: start the core from the app")

// KeyFileName is the file of the master key in the data directory, when the
// embedding application does not give one.
const KeyFileName = "core.key"

type keyFile struct {
	Version    int        `json:"version"`
	Protection Protection `json:"protection"`
	// Key is the key, encrypted by the protection, in base64.
	Key string `json:"key"`
}

// LoadOrCreate returns the master key kept in dir, creating it on first use
// with the platform's protection.
func LoadOrCreate(dir string) (Key, Protection, error) {
	path := filepath.Join(dir, KeyFileName)
	data, err := os.ReadFile(path) //nolint:gosec // The core's own data directory.
	if errors.Is(err, fs.ErrNotExist) {
		return create(path)
	} else if err != nil {
		return Key{}, "", fmt.Errorf("reading the key file: %w", err)
	}
	var f keyFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != 1 {
		return Key{}, "", errors.New("the key file is not valid")
	}
	if f.Protection == ProtectionSafeStorage {
		return Key{}, "", ErrKeyHeldByApp
	}
	blob, err := base64.StdEncoding.DecodeString(f.Key)
	if err != nil {
		return Key{}, "", errors.New("the key file is not valid")
	}
	raw, err := unprotect(f.Protection, blob)
	if err != nil {
		return Key{}, "", fmt.Errorf("unlocking the key: %w", err)
	}
	if len(raw) != KeySize {
		return Key{}, "", errors.New("the key file is not valid")
	}
	var k Key
	copy(k.b[:], raw)
	return k, f.Protection, nil
}

func create(path string) (Key, Protection, error) {
	k := NewKey()
	blob, protection, err := protect(k.b[:])
	if err != nil {
		return Key{}, "", fmt.Errorf("protecting the key: %w", err)
	}
	data, err := json.Marshal(keyFile{Version: 1, Protection: protection, Key: base64.StdEncoding.EncodeToString(blob)})
	if err != nil {
		return Key{}, "", err
	}
	// Written to a temporary file then renamed, so that a crash never leaves
	// a partial key file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return Key{}, "", fmt.Errorf("writing the key file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Key{}, "", fmt.Errorf("writing the key file: %w", err)
	}
	return k, protection, nil
}
