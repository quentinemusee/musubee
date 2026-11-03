// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
)

// TestMute: a conversation muted until unmuted, then for an hour, then
// unmuted (API 1.23, docs/ADR/0050).
func TestMute(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Delayed Echo")
	if conv.MutedUntilMS != 0 {
		t.Errorf("a new conversation is muted until %d", conv.MutedUntilMS)
	}
	mute := func(untilMS int64) {
		t.Helper()
		call[api.Empty](t, c, api.CommandConversationsMute, api.ConversationsMuteParams{ConversationID: conv.ConversationID, UntilMS: untilMS})
	}
	mute(-1)
	waitConversationUpdate(t, c, "muted", conv.ConversationID, func(cv api.Conversation) bool { return cv.MutedUntilMS == -1 })
	inAnHour := time.Now().Add(time.Hour).UnixMilli()
	mute(inAnHour)
	waitConversationUpdate(t, c, "muted for an hour", conv.ConversationID, func(cv api.Conversation) bool { return cv.MutedUntilMS == inAnHour })
	mute(0)
	waitConversationUpdate(t, c, "unmuted", conv.ConversationID, func(cv api.Conversation) bool { return cv.MutedUntilMS == 0 })

	callFails(t, c, api.CommandConversationsMute, api.ConversationsMuteParams{ConversationID: conv.ConversationID, UntilMS: -2}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandConversationsMute, api.ConversationsMuteParams{ConversationID: "c.nope", UntilMS: -1}, api.ErrorCodeNotFound)
}

// TestMutedUntil: a mute that ended reads as none.
func TestMutedUntil(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	for _, tc := range []struct{ until, want int64 }{
		{0, 0},
		{-1, -1},
		{now.UnixMilli() - 1, 0},
		{now.UnixMilli(), 0},
		{now.UnixMilli() + 1, now.UnixMilli() + 1},
	} {
		if got := mutedUntil(tc.until, now); got != tc.want {
			t.Errorf("mutedUntil(%d) = %d, want %d", tc.until, got, tc.want)
		}
	}
}
