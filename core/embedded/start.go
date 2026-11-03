// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"slices"
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
	if p.ContactID != "" {
		// A contact of the list (API 1.38): its ghost, which bridgev2's
		// helper resolves without asking the network to search.
		if identifier != "" {
			return api.ConversationsStartResult{}, newError(api.ErrorCodeInvalidParams, "give identifier or contact_id, not both")
		}
		parts, err := decodeID(contactPrefix, p.ContactID, 1, "contact")
		if err != nil {
			return api.ConversationsStartResult{}, err
		}
		identifier = parts[0]
	} else if identifier == "" || len(identifier) > maxIdentifier {
		return api.ConversationsStartResult{}, newError(api.ErrorCodeInvalidParams, "identifier must have between 1 and %d bytes", maxIdentifier)
	}
	login, err := c.loginOf(p.AccountID)
	if err != nil {
		return api.ConversationsStartResult{}, err
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

// loginOf returns the login of an account, connected or not.
func (c *Core) loginOf(accountID string) (*bridgev2.UserLogin, error) {
	network, loginID, err := parseAccountID(accountID)
	if err != nil {
		return nil, err
	}
	var login *bridgev2.UserLogin
	if br := c.host.Bridge(network); br != nil && c.hasNetwork(network) {
		login = br.GetCachedUserLoginByID(loginID)
	}
	if login == nil || login.Client == nil {
		return nil, newError(api.ErrorCodeNotFound, "no account %q", accountID)
	}
	return login, nil
}

// listsContacts says whether a login's network lists the user's contacts
// (API 1.38).
func listsContacts(login *bridgev2.UserLogin) bool {
	_, ok := login.Client.(bridgev2.ContactListingNetworkAPI)
	return ok
}

// accountsContacts lists an account's contacts on its network, by name,
// to start a conversation with one (API 1.38, docs/ADR/0069).
func (c *Core) accountsContacts(ctx context.Context, p api.AccountsContactsParams) (api.AccountsContactsResult, error) {
	result := api.AccountsContactsResult{Contacts: []api.Contact{}}
	login, err := c.loginOf(p.AccountID)
	if err != nil {
		return result, err
	}
	if !listsContacts(login) {
		return result, newError(api.ErrorCodeUnsupported, "this account's network does not list contacts")
	}
	list, err := provisionutil.GetContactList(ctx, login)
	if err != nil {
		return result, newError(api.ErrorCodeNetwork, "the network did not list the contacts")
	}
	for _, contact := range list.Contacts {
		if contact.MXID == "" {
			continue
		}
		result.Contacts = append(result.Contacts, api.Contact{ContactID: encodeID(contactPrefix, string(contact.MXID)), Name: contact.Name})
	}
	slices.SortStableFunc(result.Contacts, func(a, b api.Contact) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return result, nil
}
