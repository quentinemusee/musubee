// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package matrixtest

import (
	"context"
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/infra/coretest"
)

// TestMatrixHistory fetches a room's history from before the core's device
// existed (T2.29, docs/ADR/0045): it comes back once, in order, from bob,
// as messages the device cannot decrypt, since their keys were never
// shared with it; then the history ends.
func TestMatrixHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	run := coretest.RandomHex(t)
	aliceLocal, alicePassword := "alice-h-"+run, "alice password "+run
	aliceID, err := env.RegisterUser(ctx, aliceLocal, alicePassword, false)
	if err != nil {
		t.Fatal(err)
	}
	bobLocal, bobPassword := "bob-h-"+run, "bob password "+run
	if _, err = env.RegisterUser(ctx, bobLocal, bobPassword, false); err != nil {
		t.Fatal(err)
	}
	bob := newPeer(t, ctx, bobLocal, bobPassword)
	room := bob.createDirect(t, ctx, aliceID)
	bob.send(t, ctx, room, "before Musubee 1")
	bob.send(t, ctx, room, "before Musubee 2")

	c := coretest.Open(t, embedded.Config{DataDir: t.TempDir(), LogLevel: "debug"})
	events := coretest.Pump(t, c)
	step := startLogin(t, c)
	coretest.Call(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{
		"server": env.HomeserverURL, "username": aliceLocal, "password": alicePassword,
	}}, &step)
	waitConnected(t, ctx, events, step.AccountID)
	e := events.Wait(t, ctx, "the conversation with bob", time.Minute, func(e coretest.Event) bool {
		var data api.ConversationEvent
		return e.Type == api.EventConversationUpdated && e.Decode(t, &data) && data.Conversation.Name == bobName
	})
	var conv api.ConversationEvent
	e.Decode(t, &conv)

	// Page back to the start. The sync may have brought the latest of
	// these messages already: the whole history counts, each message once.
	for range 10 {
		var page api.MessagesFetchOlderResult
		coretest.Call(t, c, api.CommandMessagesFetchOlder, api.MessagesFetchOlderParams{ConversationID: conv.Conversation.ConversationID}, &page)
		if page.End {
			break
		}
	}
	var all api.MessagesListResult
	coretest.Call(t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.Conversation.ConversationID, Limit: 200}, &all)
	var fromBob []api.Message
	for _, m := range all.Messages {
		if !m.FromMe {
			fromBob = append(fromBob, m)
		}
	}
	if len(fromBob) != 2 {
		t.Fatalf("history from bob = %+v, want his two messages, once each", fromBob)
	}
	for _, m := range fromBob {
		if m.Kind != api.MessageKindNotice || m.SenderName != bobName {
			t.Errorf("a message from before the device = %+v, want an undecryptable notice from bob", m)
		}
	}
	if fromBob[0].TimestampMS > fromBob[1].TimestampMS {
		t.Errorf("history out of order: %+v", fromBob)
	}
}
