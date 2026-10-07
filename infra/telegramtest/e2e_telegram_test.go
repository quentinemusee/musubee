// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build telegram

// End-to-end tests of the mautrix-telegram bridge with Synapse and
// PostgreSQL, on production Telegram with dedicated test bots (ADR 0005).
// Run them with:
//
//	go test -tags=telegram -v ./infra/telegramtest/
//
// The first test needs only Docker and network access. The message test
// needs two test bots, administrators of a private channel (see
// infra/README.md):
//
//	MUSUBEE_TG_BRIDGE_BOT_TOKEN  bot the bridge logs in as
//	MUSUBEE_TG_PEER_BOT_TOKEN    bot playing the remote party
//	MUSUBEE_TG_CHAT_ID           the channel; found in the peer bot's recent
//	                             updates when unset
//
// Without the tokens it is skipped, unless MUSUBEE_REQUIRE_TELEGRAM_E2E=1 (CI
// with secrets), in which case it fails. Without Docker everything is skipped,
// unless MUSUBEE_REQUIRE_INTEGRATION=1.

package telegramtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/infra/testenv"
)

const (
	bridgeBotTokenEnv = "MUSUBEE_TG_BRIDGE_BOT_TOKEN"
	peerBotTokenEnv   = "MUSUBEE_TG_PEER_BOT_TOKEN"
	chatIDEnv         = "MUSUBEE_TG_CHAT_ID"

	// botTokenField is the input field of mautrix-telegram's bot login
	// (pkg/connector/loginbot.go).
	botTokenField = "fi.mau.telegram.login.bot_token"
)

var env *testenv.Env

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if err := testenv.DockerAvailable(ctx); err != nil {
		if os.Getenv("MUSUBEE_REQUIRE_INTEGRATION") == "1" {
			fmt.Fprintln(os.Stderr, "Telegram tests required but Docker is unavailable:", err)
			return 1
		}
		fmt.Fprintln(os.Stderr, "skipping Telegram tests, Docker is unavailable:", err)
		return 0
	}
	creds, err := CredentialsFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	project := "musubee-tg-" + randomHex(4)
	secretsDir, err := os.MkdirTemp("", project+"-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	started := time.Now()
	env, err = testenv.StartConfig(ctx, testenv.Config{
		Project:    project,
		SecretsDir: secretsDir,
		Telegram:   &testenv.TelegramOptions{APIID: creds.APIID, APIHash: creds.APIHash},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "environment %s with the Telegram bridge ready in %s (public test app: %t)\n",
		project, time.Since(started).Round(time.Second), creds.IsPublicTestApp())

	code := m.Run()

	if err := env.Stop(context.WithoutCancel(ctx)); err != nil {
		fmt.Fprintln(os.Stderr, "stopping the environment:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// TestBridgeOffersLogin needs no Telegram account: the bridge must start,
// register with Synapse, offer the bot login, and obtain a QR login token
// from Telegram (which proves it reaches Telegram's servers).
func TestBridgeOffersLogin(t *testing.T) {
	ctx := t.Context()
	user := newMatrixUser(t, "probe")

	flows, err := env.Bridge.LoginFlows(ctx, user.UserID)
	if err != nil {
		t.Fatalf("listing login flows: %v", err)
	}
	for _, want := range []string{"bot", "qr"} {
		if !hasFlow(flows, want) {
			t.Fatalf("login flows %+v do not include %q", flows, want)
		}
	}

	step, err := env.Bridge.StartLogin(ctx, user.UserID, "qr")
	if err != nil {
		t.Fatalf("starting the QR login: %v", err)
	}
	t.Cleanup(func() { _ = env.Bridge.CancelLogin(context.WithoutCancel(ctx), user.UserID, step.LoginID) })
	if step.Type != "display_and_wait" || step.DisplayAndWait == nil || step.DisplayAndWait.Type != "qr" {
		t.Fatalf("unexpected login step %+v", step)
	}
	if u, err := url.Parse(step.DisplayAndWait.Data); err != nil || u.Scheme != "tg" || u.Query().Get("token") == "" {
		t.Fatalf("unexpected QR data %q", step.DisplayAndWait.Data)
	}
}

// TestMessageFlowsThroughTheBridge is the T0.4 acceptance test, with bots:
// the bridge logs in as the bridge bot; the peer bot posts in the channel;
// the post reaches the Matrix user through the bridge; the Matrix user
// replies; the peer bot sees the reply in the channel.
func TestMessageFlowsThroughTheBridge(t *testing.T) {
	bridgeToken, peerToken := os.Getenv(bridgeBotTokenEnv), os.Getenv(peerBotTokenEnv)
	if bridgeToken == "" || peerToken == "" {
		if os.Getenv("MUSUBEE_REQUIRE_TELEGRAM_E2E") == "1" {
			t.Fatalf("%s and %s are required", bridgeBotTokenEnv, peerBotTokenEnv)
		}
		t.Skipf("set %s and %s to run this test (see infra/README.md)", bridgeBotTokenEnv, peerBotTokenEnv)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()

	peer := BotAPI{Token: peerToken}
	peerUser, err := peer.GetMe(ctx)
	if err != nil {
		t.Fatalf("peer bot getMe: %v", err)
	}
	t.Logf("peer bot: @%s", peerUser.Username)
	chatID := channelID(t, ctx, peer)
	t.Logf("channel: %d", chatID)

	// 1. Log the bridge in as the bridge bot.
	alice := newMatrixUser(t, "alice")
	step, err := env.Bridge.StartLogin(ctx, alice.UserID, "bot")
	if err != nil {
		t.Fatalf("starting the bot login: %v", err)
	}
	if step.Type != "user_input" {
		t.Fatalf("bot login started with step %q, want user_input", step.Type)
	}
	done, err := env.Bridge.SubmitInput(ctx, alice.UserID, step, map[string]string{botTokenField: bridgeToken})
	if err != nil {
		if strings.Contains(err.Error(), "API_ID_PUBLISHED_FLOOD") {
			t.Fatalf("submitting the bot token: %v (the public test api_id is rate-limited by Telegram: set %s and %s to the project's own application, see infra/README.md)", err, APIIDEnv, APIHashEnv)
		}
		t.Fatalf("submitting the bot token: %v", err)
	}
	if done.Type != "complete" || done.Complete == nil {
		t.Fatalf("bot login ended with step %q, want complete", done.Type)
	}
	t.Cleanup(func() {
		_ = env.Bridge.Logout(context.WithoutCancel(ctx), alice.UserID, done.Complete.UserLoginID)
	})

	// 2. Telegram -> Matrix: the peer bot posts in the channel.
	inbound := "musubee e2e: from Telegram " + randomHex(4)
	if _, err := peer.SendMessage(ctx, chatID, inbound); err != nil {
		t.Fatalf("peer bot posting in the channel: %v", err)
	}
	roomID := waitForMatrixMessage(t, ctx, alice, inbound)

	// 3. Matrix -> Telegram: the Matrix user replies in the portal room.
	outbound := "musubee e2e: from Matrix " + randomHex(4)
	if _, err := alice.SendText(ctx, roomID, outbound); err != nil {
		t.Fatalf("sending the reply on Matrix: %v", err)
	}
	if err := peer.WaitForText(ctx, chatID, outbound, 2*time.Minute); err != nil {
		t.Fatalf("peer bot waiting for the reply: %v", err)
	}
}

// channelID returns MUSUBEE_TG_CHAT_ID, or the channel found in the peer
// bot's recent updates (Telegram keeps them for 24 hours only, so the ID is
// printed to be stored as a CI variable).
func channelID(t *testing.T, ctx context.Context, peer BotAPI) int64 {
	t.Helper()
	if text := os.Getenv(chatIDEnv); text != "" {
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			t.Fatalf("%s: %v", chatIDEnv, err)
		}
		return id
	}
	updates, err := peer.Updates(ctx, 0)
	if err != nil {
		t.Fatalf("peer bot getUpdates: %v", err)
	}
	chat, err := FindChannel(updates)
	if err != nil {
		t.Logf("peer bot updates: %s", DescribeUpdates(updates))
		if info, infoErr := peer.GetWebhookInfo(ctx); infoErr == nil {
			t.Logf("peer bot webhook set: %t, pending updates: %d, last error: %q", info.URL != "", info.PendingUpdateCount, info.LastErrorMessage)
		}
		t.Fatalf("finding the test channel: %v (post a message in the channel, or set %s)", err, chatIDEnv)
	}
	t.Logf("found channel %q: set %s=%d to keep using it after its updates expire", chat.Title, chatIDEnv, chat.ID)
	return chat.ID
}

func newMatrixUser(t *testing.T, name string) *mautrix.Client {
	t.Helper()
	localpart := name + "-" + randomHex(4)
	password := randomHex(16)
	if _, err := env.RegisterUser(t.Context(), localpart, password, false); err != nil {
		t.Fatal(err)
	}
	client, err := env.Login(t.Context(), localpart, password)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func hasFlow(flows []testenv.LoginFlow, id string) bool {
	for _, f := range flows {
		if f.ID == id {
			return true
		}
	}
	return false
}

// waitForMatrixMessage syncs as the Matrix user, joins the rooms the bridge
// invites it to, and returns the room in which text arrives.
func waitForMatrixMessage(t *testing.T, ctx context.Context, user *mautrix.Client, text string) id.RoomID {
	t.Helper()
	since := ""
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := user.SyncRequest(ctx, 10000, since, "", false, event.PresenceOffline)
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		since = resp.NextBatch
		for roomID := range resp.Rooms.Invite {
			if _, err := user.JoinRoomByID(ctx, roomID); err != nil {
				t.Fatalf("joining %s: %v", roomID, err)
			}
		}
		for roomID, room := range resp.Rooms.Join {
			for _, evt := range room.Timeline.Events {
				if evt.Type != event.EventMessage {
					continue
				}
				_ = evt.Content.ParseRaw(evt.Type)
				if msg := evt.Content.AsMessage(); msg != nil && msg.Body == text {
					return roomID
				}
			}
		}
	}
	t.Fatalf("message %q did not reach Matrix within 3 minutes", text)
	return ""
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
