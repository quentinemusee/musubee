// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"go.mau.fi/util/random"
	"maunium.net/go/mautrix/id"
)

func newEventID(serverName string) id.EventID {
	return id.EventID("$" + random.String(32) + ":" + serverName)
}

func newRoomID(serverName string) id.RoomID {
	return id.RoomID("!" + random.String(18) + ":" + serverName)
}

func newMediaID() string {
	return random.String(32)
}

// hashID derives a stable, URL-safe identifier from its parts. The parts are
// joined with a NUL byte, which cannot appear in Matrix or network IDs, so
// that different part lists never produce the same input.
func hashID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
