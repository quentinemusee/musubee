// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"slices"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// TestFetchOlder reads the echo network's history page by page (API 1.13),
// before the messages of the conversation, until the network has no more.
func TestFetchOlder(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "hello"})
	waitEvent(t, c, "the echo", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Text == "hello" }))

	fetch := func(limit int64) api.MessagesFetchOlderResult {
		t.Helper()
		return call[api.MessagesFetchOlderResult](t, c, api.CommandMessagesFetchOlder, api.MessagesFetchOlderParams{ConversationID: conv.ConversationID, Limit: limit})
	}
	texts := func(messages []api.Message) []string {
		var out []string
		for _, m := range messages {
			out = append(out, m.Text)
		}
		return out
	}
	page := func(from int) []string {
		var out []string
		for n := from; n < from+echo.HistoryPageSize; n++ {
			out = append(out, echo.HistoryText(n))
		}
		return out
	}

	// The newest page of the history, oldest first, before "hello".
	first := fetch(0)
	if got, want := texts(first.Messages), page(2*echo.HistoryPageSize); !slices.Equal(got, want) || first.End || first.Before != "" {
		t.Fatalf("first page = %q, end %v, before %q; want %q", got, first.End, first.Before, want)
	}
	if !first.Messages[1].FromMe || first.Messages[0].FromMe {
		t.Errorf("senders: %+v, want the contact and the user in turn", first.Messages[:2])
	}
	// A smaller limit leaves the rest on the device, behind a cursor.
	second := fetch(5)
	if got := texts(second.Messages); !slices.Equal(got, page(echo.HistoryPageSize)[15:]) || second.Before == "" || second.End {
		t.Fatalf("second page = %q, before %q, end %v", got, second.Before, second.End)
	}
	rest := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID, Before: second.Before})
	if got := texts(rest.Messages); !slices.Equal(got, page(echo.HistoryPageSize)[:15]) {
		t.Errorf("the rest of the second page = %q", got)
	}
	last := fetch(0)
	if got := texts(last.Messages); !slices.Equal(got, page(0)) || !last.End {
		t.Fatalf("last page = %q, end %v; want the oldest page and the end", got, last.End)
	}
	if after := fetch(0); len(after.Messages) != 0 || !after.End {
		t.Errorf("after the end = %+v", after)
	}

	// The whole history, in order; none of it unread.
	all := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID, Limit: 200})
	want := append(append(append(page(0), page(echo.HistoryPageSize)...), page(2*echo.HistoryPageSize)...), "hello", "hello")
	if got := texts(all.Messages); !slices.Equal(got, want) {
		t.Errorf("history = %q, want %q", got, want)
	}
	if cv := conversationNamed(t, c, "Instant Echo"); cv.UnreadCount != 1 {
		t.Errorf("unread = %d, want 1: the echo only", cv.UnreadCount)
	}
}

// TestFetchOlderWithoutHistory: a conversation whose network gives none
// says so at once.
func TestFetchOlderWithoutHistory(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Delayed Echo")
	result := call[api.MessagesFetchOlderResult](t, c, api.CommandMessagesFetchOlder, api.MessagesFetchOlderParams{ConversationID: conv.ConversationID})
	if len(result.Messages) != 0 || !result.End {
		t.Errorf("result = %+v, want the end", result)
	}
	callFails(t, c, api.CommandMessagesFetchOlder, api.MessagesFetchOlderParams{ConversationID: conv.ConversationID, Limit: 201}, api.ErrorCodeInvalidParams)
}
