// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

// The end-to-end test of native Matrix accounts (T2.2, docs/ADR/0018): the
// core, driven through its API like the apps drive it, signs in to a real
// Synapse and exchanges end-to-end encrypted messages with another user's
// Matrix client. Run it with:
//
//	go test -tags=integration,goolm -v ./infra/matrixtest/
//
// Without Docker it is skipped, unless MUSUBEE_REQUIRE_INTEGRATION=1 (set in
// CI), in which case it fails.

package matrixtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
	"github.com/quentinemusee/musubee/infra/coretest"
	"github.com/quentinemusee/musubee/infra/testenv"
)

var env *testenv.Env

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := testenv.DockerAvailable(ctx); err != nil {
		if os.Getenv("MUSUBEE_REQUIRE_INTEGRATION") == "1" {
			fmt.Fprintln(os.Stderr, "integration tests required but Docker is unavailable:", err)
			return 1
		}
		fmt.Fprintln(os.Stderr, "skipping integration tests, Docker is unavailable:", err)
		return 0
	}
	project := "musubee-mx-" + suffix()
	secretsDir, err := os.MkdirTemp("", project+"-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	env, err = testenv.Start(ctx, project, secretsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	code := m.Run()
	if err := env.Stop(context.WithoutCancel(ctx)); err != nil {
		fmt.Fprintln(os.Stderr, "stopping the environment:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func suffix() string {
	return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffff)
}

const bobName = "Bob Tester"

// TestMatrixAccount signs the core in as alice, receives an encrypted direct
// conversation from bob, exchanges messages both ways, restarts the core,
// exchanges again, and logs out.
func TestMatrixAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	run := coretest.RandomHex(t)
	aliceLocal, alicePassword := "alice-"+run, "alice password "+run
	aliceID, err := env.RegisterUser(ctx, aliceLocal, alicePassword, false)
	if err != nil {
		t.Fatal(err)
	}
	bobLocal, bobPassword := "bob-"+run, "bob password "+run
	if _, err := env.RegisterUser(ctx, bobLocal, bobPassword, false); err != nil {
		t.Fatal(err)
	}
	bob := newPeer(t, ctx, bobLocal, bobPassword)

	dataDir := t.TempDir()
	// Registered before the core opens, so that it runs once the core is
	// closed.
	t.Cleanup(func() { reportLog(t, dataDir) })
	c := coretest.Open(t, embedded.Config{DataDir: dataDir, LogLevel: "debug"})
	events := coretest.Pump(t, c)

	// 1. A wrong password is refused, and says so.
	step := startLogin(t, c)
	coreErr := coretest.TryCall(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{
		"server": env.HomeserverURL, "username": aliceLocal, "password": "not the password",
	}}, nil)
	if coreErr == nil || !strings.Contains(coreErr.Message, "wrong username or password") {
		t.Fatalf("login with a wrong password: %+v", coreErr)
	}

	// 2. Sign in.
	step = startLogin(t, c)
	coretest.Call(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{
		"server": env.HomeserverURL, "username": aliceLocal, "password": alicePassword,
	}}, &step)
	if step.Type != api.LoginStepTypeComplete || step.AccountID == "" {
		t.Fatalf("login ended with %+v", step)
	}
	account := step.AccountID
	waitConnected(t, ctx, events, account)
	var accounts api.AccountsListResult
	coretest.Call(t, c, api.CommandAccountsList, nil, &accounts)
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Name != aliceLocal+" ("+testenv.ServerName+")" || accounts.Accounts[0].NetworkID != "matrix" {
		t.Fatalf("accounts = %+v", accounts.Accounts)
	}

	// 3. bob opens an encrypted direct conversation; the core accepts it.
	room := bob.createDirect(t, ctx, aliceID)
	e := events.Wait(t, ctx, "the conversation with bob", time.Minute, func(e coretest.Event) bool {
		var data api.ConversationEvent
		return e.Type == api.EventConversationUpdated && e.Decode(t, &data) &&
			data.Conversation.AccountID == account && data.Conversation.Name == bobName
	})
	var convEvent api.ConversationEvent
	e.Decode(t, &convEvent)
	conv := convEvent.Conversation
	if conv.Kind != api.ConversationKindDirect || conv.NetworkID != "matrix" {
		t.Fatalf("conversation = %+v", conv)
	}
	bob.waitJoined(t, ctx, room, aliceID)

	// 4. bob -> core, then core -> bob, encrypted.
	exchange(t, ctx, c, events, bob, room, conv.ConversationID, "before the restart")

	// 5. A direct conversation can belong to a person.
	var person api.PersonResult
	coretest.Call(t, c, api.CommandPersonsCreate, api.PersonsCreateParams{ConversationIds: []string{conv.ConversationID}}, &person)
	if person.Person.Name != bobName {
		t.Errorf("person = %+v", person.Person)
	}

	// 6. Restart: the session and the keys are kept.
	if err := c.Close(); err != nil {
		t.Fatalf("closing the core: %v", err)
	}
	c = coretest.Open(t, embedded.Config{DataDir: dataDir, LogLevel: "debug"})
	events = coretest.Pump(t, c)
	waitConnected(t, ctx, events, account)
	// An invitation to a group is not accepted for the user (ADR 0018).
	group, err := bob.cli.CreateRoom(ctx, &mautrix.ReqCreateRoom{Preset: "private_chat", Name: "Group " + run, Invite: []id.UserID{aliceID}})
	if err != nil {
		t.Fatalf("creating the group: %v", err)
	}
	texts := exchange(t, ctx, c, events, bob, room, conv.ConversationID, "after the restart")

	var history api.MessagesListResult
	coretest.Call(t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID}, &history)
	var listed []string
	for _, m := range history.Messages {
		listed = append(listed, m.Text)
	}
	for _, text := range texts {
		if !slices.Contains(listed, text) {
			t.Errorf("the history after the restart lacks a message: %d listed", len(listed))
		}
	}
	var member event.MemberEventContent
	if err := bob.cli.StateEvent(ctx, group.RoomID, event.StateMember, aliceID.String(), &member); err != nil {
		t.Fatal(err)
	}
	if member.Membership != event.MembershipInvite {
		t.Errorf("alice's membership in the group = %s, want invite", member.Membership)
	}

	// 7. Log out: the device is deleted on the server, and its keys on the
	// device.
	coretest.Call(t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: account}, nil)
	coretest.Call(t, c, api.CommandAccountsList, nil, &accounts)
	if len(accounts.Accounts) != 0 {
		t.Errorf("%d accounts left after the logout", len(accounts.Accounts))
	}
	keys, err := bob.cli.QueryKeys(ctx, &mautrix.ReqQueryKeys{DeviceKeys: mautrix.DeviceKeysRequest{aliceID: {}}})
	if err != nil {
		t.Fatal(err)
	}
	if devices := keys.DeviceKeys[aliceID]; len(devices) != 0 {
		t.Errorf("alice still has %d devices after the logout", len(devices))
	}
	if err := c.Close(); err != nil {
		t.Errorf("closing the core: %v", err)
	}
	checkNoKeysLeft(t, filepath.Join(dataDir, "core.db"))

	// 8. The log holds neither the messages nor the password.
	logData, err := os.ReadFile(filepath.Join(dataDir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	for i, secret := range append(texts, alicePassword) {
		if strings.Contains(string(logData), secret) {
			t.Errorf("core.log contains secret #%d (a message or the password)", i)
		}
	}
}

func startLogin(t *testing.T, c *embedded.Core) api.LoginStep {
	t.Helper()
	var step api.LoginStep
	coretest.Call(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "matrix", FlowID: "password"}, &step)
	return step
}

// reportLog prints the warnings, the errors and the account state changes
// of the core's log when the test failed, to diagnose it on CI. These lines
// carry no message text: the core never logs it, which step 8 checks.
func reportLog(t *testing.T, dataDir string) {
	if !t.Failed() {
		return
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "core.log"))
	if err != nil {
		t.Logf("reading core.log: %v", err)
		return
	}
	for line := range strings.Lines(string(data)) {
		if strings.Contains(line, `"level":"warn"`) || strings.Contains(line, `"level":"error"`) ||
			strings.Contains(line, `"state_event"`) {
			t.Log(strings.TrimSpace(line))
		}
	}
}

func waitConnected(t *testing.T, ctx context.Context, events coretest.Events, account string) {
	t.Helper()
	events.Wait(t, ctx, "the account connected", time.Minute, func(e coretest.Event) bool {
		var data api.AccountEvent
		return e.Type == api.EventAccountUpdated && e.Decode(t, &data) &&
			data.Account.AccountID == account && data.Account.State == api.AccountStateConnected
	})
}

// exchange sends a message from bob to the core and one from the core to
// bob, and returns their texts.
func exchange(t *testing.T, ctx context.Context, c *embedded.Core, events coretest.Events, bob *peer, room id.RoomID, conversation, when string) []string {
	t.Helper()
	inbound := "from bob " + when + " " + coretest.RandomHex(t)
	bob.send(t, ctx, room, inbound)
	e := events.Wait(t, ctx, "bob's message "+when, time.Minute, func(e coretest.Event) bool {
		var data api.MessageEvent
		return e.Type == api.EventMessageAdded && e.Decode(t, &data) && data.Message.Text == inbound
	})
	var received api.MessageEvent
	e.Decode(t, &received)
	if m := received.Message; m.ConversationID != conversation || m.FromMe || m.SenderName != bobName || m.Kind != api.MessageKindText {
		t.Errorf("bob's message %s: conversation %t, from_me %t, sender %q, kind %s", when, m.ConversationID == conversation, m.FromMe, m.SenderName, m.Kind)
	}

	outbound := "from the core " + when + " " + coretest.RandomHex(t)
	var sent api.MessagesSendResult
	coretest.Call(t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conversation, Text: outbound}, &sent)
	events.Wait(t, ctx, "the answer sent "+when, time.Minute, func(e coretest.Event) bool {
		var data api.MessageEvent
		return (e.Type == api.EventMessageUpdated || e.Type == api.EventMessageAdded) && e.Decode(t, &data) &&
			data.Message.MessageID == sent.Message.MessageID && data.Message.Status == api.MessageStatusSent
	})
	bob.waitDecrypted(t, ctx, room, outbound)
	return []string{inbound, outbound}
}

// peer is bob: another user's Matrix client, with end-to-end encryption.
type peer struct {
	cli *mautrix.Client

	mu sync.Mutex
	// decrypted holds the texts of the encrypted messages bob decrypted.
	decrypted []string
}

func newPeer(t *testing.T, ctx context.Context, localpart, password string) *peer {
	t.Helper()
	cli, err := env.Login(ctx, localpart, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.SetDisplayName(ctx, bobName); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "bob.db"))
	if err != nil {
		t.Fatal(err)
	}
	helper, err := cryptohelper.NewCryptoHelper(cli, []byte("bob's test pickle key"), db)
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Init(ctx); err != nil {
		t.Fatalf("bob's encryption: %v", err)
	}
	cli.Crypto = helper
	p := &peer{cli: cli}
	cli.Syncer.(*mautrix.DefaultSyncer).OnEventType(event.EventMessage, func(_ context.Context, evt *event.Event) {
		if evt.Mautrix.EventSource&event.SourceDecrypted == 0 {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.decrypted = append(p.decrypted, evt.Content.AsMessage().Body)
	})
	syncCtx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cli.SyncWithContext(syncCtx)
	}()
	t.Cleanup(func() {
		stop()
		cli.StopSync()
		<-done
		_ = helper.Close()
	})
	return p
}

func (p *peer) createDirect(t *testing.T, ctx context.Context, with id.UserID) id.RoomID {
	t.Helper()
	resp, err := p.cli.CreateRoom(ctx, &mautrix.ReqCreateRoom{
		Preset:   "trusted_private_chat",
		IsDirect: true,
		Invite:   []id.UserID{with},
		InitialState: []*event.Event{{
			Type:    event.StateEncryption,
			Content: event.Content{Parsed: &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}},
		}},
	})
	if err != nil {
		t.Fatalf("creating the direct conversation: %v", err)
	}
	return resp.RoomID
}

func (p *peer) waitJoined(t *testing.T, ctx context.Context, room id.RoomID, user id.UserID) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		members, err := p.cli.JoinedMembers(ctx, room)
		if err == nil {
			if _, ok := members.Joined[user]; ok {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the core did not join the direct conversation")
}

// send sends a message, and checks that it went encrypted.
func (p *peer) send(t *testing.T, ctx context.Context, room id.RoomID, text string) {
	t.Helper()
	resp, err := p.cli.SendText(ctx, room, text)
	if err != nil {
		t.Fatalf("bob sending: %v", err)
	}
	evt, err := p.cli.GetEvent(ctx, room, resp.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != event.EventEncrypted {
		t.Fatalf("bob's message went as %s, want encrypted", evt.Type.Type)
	}
}

func (p *peer) waitDecrypted(t *testing.T, ctx context.Context, room id.RoomID, text string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		p.mu.Lock()
		found := slices.Contains(p.decrypted, text)
		p.mu.Unlock()
		if found {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("bob did not receive the core's message, encrypted, in %s", room)
}

// checkNoKeysLeft checks that the logged out device's keys are gone from
// the core's database.
func checkNoKeysLeft(t *testing.T, path string) {
	t.Helper()
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"crypto_account", "crypto_olm_session", "crypto_megolm_inbound_session", "crypto_megolm_outbound_session"} {
		var n int
		if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s has %d rows after the logout", table, n)
		}
	}
}
