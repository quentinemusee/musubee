// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
)

// The API's IDs are opaque to user interfaces (docs/ADR/0012). They encode
// the core's internal identifiers, so that no lookup table is needed, but
// in base64 behind a prefix: a user interface cannot mistake them for
// Matrix IDs, nor start depending on their shape.
const (
	accountPrefix      = "a."
	conversationPrefix = "c."
	messagePrefix      = "m."
	cursorPrefix       = "k."
	processPrefix      = "p."
)

func encodeID(prefix string, parts ...string) string {
	return prefix + base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x00")))
}

// decodeID returns the n parts of an ID, or a not_found error naming what.
func decodeID(prefix, value string, n int, what string) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	parts := strings.Split(string(raw), "\x00")
	if err != nil || !strings.HasPrefix(value, prefix) || len(parts) != n {
		return nil, newError(api.ErrorCodeNotFound, "no %s %q", what, value)
	}
	return parts, nil
}

func accountID(network networkid.BridgeID, login networkid.UserLoginID) string {
	return encodeID(accountPrefix, string(network), string(login))
}

func parseAccountID(value string) (networkid.BridgeID, networkid.UserLoginID, error) {
	parts, err := decodeID(accountPrefix, value, 2, "account")
	if err != nil {
		return "", "", err
	}
	return networkid.BridgeID(parts[0]), networkid.UserLoginID(parts[1]), nil
}

func conversationID(room id.RoomID) string {
	return encodeID(conversationPrefix, string(room))
}

func parseConversationID(value string) (id.RoomID, error) {
	parts, err := decodeID(conversationPrefix, value, 1, "conversation")
	if err != nil {
		return "", err
	}
	return id.RoomID(parts[0]), nil
}

func messageID(room id.RoomID, evt id.EventID) string {
	return encodeID(messagePrefix, string(room), string(evt))
}

func cursor(streamOrder int64) string {
	return encodeID(cursorPrefix, strconv.FormatInt(streamOrder, 10))
}

// parseCursor rejects a malformed cursor as invalid params: unlike the IDs,
// a cursor names no object.
func parseCursor(value string) (int64, error) {
	parts, err := decodeID(cursorPrefix, value, 1, "cursor")
	var order int64
	if err == nil {
		order, err = strconv.ParseInt(parts[0], 10, 64)
	}
	if err != nil || order <= 0 {
		return 0, newError(api.ErrorCodeInvalidParams, "invalid cursor %q", value)
	}
	return order, nil
}

func newProcessID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return processPrefix + hex.EncodeToString(b)
}
