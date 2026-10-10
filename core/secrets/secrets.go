// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package secrets protects the core's secrets at rest (docs/ADR/0019): the
// sessions of the accounts (a Telegram authorization key, a Matrix access
// token) and the key that encrypts a Matrix account's encryption keys.
//
// One master key, 32 random bytes, is kept by the operating system's secure
// storage: Windows' DPAPI, or the embedding application's (the Android
// Keystore). Keys for each use are derived from it with HKDF-SHA256, and
// secrets are sealed with AES-256-GCM.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the size of the master key and of the derived keys.
const KeySize = 32

// Key is the master key.
type Key struct {
	b [KeySize]byte
}

// NewKey returns a random key.
func NewKey() Key {
	var k Key
	// crypto/rand.Read never fails (Go 1.24 and later).
	_, _ = rand.Read(k.b[:])
	return k
}

// ParseKey decodes a key given in standard base64.
func ParseKey(encoded string) (Key, error) {
	var k Key
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) != KeySize {
		return k, fmt.Errorf("the key must be %d bytes in base64", KeySize)
	}
	copy(k.b[:], raw)
	return k, nil
}

// Derive returns the key of one use of the master key. Different purposes
// give independent keys.
func (k Key) Derive(purpose string) []byte {
	derived, err := hkdf.Key(sha256.New, k.b[:], nil, "musubee/v1/"+purpose, KeySize)
	if err != nil {
		// Only possible for an output longer than 255 hash blocks.
		panic(err)
	}
	return derived
}

// SealedPrefix starts every sealed value, so that sealed and plain values
// can be told apart (plain values come from databases written before
// secrets were sealed).
const SealedPrefix = "musubee-sealed:v1:"

// Sealer seals and opens values for one purpose.
type Sealer struct {
	aead    cipher.AEAD
	purpose []byte
}

// NewSealer returns the sealer of a purpose. The purpose is also bound to
// every sealed value, so that a value sealed for one purpose does not open
// for another.
func NewSealer(k Key, purpose string) *Sealer {
	block, err := aes.NewCipher(k.Derive("sealer/" + purpose))
	if err != nil {
		panic(err) // The key always has a valid size.
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &Sealer{aead: aead, purpose: []byte(purpose)}
}

// Seal encrypts a value with a random nonce.
func (s *Sealer) Seal(plain []byte) string {
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plain)+s.aead.Overhead())
	_, _ = rand.Read(nonce)
	sealed := s.aead.Seal(nonce, nonce, plain, s.purpose)
	return SealedPrefix + base64.StdEncoding.EncodeToString(sealed)
}

// ErrNotSealed is returned by Open for a value that is not sealed.
var ErrNotSealed = errors.New("the value is not sealed")

// Open decrypts a sealed value. The error never quotes the value.
func (s *Sealer) Open(sealed string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(sealed, SealedPrefix)
	if !ok {
		return nil, ErrNotSealed
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < s.aead.NonceSize()+s.aead.Overhead() {
		return nil, errors.New("a sealed value is malformed")
	}
	nonce, ciphertext := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, ciphertext, s.purpose)
	if err != nil {
		return nil, errors.New("a sealed value does not open with this key")
	}
	return plain, nil
}

// IsSealed reports whether a value is sealed.
func IsSealed(value string) bool {
	return strings.HasPrefix(value, SealedPrefix)
}

// IsSealed reports whether a value is sealed (see the function IsSealed).
func (s *Sealer) IsSealed(value string) bool {
	return IsSealed(value)
}
