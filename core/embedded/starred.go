// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// maxStarred bounds what messages.starred returns.
const maxStarred = 500

// messagesStar stars or unstars a message, on this device only (API 1.36,
// docs/ADR/0065), and returns it as it now is.
func (c *Core) messagesStar(ctx context.Context, p api.MessagesStarParams) (api.MessagesSendResult, error) {
	roomID, eventID, err := parseMessageID(p.MessageID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	_, evt, err := c.target(ctx, conversationID(roomID), p.MessageID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	if _, err = c.host.Matrix.Star(ctx, roomID, eventID, p.Starred); err != nil {
		return api.MessagesSendResult{}, err
	}
	msg, err := c.messageOf(ctx, evt)
	return api.MessagesSendResult{Message: msg}, err
}

// messagesStarred lists the starred messages of every conversation, the
// most recently starred first. A starred message whose conversation or
// event is gone is left out.
func (c *Core) messagesStarred(ctx context.Context, _ api.Empty) (api.MessagesStarredResult, error) {
	result := api.MessagesStarredResult{Messages: []api.Message{}}
	all, err := c.host.Matrix.AllStarred(ctx)
	if err != nil {
		return result, err
	}
	for _, starred := range all {
		if len(result.Messages) == maxStarred {
			break
		}
		evt, err := c.host.Matrix.Event(ctx, starred.RoomID, starred.EventID)
		if errors.Is(err, localmatrix.ErrNotFound) {
			continue
		} else if err != nil {
			return result, err
		}
		msg, err := c.messageOf(ctx, evt)
		if err != nil {
			return result, err
		}
		result.Messages = append(result.Messages, msg)
	}
	return result, nil
}
