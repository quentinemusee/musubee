// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"slices"
	"strings"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// conversationOf returns the conversation of an account with an echo contact.
func conversationOf(t *testing.T, c *Core, account, name string) api.Conversation {
	t.Helper()
	convs := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: account}).Conversations
	for _, conv := range convs {
		if conv.Name == name {
			return conv
		}
	}
	t.Fatalf("account %s has no conversation named %q: %+v", account, name, convs)
	return api.Conversation{}
}

func listPersons(t *testing.T, c *Core) []api.Person {
	t.Helper()
	return call[api.PersonsListResult](t, c, api.CommandPersonsList, nil).Persons
}

// waitPerson waits for the person.updated event of a person with exactly
// the given conversations.
func waitPerson(t *testing.T, c *Core, personID string, conversations ...string) api.Person {
	t.Helper()
	e := waitEvent(t, c, "person.updated", func(e rawEvent) bool {
		p := decode[api.PersonEvent](t, e.Data).Person
		return e.Type == api.EventPersonUpdated && p.PersonID == personID && slices.Equal(p.ConversationIds, conversations)
	})
	return decode[api.PersonEvent](t, e.Data).Person
}

func waitPersonDeleted(t *testing.T, c *Core, personID string) {
	t.Helper()
	waitEvent(t, c, "person.deleted", func(e rawEvent) bool {
		return e.Type == api.EventPersonDeleted && decode[api.PersonDeletedEvent](t, e.Data).PersonID == personID
	})
}

// waitConversationPerson waits for the conversation.updated event that
// gives a conversation its person ("": none).
func waitConversationPerson(t *testing.T, c *Core, conversation, personID string) {
	t.Helper()
	waitEvent(t, c, "conversation.updated with its person", func(e rawEvent) bool {
		conv := decode[api.ConversationEvent](t, e.Data).Conversation
		return e.Type == api.EventConversationUpdated && conv.ConversationID == conversation && conv.PersonID == personID
	})
}

// TestPersonsJourney merges the conversations of two accounts with the same
// contact into one person, then moves, renames, unlinks and deletes, as a
// user would.
func TestPersonsJourney(t *testing.T) {
	c := open(t)
	alice := loginAs(t, c, "alice", 3)
	bob := loginAs(t, c, "bob", 6)
	instantA := conversationOf(t, c, alice, "Instant Echo").ConversationID
	instantB := conversationOf(t, c, bob, "Instant Echo").ConversationID
	delayedA := conversationOf(t, c, alice, "Delayed Echo").ConversationID

	if persons := listPersons(t, c); len(persons) != 0 {
		t.Fatalf("persons before any = %+v", persons)
	}
	// Without a name, the person takes the first conversation's.
	echo := call[api.PersonResult](t, c, api.CommandPersonsCreate, api.PersonsCreateParams{ConversationIds: []string{instantA, instantB}}).Person
	if echo.Name != "Instant Echo" || !strings.HasPrefix(echo.PersonID, "h.") || !slices.Equal(echo.ConversationIds, []string{instantA, instantB}) {
		t.Fatalf("created %+v", echo)
	}
	waitPerson(t, c, echo.PersonID, instantA, instantB)
	waitConversationPerson(t, c, instantA, echo.PersonID)
	waitConversationPerson(t, c, instantB, echo.PersonID)
	if persons := listPersons(t, c); len(persons) != 1 || persons[0].PersonID != echo.PersonID {
		t.Fatalf("persons = %+v", persons)
	}
	for _, conv := range call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations {
		linked := conv.ConversationID == instantA || conv.ConversationID == instantB
		if (conv.PersonID == echo.PersonID) != linked || (!linked && conv.PersonID != "") {
			t.Errorf("conversation %s (%s) has person %q", conv.Name, conv.AccountID, conv.PersonID)
		}
	}

	renamed := call[api.PersonResult](t, c, api.CommandPersonsRename, api.PersonsRenameParams{PersonID: echo.PersonID, Name: "Echo"}).Person
	if renamed.Name != "Echo" || !slices.Equal(renamed.ConversationIds, echo.ConversationIds) {
		t.Fatalf("renamed %+v", renamed)
	}
	if p := waitPerson(t, c, echo.PersonID, instantA, instantB); p.Name != "Echo" {
		t.Fatalf("person.updated after renaming: %+v", p)
	}

	linked := call[api.PersonResult](t, c, api.CommandPersonsLink, api.PersonsLinkParams{PersonID: echo.PersonID, ConversationID: delayedA}).Person
	if !slices.Equal(linked.ConversationIds, []string{instantA, instantB, delayedA}) {
		t.Fatalf("linked %+v", linked)
	}
	waitConversationPerson(t, c, delayedA, echo.PersonID)

	// A new person takes its conversation from the person it belonged to.
	delayed := call[api.PersonResult](t, c, api.CommandPersonsCreate, api.PersonsCreateParams{Name: "Delayed", ConversationIds: []string{delayedA}}).Person
	waitPerson(t, c, delayed.PersonID, delayedA)
	waitPerson(t, c, echo.PersonID, instantA, instantB)
	waitConversationPerson(t, c, delayedA, delayed.PersonID)

	// Its last conversation unlinked, a person is deleted.
	call[api.Empty](t, c, api.CommandPersonsUnlink, api.PersonsUnlinkParams{ConversationID: delayedA})
	waitPersonDeleted(t, c, delayed.PersonID)
	waitConversationPerson(t, c, delayedA, "")
	// Unlinking a conversation without a person changes nothing.
	call[api.Empty](t, c, api.CommandPersonsUnlink, api.PersonsUnlinkParams{ConversationID: delayedA})

	call[api.Empty](t, c, api.CommandPersonsDelete, api.PersonsDeleteParams{PersonID: echo.PersonID})
	waitPersonDeleted(t, c, echo.PersonID)
	waitConversationPerson(t, c, instantA, "")
	waitConversationPerson(t, c, instantB, "")
	if persons := listPersons(t, c); len(persons) != 0 {
		t.Fatalf("persons after deleting = %+v", persons)
	}
}

func TestPersonsRejectBadRequests(t *testing.T) {
	c := open(t)
	account := login(t, c)
	instant := conversationOf(t, c, account, "Instant Echo").ConversationID
	p := call[api.PersonResult](t, c, api.CommandPersonsCreate, api.PersonsCreateParams{ConversationIds: []string{instant}}).Person

	callFails(t, c, api.CommandPersonsCreate, api.PersonsCreateParams{}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandPersonsCreate, api.PersonsCreateParams{Name: "  ", ConversationIds: []string{instant}}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandPersonsCreate, api.PersonsCreateParams{Name: strings.Repeat("\u00e9", 129), ConversationIds: []string{instant}}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandPersonsCreate, api.PersonsCreateParams{ConversationIds: []string{"c.nope"}}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandPersonsCreate, api.PersonsCreateParams{ConversationIds: []string{conversationID("!gone:musubee.local")}}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandPersonsRename, api.PersonsRenameParams{PersonID: p.PersonID, Name: ""}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandPersonsRename, api.PersonsRenameParams{PersonID: "h.nope", Name: "X"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandPersonsLink, api.PersonsLinkParams{PersonID: "h.nope", ConversationID: instant}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandPersonsLink, api.PersonsLinkParams{PersonID: p.PersonID, ConversationID: "c.nope"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandPersonsUnlink, api.PersonsUnlinkParams{ConversationID: "c.nope"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandPersonsDelete, api.PersonsDeleteParams{PersonID: "h.nope"}, api.ErrorCodeNotFound)
	// Nothing changed.
	if persons := listPersons(t, c); len(persons) != 1 || !slices.Equal(persons[0].ConversationIds, []string{instant}) || persons[0].Name != "Instant Echo" {
		t.Fatalf("persons = %+v", persons)
	}
}

// The links name conversations by their identity on their network, not by
// their local room: they survive a restart, and an account logged out then
// added again finds its persons back, although logging out deleted its
// conversations. (Their rooms come back with the same IDs: localmatrix
// derives a portal's room ID from the portal's key.)
func TestPersonsOutliveRoomsAndRestarts(t *testing.T) {
	dir := t.TempDir()
	c := openDir(t, dir)
	alice := loginAs(t, c, "alice", 3)
	bob := loginAs(t, c, "bob", 6)
	instantA := conversationOf(t, c, alice, "Instant Echo").ConversationID
	instantB := conversationOf(t, c, bob, "Instant Echo").ConversationID
	p := call[api.PersonResult](t, c, api.CommandPersonsCreate, api.PersonsCreateParams{Name: "Echo", ConversationIds: []string{instantA, instantB}}).Person
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	c = openDir(t, dir)
	waitConversations(t, c, 6)
	persons := listPersons(t, c)
	if len(persons) != 1 || persons[0].PersonID != p.PersonID || persons[0].Name != "Echo" || !slices.Equal(persons[0].ConversationIds, []string{instantA, instantB}) {
		t.Fatalf("persons after reopening = %+v", persons)
	}
	if conv := conversationOf(t, c, alice, "Instant Echo"); conv.PersonID != p.PersonID {
		t.Fatalf("conversation after reopening = %+v", conv)
	}

	// bob's conversation leaves the device; its link stays, dormant.
	call[api.Empty](t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: bob})
	if convs := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: bob}).Conversations; len(convs) != 0 {
		t.Fatalf("bob's conversations after logout = %+v", convs)
	}
	if persons = listPersons(t, c); len(persons) != 1 || !slices.Equal(persons[0].ConversationIds, []string{instantA}) {
		t.Fatalf("persons after bob's logout = %+v", persons)
	}
	// Without a conversation on the device, the person is not listed.
	call[api.Empty](t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: alice})
	if persons = listPersons(t, c); len(persons) != 0 {
		t.Fatalf("persons without accounts = %+v", persons)
	}

	if again := loginAs(t, c, "bob", 3); again != bob {
		t.Fatalf("bob's account came back as %s, was %s", again, bob)
	}
	if conv := conversationOf(t, c, bob, "Instant Echo"); conv.PersonID != p.PersonID {
		t.Fatalf("bob's new conversation = %+v, want person %s", conv, p.PersonID)
	}
	if persons = listPersons(t, c); len(persons) != 1 || !slices.Equal(persons[0].ConversationIds, []string{instantB}) {
		t.Fatalf("persons after bob's return = %+v", persons)
	}
}
