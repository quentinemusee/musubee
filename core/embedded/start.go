// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/provisionutil"

	"github.com/quentinemusee/musubee/core/api"
)

// maxIdentifier bounds what the user types to find someone.
const maxIdentifier = 256

// startsConversations says whether a login's network finds people from
// what the user types (API 1.37).
func startsConversations(login *bridgev2.UserLogin) bool {
	_, ok := login.Client.(bridgev2.IdentifierResolvingNetworkAPI)
	return ok
}

// conversationsStart finds someone on an account's network from what the
// user typed (a username, a phone number, an ID) and opens the direct
// conversation with them, creating it when there is none (API 1.37,
// docs/ADR/0069). It goes through bridgev2's provisioning helper, as the
// bridge bot's start-chat command does.
func (c *Core) conversationsStart(ctx context.Context, p api.ConversationsStartParams) (api.ConversationsStartResult, error) {
	identifier := strings.TrimSpace(p.Identifier)
	if identifier == "" || len(identifier) > maxIdentifier {
		return api.ConversationsStartResult{}, newError(api.ErrorCodeInvalidParams, "identifier must have between 1 and %d bytes", maxIdentifier)
	}
	network, loginID, err := parseAccountID(p.AccountID)
	if err != nil {
		return api.ConversationsStartResult{}, err
	}
	var login *bridgev2.UserLogin
	if br := c.host.Bridge(network); br != nil && c.hasNetwork(network) {
		login = br.GetCachedUserLoginByID(loginID)
	}
	if login == nil || login.Client == nil {
		return api.ConversationsStartResult{}, newError(api.ErrorCodeNotFound, "no account %q", p.AccountID)
	}
	if !startsConversations(login) {
		return api.ConversationsStartResult{}, newError(api.ErrorCodeUnsupported, "this account's network cannot find people")
	}
	resp, err := provisionutil.ResolveIdentifier(ctx, login, identifier, true)
	if err != nil || resp == nil || resp.DMRoomID == "" {
		// The network's reason may hold what the user typed: it is not logged.
		return api.ConversationsStartResult{}, newError(api.ErrorCodeNotFound, "nobody found for what was typed")
	}
	conv, err := c.conversationOf(ctx, resp.DMRoomID, nil)
	if err != nil {
		return api.ConversationsStartResult{}, err
	}
	if conv == nil {
		return api.ConversationsStartResult{}, newError(api.ErrorCodeNotFound, "the conversation is not ready yet")
	}
	return api.ConversationsStartResult{Conversation: *conv}, nil
}
