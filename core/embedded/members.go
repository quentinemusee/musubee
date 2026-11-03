// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"slices"
	"strings"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/api"
)

// conversationsMembers lists who is in a conversation (API 1.27): the
// room's joined members, without the bridge's bot, the user first, then
// the others by name.
func (c *Core) conversationsMembers(ctx context.Context, p api.ConversationsMembersParams) (api.ConversationsMembersResult, error) {
	result := api.ConversationsMembersResult{Members: []api.ConversationMember{}}
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return result, err
	}
	room, err := c.host.Matrix.Room(ctx, roomID)
	if err != nil {
		return result, err
	}
	var bot string
	if br := c.host.Bridge(networkid.BridgeID(room.BridgeID)); br != nil && br.Bot != nil {
		bot = string(br.Bot.GetMXID())
	}
	members, err := c.host.Matrix.Members(ctx, roomID)
	if err != nil {
		return result, err
	}
	me := c.host.Matrix.UserID()
	var others []api.ConversationMember
	for userID, member := range members {
		if member.Membership != event.MembershipJoin || string(userID) == bot {
			continue
		}
		entry := api.ConversationMember{Name: member.Displayname, Me: userID == me}
		// Their current picture (API 1.35), from their global profile.
		profile, err := c.host.Matrix.Profile(ctx, userID)
		if err != nil {
			return result, err
		}
		if profile.AvatarURL != "" {
			entry.AvatarID = mediaID(profile.AvatarURL)
		}
		if entry.Me {
			result.Members = append(result.Members, entry)
			continue
		}
		others = append(others, entry)
	}
	slices.SortFunc(others, func(a, b api.ConversationMember) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	result.Members = append(result.Members, others...)
	return result, nil
}
