// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"slices"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/quentinemusee/musubee/core/api"
)

// Older history from the networks (API 1.13, docs/ADR/0041): when the
// interface reaches the oldest message on the device, it asks the core,
// which asks the network through bridgev2's backward backfill, then reads
// back what the network gave.

func (c *Core) messagesFetchOlder(ctx context.Context, p api.MessagesFetchOlderParams) (api.MessagesFetchOlderResult, error) {
	result := api.MessagesFetchOlderResult{Messages: []api.Message{}}
	limit := int(p.Limit)
	if p.Limit == 0 {
		limit = defaultMessageLimit
	} else if p.Limit < 1 || p.Limit > maxMessageLimit {
		return result, newError(api.ErrorCodeInvalidParams, "limit must be between 1 and %d", maxMessageLimit)
	}
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return result, err
	}
	room, err := c.host.Matrix.Room(ctx, roomID)
	if err != nil {
		return result, err
	}
	br := c.host.Bridge(networkid.BridgeID(room.BridgeID))
	if br == nil {
		result.End = true
		return result, nil
	}
	portal, err := br.GetPortalByMXID(ctx, roomID)
	if err != nil || portal == nil {
		result.End = true
		return result, err
	}
	_, login, err := portalLogin(ctx, br, portal)
	if err != nil {
		return result, err
	}
	if login == nil || login.Client == nil || !login.Client.IsLoggedIn() {
		return result, newError(api.ErrorCodeNetwork, "the account of this conversation is not connected")
	}
	if _, ok := login.Client.(bridgev2.BackfillingNetworkAPI); !ok {
		// The network gives no history (the echo network gives some).
		result.End = true
		return result, nil
	}
	// The task bridgev2 keeps per conversation: its cursor, and whether the
	// network has more. Conversations made before backfill was on have none.
	tasks := br.DB.BackfillTask
	if err = tasks.EnsureExists(ctx, portal.PortalKey, login.ID); err != nil {
		return result, err
	}
	if task, err := tasks.GetNextForPortal(ctx, portal.PortalKey, true); err != nil {
		return result, err
	} else if task == nil || task.IsDone {
		result.End = true
		return result, nil
	}
	oldest, err := c.host.Matrix.OldestMessageOrder(ctx, roomID)
	if err != nil {
		return result, err
	}
	if err = backfill(portal.Log.WithContext(ctx), login, portal); err != nil {
		return result, newError(api.ErrorCodeNetwork, "the network did not give the history: %v", err)
	}
	if task, err := tasks.GetNextForPortal(ctx, portal.PortalKey, true); err != nil {
		return result, err
	} else {
		result.End = task == nil || task.IsDone
	}
	// What is older than the oldest message the device had: all of the
	// room's messages when it had none.
	events, err := c.host.Matrix.MessagesBefore(ctx, roomID, oldest, limit+1)
	if err != nil {
		return result, err
	}
	if len(events) > limit {
		events = events[:limit]
		result.Before = cursor(events[limit-1].Unsigned.BeeperHSOrder)
	}
	slices.Reverse(events)
	result.Messages, err = c.messages(ctx, roomID, events)
	return result, err
}

// errBackfillPending is returned when the network answers later (bridgev2
// keeps the task for a later dispatch).
var errBackfillPending = errors.New("the network will give the history later")

// backfill runs one backward batch of a conversation's history, in this
// goroutine, as bridgev2's manual backfill does.
func backfill(ctx context.Context, login *bridgev2.UserLogin, portal *bridgev2.Portal) error {
	done := make(chan error, 1)
	manual := &bridgev2.ManualBackfill{
		Source: login,
		Portal: portal,
		// Called at once when the batch completes; a pending batch keeps it
		// for later, when nobody waits any more: never block.
		DoneCallback: func(err error) {
			select {
			case done <- err:
			default:
			}
		},
	}
	manual.Do(ctx)
	select {
	case err := <-done:
		return err
	default:
		return errBackfillPending
	}
}
