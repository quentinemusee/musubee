// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"slices"
	"testing"

	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/quentinemusee/musubee/core/api"
)

// TestMessagesWaitForTheirAccount sends while the account is away (API
// 1.14): the messages wait on the device, queued, and go in order once the
// account connects again.
func TestMessagesWaitForTheirAccount(t *testing.T) {
	c := open(t)
	account := login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	network, loginID, err := parseAccountID(account)
	if err != nil {
		t.Fatal(err)
	}
	userLogin := c.host.Bridge(network).GetCachedUserLoginByID(loginID)
	if userLogin == nil {
		t.Fatal("no login")
	}

	// The network goes away.
	userLogin.Client.Disconnect()
	userLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect})
	waitEvent(t, c, "the account away", func(e rawEvent) bool {
		return e.Type == api.EventAccountUpdated && decode[api.AccountEvent](t, e.Data).Account.State == api.AccountStateTransientDisconnect
	})
	var waiting []string
	for _, text := range []string{"first while away", "second while away"} {
		msg := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: text}).Message
		if msg.Status != api.MessageStatusQueued {
			t.Fatalf("%q: status = %s, want queued", text, msg.Status)
		}
		waiting = append(waiting, msg.MessageID)
	}
	listed := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID})
	for _, m := range listed.Messages {
		if slices.Contains(waiting, m.MessageID) && m.Status != api.MessageStatusQueued {
			t.Errorf("listed %q as %s, want queued", m.Text, m.Status)
		}
	}

	// It comes back: both go, in order, and are echoed.
	userLogin.Client.Connect(t.Context())
	for _, id := range waiting {
		waitEvent(t, c, "a waiting message sent", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool {
			return m.MessageID == id && m.Status == api.MessageStatusSent
		}))
	}
	waitEvent(t, c, "the second echo", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Text == "second while away" }))
	listed = call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID})
	var echoes []string
	for _, m := range listed.Messages {
		if !m.FromMe {
			echoes = append(echoes, m.Text)
		}
	}
	if want := []string{"first while away", "second while away"}; !slices.Equal(echoes, want) {
		t.Errorf("echoes = %q, want %q", echoes, want)
	}
	// Once connected, a message goes at once.
	if msg := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "back"}).Message; msg.Status != api.MessageStatusSending {
		t.Errorf("status after reconnection = %s, want sending", msg.Status)
	}
}
