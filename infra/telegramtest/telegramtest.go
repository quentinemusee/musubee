// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package telegramtest holds the Telegram end-to-end tests of the bridge
// (build tag "telegram") and the application credentials they use.
//
// Telegram's test environment turned out to be unusable for us (ADR 0005), so
// the tests run on production Telegram with two dedicated test bots and a
// private channel; no user account is involved. Credentials come from the
// environment, never from the repository:
//
//	MUSUBEE_TG_API_ID, MUSUBEE_TG_API_HASH  application credentials; default to
//	                                        the public test application of
//	                                        Telegram's open-source clients
//	MUSUBEE_TG_BRIDGE_BOT_TOKEN             bot the bridge logs in as
//	MUSUBEE_TG_PEER_BOT_TOKEN               bot playing the remote party
//	MUSUBEE_TG_CHAT_ID                      the private channel (optional)
package telegramtest

import (
	"fmt"
	"os"
	"strconv"
)

const (
	// APIIDEnv and APIHashEnv override the application credentials.
	APIIDEnv   = "MUSUBEE_TG_API_ID"
	APIHashEnv = "MUSUBEE_TG_API_HASH"

	// PublicTestAppID and PublicTestAppHash identify the sample application
	// shipped with Telegram's open-source clients (Telegram Desktop), which
	// Telegram allows for testing: https://core.telegram.org/api/obtaining_api_id
	PublicTestAppID   = 17349
	PublicTestAppHash = "344583e45741c457fe1862106095a5eb"
)

// Credentials identify the application (not a user) to Telegram.
type Credentials struct {
	APIID   int
	APIHash string
}

// CredentialsFromEnv returns the credentials from the environment, or the
// public test application credentials when none are set.
func CredentialsFromEnv() (Credentials, error) {
	idText, hash := os.Getenv(APIIDEnv), os.Getenv(APIHashEnv)
	if idText == "" && hash == "" {
		return Credentials{APIID: PublicTestAppID, APIHash: PublicTestAppHash}, nil
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
	return c.APIID == PublicTestAppID && c.APIHash == PublicTestAppHash
}
