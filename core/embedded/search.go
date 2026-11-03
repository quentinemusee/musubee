// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"strings"
	"unicode/utf8"

	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
)

// Searching the messages on the device (API 1.17, docs/ADR/0046). The
// local server finds candidates with SQLite's LIKE on the stored text and
// on the edits' text; the core keeps those whose latest text, as the
// interface shows it, contains the query. The query is never logged.

const maxSearchQuery = 200

// matches tells whether a message's latest text or caption contains the
// query, ignoring the case of ASCII letters as SQLite's LIKE does.
func matches(msg *api.Message, query string) bool {
	return strings.Contains(asciiLower(msg.Text), asciiLower(query))
}

func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

func (c *Core) messagesSearch(ctx context.Context, p api.MessagesSearchParams) (api.MessagesSearchResult, error) {
	result := api.MessagesSearchResult{Messages: []api.Message{}}
	query := strings.TrimSpace(p.Query)
	if query == "" || utf8.RuneCountInString(query) > maxSearchQuery {
		return result, newError(api.ErrorCodeInvalidParams, "query must have between 1 and %d characters", maxSearchQuery)
	}
	limit := int(p.Limit)
	if p.Limit == 0 {
		limit = defaultMessageLimit
	} else if p.Limit < 1 || p.Limit > maxMessageLimit {
		return result, newError(api.ErrorCodeInvalidParams, "limit must be between 1 and %d", maxMessageLimit)
	}
	var roomID id.RoomID
	if p.ConversationID != "" {
		var err error
		if roomID, err = c.room(ctx, p.ConversationID); err != nil {
			return result, err
		}
	}
	var before int64
	if p.Before != "" {
		var err error
		if before, err = parseCursor(p.Before); err != nil {
			return result, err
		}
	}
	events, err := c.host.Matrix.SearchMessages(ctx, roomID, query, before, limit+1)
	if err != nil {
		return result, err
	}
	if len(events) > limit {
		events = events[:limit]
		result.Before = cursor(events[limit-1].Unsigned.BeeperHSOrder)
	}
	for _, evt := range events {
		msg, err := c.messageOf(ctx, evt)
		if err != nil {
			return result, err
		}
		// Found by an older text: the message no longer says it.
		if !msg.Deleted && matches(&msg, query) {
			result.Messages = append(result.Messages, msg)
		}
	}
	return result, nil
}
