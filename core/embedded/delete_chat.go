// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"time"

	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// deleteChatTimeout bounds how long conversations.delete waits for the
// network.
const deleteChatTimeout = 30 * time.Second

// conversationsDelete deletes a conversation for the user (API 1.25,
// docs/ADR/0053). The bridge deletes it asynchronously, and says nothing
// when its network refuses: the command waits for the room to go, and
// fails after deleteChatTimeout.
func (c *Core) conversationsDelete(ctx context.Context, p api.ConversationsDeleteParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	if err = c.allowed(ctx, roomID, "deleting the conversation", func(a *api.ConversationActions) bool { return a.DeleteChat }); err != nil {
		return api.Empty{}, err
	}
	// Subscribed before asking, so that the deletion cannot be missed.
	updates, unsubscribe := c.host.Matrix.Subscribe(256)
	defer unsubscribe()
	if err = c.host.Matrix.DeleteChat(ctx, roomID); err != nil {
		return api.Empty{}, err
	}
	timer := time.NewTimer(deleteChatTimeout)
	defer timer.Stop()
	for {
		select {
		case update, ok := <-updates:
			if !ok {
				// Dropped for falling behind: the storage says.
				return api.Empty{}, c.roomGone(ctx, roomID)
			}
			if update.RoomDeleted == roomID {
				return api.Empty{}, nil
			}
		case <-timer.C:
			return api.Empty{}, c.roomGone(ctx, roomID)
		case <-ctx.Done():
			return api.Empty{}, ctx.Err()
		}
	}
}

// roomGone succeeds when a room no longer exists, and explains otherwise.
func (c *Core) roomGone(ctx context.Context, roomID id.RoomID) error {
	_, err := c.host.Matrix.Room(ctx, roomID)
	switch {
	case errors.Is(err, localmatrix.ErrNotFound):
		return nil
	case err != nil:
		return err
	default:
		return newError(api.ErrorCodeNetwork, "the network did not delete the conversation")
	}
}
