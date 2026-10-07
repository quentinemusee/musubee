// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package telegramtest connects to Telegram's official test environment (the
// "test DCs"), which is completely separate from production Telegram.
//
// Accounts there must be created by hand from an official Telegram app (see
// infra/README.md); the 99966XYYYY numbers no longer sign in automatically
// (verified 2026-10-07, PHONE_CODE_INVALID on all three test DCs).
//
// Credentials come from the environment, never from the repository:
//
//	MUSUBEE_TG_API_ID, MUSUBEE_TG_API_HASH  application credentials; default to
//	                                        the public test application of
//	                                        Telegram's open-source clients
//	MUSUBEE_TG_SESSION                      base64 session of the test account
package telegramtest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
)

const (
	// APIIDEnv and APIHashEnv override the application credentials.
	APIIDEnv   = "MUSUBEE_TG_API_ID"
	APIHashEnv = "MUSUBEE_TG_API_HASH"
	// SessionEnv holds the base64-encoded session of the test account.
	SessionEnv = "MUSUBEE_TG_SESSION"

	// DefaultDC is the test DC the client first connects to; Telegram
	// redirects the client if the account lives on another one.
	DefaultDC = 2
)

// ErrNoSession is returned when MUSUBEE_TG_SESSION is not set.
var ErrNoSession = errors.New(SessionEnv + " is not set")

// Credentials identify the application (not the user) to Telegram.
type Credentials struct {
	APIID   int
	APIHash string
}

// CredentialsFromEnv returns the credentials from the environment, or the
// public test application credentials when none are set.
func CredentialsFromEnv() (Credentials, error) {
	idText, hash := os.Getenv(APIIDEnv), os.Getenv(APIHashEnv)
	if idText == "" && hash == "" {
		return Credentials{APIID: telegram.TestAppID, APIHash: telegram.TestAppHash}, nil
	}
	if idText == "" || hash == "" {
		return Credentials{}, fmt.Errorf("set both %s and %s, or neither", APIIDEnv, APIHashEnv)
	}
	id, err := strconv.Atoi(idText)
	if err != nil || id <= 0 {
		return Credentials{}, fmt.Errorf("%s must be a positive integer", APIIDEnv)
	}
	return Credentials{APIID: id, APIHash: hash}, nil
}

// IsPublicTestApp reports whether these are the public test credentials.
func (c Credentials) IsPublicTestApp() bool {
	return c.APIID == telegram.TestAppID && c.APIHash == telegram.TestAppHash
}

// NewClient returns a client for the test DCs that keeps its session in
// storage.
func NewClient(creds Credentials, storage telegram.SessionStorage) *telegram.Client {
	return telegram.NewClient(creds.APIID, creds.APIHash, telegram.Options{
		DC:             DefaultDC,
		DCList:         dcs.Test(),
		SessionStorage: storage,
		Device: telegram.DeviceConfig{
			DeviceModel:   "musubee-test",
			SystemVersion: "test",
			AppVersion:    "test",
		},
	})
}

// EncodeSession turns a stored session into a single-line string fit for an
// environment variable or a CI secret.
func EncodeSession(ctx context.Context, storage *session.StorageMemory) (string, error) {
	data, err := storage.LoadSession(ctx)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("empty session")
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// DecodeSession is the inverse of EncodeSession.
func DecodeSession(ctx context.Context, encoded string) (*session.StorageMemory, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decoding the session: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("empty session")
	}
	storage := &session.StorageMemory{}
	if err := storage.StoreSession(ctx, data); err != nil {
		return nil, err
	}
	return storage, nil
}

// SessionFromEnv decodes MUSUBEE_TG_SESSION, or returns ErrNoSession.
func SessionFromEnv(ctx context.Context) (*session.StorageMemory, error) {
	encoded := os.Getenv(SessionEnv)
	if encoded == "" {
		return nil, ErrNoSession
	}
	return DecodeSession(ctx, encoded)
}
