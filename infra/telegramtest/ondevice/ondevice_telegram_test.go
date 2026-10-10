// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build telegram

// The end-to-end test of Telegram "on device" (T1.6, ADR 0014): the core of the
// apps, with mautrix-telegram inside, talks to production Telegram itself,
// with no homeserver and no Docker. It drives the core through its API, as
// a user interface does. Run it with:
//
//	go test -tags=telegram -v ./infra/telegramtest/ondevice/
//
// It needs the two test bots of ADR 0006 and the private channel (see
// infra/README.md and package telegramtest):
//
//	MUSUBEE_TG_BRIDGE_BOT_TOKEN  bot the core logs in as
//	MUSUBEE_TG_PEER_BOT_TOKEN    bot playing the remote party
//	MUSUBEE_TG_CHAT_ID           the channel
//	MUSUBEE_TG_API_ID, MUSUBEE_TG_API_HASH  the application (optional)
//
// Without the tokens it is skipped, unless MUSUBEE_REQUIRE_TELEGRAM_E2E=1 (CI
// with secrets), in which case it fails. A bot has one session at a time:
// never run it alongside another Telegram end-to-end test.

package ondevice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/infra/coretest"
	"github.com/quentinemusee/musubee/infra/telegramtest"
)

const (
	bridgeBotTokenEnv = "MUSUBEE_TG_BRIDGE_BOT_TOKEN"
	peerBotTokenEnv   = "MUSUBEE_TG_PEER_BOT_TOKEN"
	chatIDEnv         = "MUSUBEE_TG_CHAT_ID"
)

// TestTelegramOnDevice logs the core in as the bridge bot, receives a post
// of the peer bot in the channel, answers it, checks that the peer bot sees
// the answer, and logs out. It reports the timings and the core's memory.
func TestTelegramOnDevice(t *testing.T) {
	bridgeToken, peerToken, chatText := os.Getenv(bridgeBotTokenEnv), os.Getenv(peerBotTokenEnv), os.Getenv(chatIDEnv)
	if bridgeToken == "" || peerToken == "" || chatText == "" {
		if os.Getenv("MUSUBEE_REQUIRE_TELEGRAM_E2E") == "1" {
			t.Fatalf("%s, %s and %s are required", bridgeBotTokenEnv, peerBotTokenEnv, chatIDEnv)
		}
		t.Skipf("set %s, %s and %s to run this test (see infra/README.md)", bridgeBotTokenEnv, peerBotTokenEnv, chatIDEnv)
	}
	chatID, err := strconv.ParseInt(chatText, 10, 64)
	if err != nil {
		t.Fatalf("%s is not a number", chatIDEnv)
	}
	creds, err := telegramtest.CredentialsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	peer := telegramtest.BotAPI{Token: peerToken}

	dataDir := t.TempDir()
	started := time.Now()
	c := openCore(t, dataDir, creds)
	t.Logf("core open in %s", time.Since(started).Round(time.Millisecond))
	events := coretest.Pump(t, c)

	// 1. Telegram is offered, with the bot flow.
	var networks api.NetworksListResult
	coretest.Call(t, c, api.CommandNetworksList, nil, &networks)
	if !hasBotFlow(networks) {
		t.Fatalf("networks = %+v, want telegram with the bot flow", networks)
	}

	// 2. Log in as the bridge bot.
	started = time.Now()
	var step api.LoginStep
	coretest.Call(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "telegram", FlowID: "bot"}, &step)
	if step.Type != api.LoginStepTypeUserInput || len(step.Fields) != 1 || step.Fields[0].Type != api.LoginFieldTypeToken {
		t.Fatalf("bot login started with %+v, want one token field", step)
	}
	if coreErr := coretest.TryCall(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{
		ProcessID: step.ProcessID, Values: map[string]string{step.Fields[0].FieldID: bridgeToken},
	}, &step); coreErr != nil {
		t.Fatalf("submitting the bot token: %s", explain(coreErr.Message, creds))
	}
	if step.Type != api.LoginStepTypeComplete || step.AccountID == "" {
		t.Fatalf("bot login ended with step %q", step.Type)
	}
	account := step.AccountID
	loggedIn := false
	t.Cleanup(func() {
		if !loggedIn {
			return
		}
		// Never leave a session of the bot behind: the next run could not
		// receive its updates.
		_ = coretest.TryCall(t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: account}, nil)
	})
	loggedIn = true
	t.Logf("login done in %s", time.Since(started).Round(time.Millisecond))
	events.Wait(t, ctx, "the account connected", 2*time.Minute, func(e coretest.Event) bool {
		var data api.AccountEvent
		return e.Type == api.EventAccountUpdated && e.Decode(t, &data) &&
			data.Account.AccountID == account && data.Account.State == api.AccountStateConnected
	})
	t.Logf("connected %s after the login started", time.Since(started).Round(time.Millisecond))

	// 3. Telegram -> core: the peer bot posts in the channel. A missed post
	// is retried with a new message, and reported, so that flakiness stays
	// visible.
	var received api.Message
	for attempt := 1; attempt <= 3 && received.ConversationID == ""; attempt++ {
		inbound := fmt.Sprintf("musubee on-device e2e: from Telegram %s (attempt %d)", coretest.RandomHex(t), attempt)
		posted := time.Now()
		if _, err := peer.SendMessage(ctx, chatID, inbound); err != nil {
			t.Fatalf("peer bot posting in the channel: %v", err)
		}
		e, ok := events.Find(ctx, time.Minute, func(e coretest.Event) bool {
			var data api.MessageEvent
			return e.Type == api.EventMessageAdded && e.Decode(t, &data) && data.Message.Text == inbound
		})
		if !ok {
			t.Logf("attempt %d: the post did not reach the core within a minute", attempt)
			continue
		}
		var data api.MessageEvent
		e.Decode(t, &data)
		received = data.Message
		t.Logf("Telegram -> core: %s (attempt %d)", time.Since(posted).Round(time.Millisecond), attempt)
	}
	if received.ConversationID == "" {
		t.Fatal("no post of the peer bot reached the core after 3 attempts")
	}
	if received.FromMe || received.Status != api.MessageStatusReceived {
		t.Errorf("received message: from_me %t, status %q", received.FromMe, received.Status)
	}
	var convs api.ConversationsListResult
	coretest.Call(t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: account}, &convs)
	if !hasConversation(convs, received.ConversationID) {
		t.Errorf("the channel is not among the account's conversations: %d of them", len(convs.Conversations))
	}

	// 4. core -> Telegram: answer in the channel.
	outbound := "musubee on-device e2e: from the core " + coretest.RandomHex(t)
	sent := time.Now()
	var sendResult api.MessagesSendResult
	coretest.Call(t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: received.ConversationID, Text: outbound}, &sendResult)
	events.Wait(t, ctx, "the answer sent", time.Minute, func(e coretest.Event) bool {
		var data api.MessageEvent
		return (e.Type == api.EventMessageUpdated || e.Type == api.EventMessageAdded) && e.Decode(t, &data) &&
			data.Message.MessageID == sendResult.Message.MessageID && data.Message.Status == api.MessageStatusSent
	})
	t.Logf("core -> Telegram, sent status: %s", time.Since(sent).Round(time.Millisecond))
	if err := peer.WaitForText(ctx, chatID, outbound, 2*time.Minute); err != nil {
		t.Fatalf("peer bot waiting for the answer: %v", err)
	}
	t.Logf("core -> Telegram, seen by the peer bot: %s", time.Since(sent).Round(time.Millisecond))

	// The session's authorization key is sealed (ADR 0019), checked while
	// the core runs: its write-ahead log counts too.
	coretest.CheckSessionsSealed(t, dataDir, `"auth_key"`, `"push_encryption_key"`)

	var stats api.StatsResult
	coretest.Call(t, c, api.CommandDebugStats, nil, &stats)
	t.Logf("core memory: heap %.1f MiB in use, %.1f MiB from the system; %d goroutines",
		float64(stats.HeapAllocBytes)/(1<<20), float64(stats.HeapSysBytes)/(1<<20), stats.Goroutines)

	// 5. Log out.
	coretest.Call(t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: account}, nil)
	loggedIn = false
	var accounts api.AccountsListResult
	coretest.Call(t, c, api.CommandAccountsList, nil, &accounts)
	if len(accounts.Accounts) != 0 {
		t.Errorf("%d accounts left after the logout", len(accounts.Accounts))
	}
	if err := c.Close(); err != nil {
		t.Errorf("closing the core: %v", err)
	}

	// 6. The log holds neither the messages nor the bot's token.
	logData, err := os.ReadFile(filepath.Join(dataDir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	for what, secret := range map[string]string{"the received message": received.Text, "the sent message": outbound, "the bot token": bridgeToken} {
		if strings.Contains(string(logData), secret) {
			t.Errorf("core.log contains %s", what)
		}
	}
}

func openCore(t *testing.T, dataDir string, creds telegramtest.Credentials) *embedded.Core {
	t.Helper()
	return coretest.Open(t, embedded.Config{
		DataDir:  dataDir,
		LogLevel: "debug",
		Telegram: &embedded.TelegramConfig{APIID: creds.APIID, APIHash: creds.APIHash},
	})
}

// explain adds what to do about Telegram's rate limits.
func explain(message string, creds telegramtest.Credentials) string {
	switch {
	case strings.Contains(message, "FLOOD_WAIT"):
		return message + " (Telegram rate-limits bot logins after many runs: wait the number of seconds shown, then rerun)"
	case strings.Contains(message, "API_ID_PUBLISHED_FLOOD") && creds.IsPublicTestApp():
		return message + fmt.Sprintf(" (the public test api_id is rate-limited by Telegram: set %s and %s, see infra/README.md)", telegramtest.APIIDEnv, telegramtest.APIHashEnv)
	}
	return message
}

func hasBotFlow(networks api.NetworksListResult) bool {
	for _, n := range networks.Networks {
		for _, f := range n.LoginFlows {
			if n.NetworkID == "telegram" && f.FlowID == "bot" {
				return true
			}
		}
	}
	return false
}

func hasConversation(convs api.ConversationsListResult, id string) bool {
	for _, conv := range convs.Conversations {
		if conv.ConversationID == id {
			return true
		}
	}
	return false
}
