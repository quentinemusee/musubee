// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build telegram

// End-to-end tests of the mautrix-telegram bridge against Telegram's TEST
// environment, with Synapse and PostgreSQL. Run them with:
//
//	go test -tags=telegram -v ./infra/telegramtest/
//
// The first test needs only Docker and network access. The message test also
// needs a test-environment account and bot (see infra/README.md):
//
//	MUSUBEE_TG_SESSION    session of the test account (go run ./infra/cmd/tgsession)
//	MUSUBEE_TG_BOT_TOKEN  token of a bot created with @BotFather on the test environment
//
// Without them it is skipped, unless MUSUBEE_REQUIRE_TELEGRAM_E2E=1 (CI with
// secrets), in which case it fails. Without Docker everything is skipped,
// unless MUSUBEE_REQUIRE_INTEGRATION=1.

package telegramtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/infra/testenv"
)

const botTokenEnv = "MUSUBEE_TG_BOT_TOKEN"

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

// TestBridgeOffersQRLoginOnTestServers needs no Telegram account: the bridge
// must start, register with Synapse, and obtain a login token from
// Telegram's test environment.
func TestBridgeOffersQRLoginOnTestServers(t *testing.T) {
	ctx := t.Context()
	user := newMatrixUser(t, "qr")

	flows, err := env.Bridge.LoginFlows(ctx, user.UserID)
	if err != nil {
		t.Fatalf("listing login flows: %v", err)
	}
	if !hasFlow(flows, "qr") {
		t.Fatalf("login flows %+v do not include qr", flows)
	}

	step, err := env.Bridge.StartLogin(ctx, user.UserID, "qr")
	if err != nil {
		t.Fatalf("starting the QR login: %v", err)
	}
	t.Cleanup(func() { _ = env.Bridge.CancelLogin(context.WithoutCancel(ctx), user.UserID, step.LoginID) })
	if _, err := loginToken(step); err != nil {
		t.Fatal(err)
	}
}

// TestMessageFlowsThroughTheBridge is the T0.4 acceptance test: a test bot
// writes to the test account, the message reaches Matrix through the bridge,
// the Matrix user replies, and the bot receives the reply on Telegram.
func TestMessageFlowsThroughTheBridge(t *testing.T) {
	session, err := SessionFromEnv(t.Context())
	botToken := os.Getenv(botTokenEnv)
	if errors.Is(err, ErrNoSession) || botToken == "" {
		if os.Getenv("MUSUBEE_REQUIRE_TELEGRAM_E2E") == "1" {
			t.Fatalf("%s and %s are required", SessionEnv, botTokenEnv)
		}
		t.Skipf("set %s and %s to run this test (see infra/README.md)", SessionEnv, botTokenEnv)
	}
	if err != nil {
		t.Fatal(err)
	}
	creds, err := CredentialsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	bot := botAPI{token: botToken}
	botUser, err := bot.getMe(ctx)
	if err != nil {
		t.Fatalf("bot getMe: %v", err)
	}
	offset, err := bot.drainUpdates(ctx)
	if err != nil {
		t.Fatalf("bot getUpdates: %v", err)
	}

	alice := newMatrixUser(t, "alice")
	client := NewClient(creds, session)
	err = client.Run(ctx, func(ctx context.Context) error {
		// 1. Log the bridge in to the test account through the QR flow: the
		//    harness, already logged in, accepts the login token.
		step, err := env.Bridge.StartLogin(ctx, alice.UserID, "qr")
		if err != nil {
			return fmt.Errorf("starting the QR login: %w", err)
		}
		token, err := loginToken(step)
		if err != nil {
			return err
		}
		if _, err := client.API().AuthAcceptLoginToken(ctx, token); err != nil {
			return fmt.Errorf("accepting the login token: %w", err)
		}
		done, err := env.Bridge.WaitStep(ctx, alice.UserID, step)
		if err != nil {
			return fmt.Errorf("finishing the QR login: %w", err)
		}
		if done.Type != "complete" || done.Complete == nil {
			return fmt.Errorf("login ended with step %q, want complete", done.Type)
		}
		t.Cleanup(func() {
			_ = env.Bridge.Logout(context.WithoutCancel(ctx), alice.UserID, done.Complete.UserLoginID)
		})

		// 2. The test account starts the bot, so that the bot may write to it.
		resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: botUser.Username})
		if err != nil {
			return fmt.Errorf("resolving the bot: %w", err)
		}
		botPeer, err := inputUser(resolved, botUser.ID)
		if err != nil {
			return err
		}
		if _, err := message.NewSender(client.API()).To(botPeer).Text(ctx, "/start"); err != nil {
			return fmt.Errorf("starting the bot: %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	accountChatID, offset, err := bot.waitForText(ctx, offset, "/start")
	if err != nil {
		t.Fatalf("bot waiting for /start: %v", err)
	}

	// 3. Telegram -> Matrix.
	inbound := "hello from the test bot " + randomHex(4)
	if err := bot.sendMessage(ctx, accountChatID, inbound); err != nil {
		t.Fatalf("bot sendMessage: %v", err)
	}
	roomID := waitForMatrixMessage(t, ctx, alice, inbound)

	// 4. Matrix -> Telegram.
	outbound := "reply from Matrix " + randomHex(4)
	if _, err := alice.SendText(ctx, roomID, outbound); err != nil {
		t.Fatalf("sending the reply on Matrix: %v", err)
	}
	if _, _, err := bot.waitForText(ctx, offset, outbound); err != nil {
		t.Fatalf("bot waiting for the reply: %v", err)
	}
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

// loginToken extracts the token from the bridge's QR step
// ("tg://login?token=<base64url>").
func loginToken(step *testenv.LoginStep) ([]byte, error) {
	if step.Type != "display_and_wait" || step.DisplayAndWait == nil || step.DisplayAndWait.Type != "qr" {
		return nil, fmt.Errorf("unexpected login step %+v", step)
	}
	u, err := url.Parse(step.DisplayAndWait.Data)
	if err != nil || u.Scheme != "tg" || u.Host != "login" {
		return nil, fmt.Errorf("unexpected QR data %q", step.DisplayAndWait.Data)
	}
	token, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(u.Query().Get("token"), "="))
	if err != nil || len(token) == 0 {
		return nil, fmt.Errorf("invalid login token in %q: %v", step.DisplayAndWait.Data, err)
	}
	return token, nil
}

func inputUser(resolved *tg.ContactsResolvedPeer, userID int64) (tg.InputPeerClass, error) {
	for _, u := range resolved.Users {
		if user, ok := u.(*tg.User); ok && user.ID == userID {
			return &tg.InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("bot %d not found in the resolved peer", userID)
}

// waitForMatrixMessage syncs as the Matrix user, joins the rooms the bridge
// invites it to, and returns the room in which text arrives.
func waitForMatrixMessage(t *testing.T, ctx context.Context, user *mautrix.Client, text string) id.RoomID {
	t.Helper()
	since := ""
	deadline := time.Now().Add(2 * time.Minute)
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
	t.Fatalf("message %q did not reach Matrix within 2 minutes", text)
	return ""
}

// botAPI is a minimal client for the Bot API of Telegram's test environment
// (https://api.telegram.org/bot<token>/test/<method>).
type botAPI struct {
	token string
}

type botUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type botUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func (b botAPI) call(ctx context.Context, method string, params, out any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	endpoint := "https://api.telegram.org/bot" + b.token + "/test/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		// The URL contains the token: never report it.
		return fmt.Errorf("Bot API %s: request failed", method)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("Bot API %s: HTTP %d", method, resp.StatusCode)
	}
	if !envelope.OK {
		return fmt.Errorf("Bot API %s: %s", method, envelope.Description)
	}
	return json.Unmarshal(envelope.Result, out)
}

func (b botAPI) getMe(ctx context.Context) (botUser, error) {
	var u botUser
	err := b.call(ctx, "getMe", map[string]any{}, &u)
	return u, err
}

// drainUpdates skips updates left over from earlier runs and returns the
// offset of the next one.
func (b botAPI) drainUpdates(ctx context.Context) (int64, error) {
	var updates []botUpdate
	if err := b.call(ctx, "getUpdates", map[string]any{"offset": -1, "timeout": 0}, &updates); err != nil {
		return 0, err
	}
	if len(updates) == 0 {
		return 0, nil
	}
	return updates[len(updates)-1].UpdateID + 1, nil
}

func (b botAPI) waitForText(ctx context.Context, offset int64, text string) (chatID, next int64, err error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var updates []botUpdate
		if err := b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 10}, &updates); err != nil {
			return 0, offset, err
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message != nil && u.Message.Text == text {
				return u.Message.Chat.ID, offset, nil
			}
		}
	}
	return 0, offset, fmt.Errorf("no message %q within 2 minutes", text)
}

func (b botAPI) sendMessage(ctx context.Context, chatID int64, text string) error {
	var ignored json.RawMessage
	return b.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, &ignored)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
