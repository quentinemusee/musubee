// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package echo

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

// The echo network's history (docs/ADR/0041): ContactInstant has
// HistoryPages pages of HistoryPageSize older messages, which the core
// fetches when the user scrolls past the oldest message on the device.
// The other conversations have none. A new conversation gets no history
// at once (forward backfill): the tests of the conversations' first
// messages stay as they were.
const (
	HistoryPages    = 3
	HistoryPageSize = 20
)

var _ bridgev2.BackfillingNetworkAPI = (*client)(nil)

// HistoryText is the text of the nth message of ContactInstant's history,
// counted from the oldest (0).
func HistoryText(n int) string {
	return fmt.Sprintf("Old message %d", n)
}

// FetchMessages gives one page of older history, newest page first, each
// page oldest first, all before the oldest message the bridge has.
func (c *client) FetchMessages(_ context.Context, params bridgev2.FetchMessagesParams) (*bridgev2.FetchMessagesResponse, error) {
	if params.Forward || networkid.UserID(params.Portal.ID) != ContactInstant {
		return &bridgev2.FetchMessagesResponse{Forward: params.Forward}, nil
	}
	page := 0
	if params.Cursor != "" {
		n, err := strconv.Atoi(string(params.Cursor))
		if err != nil {
			return nil, fmt.Errorf("bad history cursor %q", params.Cursor)
		}
		page = n
	}
	if page >= HistoryPages {
		return &bridgev2.FetchMessagesResponse{}, nil
	}
	before := time.Now()
	if params.AnchorMessage != nil {
		before = params.AnchorMessage.Timestamp
	}
	// Page 0 holds the newest of the history: messages HistoryPages*size-1
	// down to (HistoryPages-1)*size.
	first := (HistoryPages - 1 - page) * HistoryPageSize
	messages := make([]*bridgev2.BackfillMessage, 0, HistoryPageSize)
	for i := range HistoryPageSize {
		n := first + i
		sender := bridgev2.EventSender{Sender: ContactInstant}
		if n%2 == 1 {
			sender = bridgev2.EventSender{Sender: c.selfID(), IsFromMe: true}
		}
		messages = append(messages, &bridgev2.BackfillMessage{
			ConvertedMessage: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
				Type:    event.EventMessage,
				Content: &event.MessageEventContent{MsgType: event.MsgText, Body: HistoryText(n)},
			}}},
			Sender:    sender,
			ID:        networkid.MessageID("history-" + strconv.Itoa(n)),
			Timestamp: before.Add(-time.Duration(HistoryPageSize-i) * time.Minute),
		})
	}
	return &bridgev2.FetchMessagesResponse{
		Messages: messages,
		Cursor:   networkid.PaginationCursor(strconv.Itoa(page + 1)),
		HasMore:  page+1 < HistoryPages,
	}, nil
}
