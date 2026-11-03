// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"time"

	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/api"
)

// conversationsArchive archives or unarchives a conversation on this
// device (API 1.31, docs/ADR/0060).
func (c *Core) conversationsArchive(ctx context.Context, p api.ConversationsArchiveParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	var at int64
	if p.Archived {
		at = time.Now().UnixMilli()
	}
	_, err = c.host.Matrix.Archive(ctx, roomID, at)
	return api.Empty{}, err
}

// unarchiveOnMessage brings an archived conversation back when someone
// writes in it after it was archived, unless it is muted: a muted
// conversation stays archived, as on Telegram (docs/ADR/0060). History
// older than the archiving changes nothing.
func (c *Core) unarchiveOnMessage(ctx context.Context, evt *event.Event) error {
	if evt.Sender == c.host.Matrix.UserID() {
		return nil
	}
	read, err := c.host.Matrix.ReadState(ctx, evt.RoomID)
	if err != nil || read.ArchivedAt == 0 || evt.Timestamp < read.ArchivedAt {
		return err
	}
	if mutedUntil(read.MutedUntil, time.Now()) != 0 {
		return nil
	}
	_, err = c.host.Matrix.Archive(ctx, evt.RoomID, 0)
	return err
}
