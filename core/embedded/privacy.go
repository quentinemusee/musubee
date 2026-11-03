// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"

	"maunium.net/go/mautrix/bridgev2"

	"github.com/quentinemusee/musubee/core/api"
)

// Who can read a conversation besides its members (API 1.12,
// docs/ADR/0039). The interface says it as the core says it: a level it
// cannot know is unknown, never assumed safe (CLAUDE.md §2: "the UI must
// say so honestly").

// roomEncryption is implemented by network clients that know whether one
// conversation is end-to-end encrypted: the Matrix connector, where each
// room turns encryption on or not.
type roomEncryption interface {
	RoomEncrypted(ctx context.Context, portal *bridgev2.Portal) (bool, error)
}

// matrixReaders are who reads an unencrypted Matrix room: every member's
// homeserver keeps its events.
const matrixReaders = "the Matrix servers of the room's members"

// privacy says who can read a conversation, from what each network is:
//   - echo: a test network inside the core; nothing leaves the device;
//   - Signal: always end-to-end encrypted, and the core is a linked device;
//   - Telegram: mautrix-telegram has no secret chats (its ROADMAP.md lists
//     them as not done): every conversation is a cloud chat, which
//     Telegram's servers can read;
//   - Matrix: per room, end-to-end encrypted when encryption is on;
//   - any other network: unknown.
func privacy(ctx context.Context, br *bridgev2.Bridge, portal *bridgev2.Portal, login *bridgev2.UserLogin) *api.ConversationPrivacy {
	switch br.ID {
	case echoBridge:
		return &api.ConversationPrivacy{Level: api.PrivacyLevelDeviceOnly}
	case signalBridge:
		return &api.ConversationPrivacy{Level: api.PrivacyLevelEndToEnd}
	case telegramBridge:
		return &api.ConversationPrivacy{Level: api.PrivacyLevelNetworkCanRead, Readers: "Telegram"}
	case matrixBridge:
		if login == nil {
			break
		}
		reporter, ok := login.Client.(roomEncryption)
		if !ok {
			break
		}
		encrypted, err := reporter.RoomEncrypted(ctx, portal)
		if err != nil {
			break
		}
		if encrypted {
			return &api.ConversationPrivacy{Level: api.PrivacyLevelEndToEnd}
		}
		return &api.ConversationPrivacy{Level: api.PrivacyLevelNetworkCanRead, Readers: matrixReaders}
	}
	return &api.ConversationPrivacy{Level: api.PrivacyLevelUnknown}
}
