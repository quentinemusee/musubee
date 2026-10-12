// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"slices"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// waitAccountState waits for an account.updated event of the account in
// the given state.
func waitAccountState(t *testing.T, c *Core, account string, state api.AccountState) api.Account {
	t.Helper()
	e := waitEvent(t, c, "account "+string(state), func(e rawEvent) bool {
		a := decode[api.AccountEvent](t, e.Data).Account
		return e.Type == api.EventAccountUpdated && a.AccountID == account && a.State == state
	})
	return decode[api.AccountEvent](t, e.Data).Account
}

// An account whose session the network ended is logged in to again with
// account_id: it keeps its ID and its conversations, and works again.
func TestLoginAgainKeepsTheConversations(t *testing.T) {
	dir := t.TempDir()
	c := openDir(t, dir)
	account := loginAs(t, c, "expired", conversationsPerLogin)
	conv := conversationNamed(t, c, "Instant Echo")
	// As in TestOfflineLoginKeepsItsConversations: an echo in every
	// conversation that echoes tells that bridgev2 is done creating them.
	group := conversationNamed(t, c, "Echo Group")
	for _, id := range []string{group.ConversationID, conv.ConversationID} {
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: id, Text: "before"})
		waitEvent(t, c, "echo", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.ConversationID == id }))
	}
	before := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	c = openDir(t, dir)
	if a := waitAccountState(t, c, account, api.AccountStateBadCredentials); a.Error == "" {
		t.Errorf("account with an ended session = %+v, want an explanation", a)
	}

	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username", AccountID: account})
	if step.Type != api.LoginStepTypeUserInput || len(step.Fields) != 1 || step.Fields[0].DefaultValue != "expired" {
		t.Fatalf("first step of logging in again = %+v, want the username filled in", step)
	}
	done := call[api.LoginStep](t, c, api.CommandLoginSubmit, api.LoginSubmitParams{
		ProcessID: step.ProcessID, Values: map[string]string{step.Fields[0].FieldID: step.Fields[0].DefaultValue},
	})
	if done.Type != api.LoginStepTypeComplete || done.AccountID != account {
		t.Fatalf("last step = %+v, want the same account %q", done, account)
	}
	waitAccountState(t, c, account, api.AccountStateConnected)

	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts
	if len(accounts) != 1 || accounts[0].AccountID != account || accounts[0].State != api.AccountStateConnected {
		t.Errorf("accounts after logging in again = %+v", accounts)
	}
	after := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations
	ids := func(convs []api.Conversation) []string {
		out := make([]string, 0, len(convs))
		for _, conv := range convs {
			out = append(out, conv.ConversationID)
		}
		slices.Sort(out)
		return out
	}
	if !slices.Equal(ids(before), ids(after)) {
		t.Errorf("conversations after logging in again = %v, want the same as before: %v", ids(after), ids(before))
	}
	messages := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID}).Messages
	if len(messages) < 2 || messages[0].Text != "before" {
		t.Errorf("messages after logging in again = %+v, want the history kept", messages)
	}
	// The account works again: its contact echoes.
	call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "after"})
	waitEvent(t, c, "echo after logging in again", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool {
		return !m.FromMe && m.ConversationID == conv.ConversationID && m.Text == "after"
	}))
}

// Logging in again on another remote account adds that account and leaves
// the first one, with its conversations, as it was: unlike bridgev2's
// provisioning API, the core never removes an account the user did not log
// out of (ADR 0030).
func TestLoginAgainOnAnotherAccountKeepsBoth(t *testing.T) {
	c := open(t)
	first := loginAs(t, c, "alice", conversationsPerLogin)
	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "code", AccountID: first})
	done := call[api.LoginStep](t, c, api.CommandLoginWait, api.LoginProcessParams{ProcessID: step.ProcessID})
	if done.Type != api.LoginStepTypeComplete || done.AccountID == first {
		t.Fatalf("last step = %+v, want another account", done)
	}
	waitConversations(t, c, 2*conversationsPerLogin)
	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts
	got := []string{}
	for _, a := range accounts {
		got = append(got, a.AccountID)
	}
	if !slices.Contains(got, first) || !slices.Contains(got, done.AccountID) || len(got) != 2 {
		t.Errorf("accounts = %v, want %q and %q", got, first, done.AccountID)
	}
}

func TestLoginAgainNeedsAnAccountOfTheNetwork(t *testing.T) {
	c := open(t)
	account := login(t, c)
	for _, id := range []string{"not an account", accountID("matrix", "alice")} {
		callFails(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username", AccountID: id}, api.ErrorCodeNotFound)
	}
	call[api.Empty](t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: account})
	callFails(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username", AccountID: account}, api.ErrorCodeNotFound)
}
