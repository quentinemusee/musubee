// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/api/apitest"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// These tests are the Go side of the contract tests (docs/ADR/0012): every
// response and every event of the real core is checked against the schema
// by apitest, a validator independent of the generated types.

const (
	eventTimeout = 15 * time.Second
	echoDelay    = 200 * time.Millisecond
)

var (
	validatorOnce sync.Once
	validator     *apitest.Validator
	validatorErr  error
)

func schema(t *testing.T) *apitest.Validator {
	t.Helper()
	validatorOnce.Do(func() { validator, validatorErr = apitest.New() })
	if validatorErr != nil {
		t.Fatal(validatorErr)
	}
	return validator
}

func open(t *testing.T) *Core {
	t.Helper()
	return openDir(t, t.TempDir())
}

func openDir(t *testing.T, dir string) *Core {
	t.Helper()
	cfg, err := json.Marshal(Config{DataDir: dir, LogLevel: "debug", EchoDelayMS: int(echoDelay / time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		// Give goroutines that outlive Close a moment to show themselves.
		time.Sleep(50 * time.Millisecond)
		checkLateLogs(t, c.logs.lateLines())
		if t.Failed() {
			logProblems(t, dir)
		}
	})
	return c
}

// logProblems shows the warnings and errors of a failed test's core.log,
// which t.TempDir deletes: on CI they are the only trace of why bridgev2
// dropped an event.
func logProblems(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Logf("core.log: %v", err)
		return
	}
	shown := 0
	for line := range strings.Lines(string(data)) {
		var entry struct {
			Level string `json:"level"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || (entry.Level != "warn" && entry.Level != "error" && entry.Level != "fatal" && entry.Level != "panic") {
			continue
		}
		if shown == maxLogProblems {
			t.Logf("core.log: more warnings and errors not shown")
			return
		}
		t.Logf("core.log: %s", strings.TrimSpace(line))
		shown++
	}
}

const maxLogProblems = 100

// checkLateLogs accepts, among the lines logged after Close, only those of
// the work bridgev2 aborts when it stops (see logWriter): failures caused by
// the cancellation, and anything else logged by an aborted portal event
// handler, unless it panicked.
func checkLateLogs(t *testing.T, lines []string) {
	t.Helper()
	for _, line := range lines {
		var entry struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal([]byte(line), &entry)
		// Work cut short by Close. When a transaction's context is
		// canceled, database/sql rolls it back itself, and dbutil's own
		// rollback then fails with "already been committed or rolled back".
		aborted := strings.Contains(entry.Error, "context canceled") || strings.Contains(entry.Error, "database is closed") ||
			strings.Contains(entry.Error, "already been committed or rolled back")
		// bridgev2 goroutines that Bridge.Stop does not wait for: event
		// handlers, PostStart (which resends the bridge info to every room
		// on a bridge's first start), and the bridge state queue, whose
		// states localmatrix drops after Stop. Nested loggers repeat the
		// "action" key, and json.Unmarshal keeps only the last one: look
		// for it in the raw line.
		background := strings.Contains(line, `"action":"handle `) || strings.Contains(line, `"action":"resend bridge info"`) ||
			entry.Message == "Sent new bridge state"
		if (!aborted && !background) || strings.Contains(strings.ToLower(entry.Message), "panic") {
			t.Errorf("unexpected log line after Close: %s", line)
		}
	}
	if len(lines) > 0 {
		t.Logf("%d lines logged after Close, starting with: %s", len(lines), lines[0])
	}
}

// rawCall sends a request, checks the response against the schema, and
// returns the result or the error.
func rawCall(t *testing.T, c *Core, command string, params any) (json.RawMessage, *api.CoreError) {
	t.Helper()
	req := map[string]any{"id": 7, "command": command}
	if params != nil {
		req["params"] = params
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	out := c.Call(data)
	if err = schema(t).ValidateResponse(command, out); err != nil {
		t.Fatalf("%s: the response breaks the contract: %v\n%s", command, err, out)
	}
	if command != api.CommandDebugPing {
		checkNoMatrixIDs(t, out, data)
	}
	var resp struct {
		ID     int64           `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *api.CoreError  `json:"error"`
	}
	if err = json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 7 {
		t.Fatalf("%s: response ID %d, want 7", command, resp.ID)
	}
	return resp.Result, resp.Error
}

// call runs a command that must succeed and decodes its result.
func call[R any](t *testing.T, c *Core, command string, params any) R {
	t.Helper()
	raw, coreErr := rawCall(t, c, command, params)
	if coreErr != nil {
		t.Fatalf("%s: %v", command, coreErr)
	}
	var result R
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// callFails runs a command that must fail with the given code.
func callFails(t *testing.T, c *Core, command string, params any, code api.ErrorCode) {
	t.Helper()
	if _, coreErr := rawCall(t, c, command, params); coreErr == nil || coreErr.Code != code {
		t.Errorf("%s %v: error %v, want code %s", command, params, coreErr, code)
	}
}

type rawEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// waitEvent reads events, each checked against the schema, until match
// returns true.
func waitEvent(t *testing.T, c *Core, what string, match func(rawEvent) bool) rawEvent {
	t.Helper()
	deadline := time.Now().Add(eventTimeout)
	for time.Now().Before(deadline) {
		data, ok := c.NextEvent(time.Until(deadline))
		if !ok {
			break
		}
		if err := schema(t).ValidateEvent(data); err != nil {
			t.Fatalf("an event breaks the contract: %v\n%s", err, data)
		}
		checkNoMatrixIDs(t, data, nil)
		var evt rawEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			t.Fatal(err)
		}
		if match(evt) {
			return evt
		}
	}
	t.Fatalf("no %s within %s", what, eventTimeout)
	return rawEvent{}
}

// checkNoMatrixIDs fails if a response or an event shows a Matrix
// identifier (apitest.MatrixIDs).
func checkNoMatrixIDs(t *testing.T, data, request []byte) {
	t.Helper()
	found, err := apitest.MatrixIDs(data, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range found {
		t.Errorf("a Matrix identifier reaches the API: %q in %s", v, data)
	}
}

func decode[T any](t *testing.T, data json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// messageEvent matches a message event of the given type.
func messageEvent(t *testing.T, typ string, match func(api.Message) bool) func(rawEvent) bool {
	return func(e rawEvent) bool {
		return e.Type == typ && match(decode[api.MessageEvent](t, e.Data).Message)
	}
}

// login adds the echo account "alice" and waits until its three
// conversations exist.
func login(t *testing.T, c *Core) string {
	t.Helper()
	return loginAs(t, c, "alice", conversationsPerLogin)
}

// loginAs adds an echo account and waits until the core has total
// conversations.
func loginAs(t *testing.T, c *Core, username string, total int) string {
	t.Helper()
	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username"})
	step = call[api.LoginStep](t, c, api.CommandLoginSubmit, api.LoginSubmitParams{
		ProcessID: step.ProcessID, Values: map[string]string{step.Fields[0].FieldID: username},
	})
	if step.Type != api.LoginStepTypeComplete || step.AccountID == "" {
		t.Fatalf("login ended with %+v", step)
	}
	waitConversations(t, c, total)
	return step.AccountID
}

// conversationsPerLogin is the number of conversations of an echo login:
// three contacts and a group.
const conversationsPerLogin = 4

// timeoutFor is how long n conversations may take to appear: a crowd login
// creates more than a hundred, which took over 15 s on CI's Windows runner
// under the race detector.
func timeoutFor(n int) time.Duration {
	return eventTimeout + time.Duration(n)*250*time.Millisecond
}

// listPollInterval spaces the conversation lists of a test that waits:
// each reads every conversation, its last message and its unread count,
// and listing a crowd of them in a loop slows bridgev2's own writes down
// on slow CI runners.
const listPollInterval = 100 * time.Millisecond

func waitConversations(t *testing.T, c *Core, n int) []api.Conversation {
	t.Helper()
	deadline := time.Now().Add(timeoutFor(n))
	for {
		convs := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations
		if len(convs) >= n {
			return convs
		}
		if time.Now().After(deadline) {
			t.Fatalf("conversations = %+v, want %d", convs, n)
		}
		time.Sleep(listPollInterval)
	}
}

func conversationNamed(t *testing.T, c *Core, name string) api.Conversation {
	t.Helper()
	for _, conv := range waitConversations(t, c, conversationsPerLogin) {
		if conv.Name == name {
			return conv
		}
	}
	t.Fatalf("no conversation named %q", name)
	return api.Conversation{}
}

func TestOpenRejectsInvalidConfig(t *testing.T) {
	dir := strings.ReplaceAll(t.TempDir(), `\`, `/`)
	for _, cfg := range []string{``, `{}`, `{"data_dir":`, `{"data_dir":"x","log_level":"loud"}`,
		`{"data_dir":"` + dir + `","telegram":{"api_id":1}}`, `{"data_dir":"` + dir + `","telegram":{"api_hash":"x"}}`} {
		if c, err := Open([]byte(cfg)); err == nil {
			_ = c.Close()
			t.Errorf("Open(%q) succeeded, want an error", cfg)
		}
	}
}

func TestHello(t *testing.T) {
	c := open(t)
	hello := call[api.HelloResult](t, c, api.CommandCoreHello, nil)
	if hello.APIVersion != api.APIVersion || hello.CoreVersion != Version {
		t.Errorf("hello = %+v", hello)
	}
}

func TestPing(t *testing.T) {
	c := open(t)
	if got := call[api.PingPayload](t, c, api.CommandDebugPing, api.PingPayload{Payload: "hi 👋"}); got.Payload != "hi 👋" {
		t.Errorf("ping payload = %q", got.Payload)
	}
}

// Every command of the schema is implemented: none answers unknown_command,
// even with missing params.
func TestEveryCommandIsImplemented(t *testing.T) {
	c := open(t)
	for _, spec := range api.Commands {
		if _, coreErr := rawCall(t, c, spec.Name, nil); coreErr != nil && coreErr.Code == api.ErrorCodeUnknownCommand {
			t.Errorf("%s is not implemented", spec.Name)
		}
	}
}

func TestInvalidRequests(t *testing.T) {
	c := open(t)
	out := c.Call([]byte(`{not json`))
	if err := schema(t).Validate("Response", out); err != nil || !strings.Contains(string(out), `"id":0,"error":{"code":"invalid_request"`) {
		t.Errorf("invalid JSON: %s (%v)", out, err)
	}
	out = c.Call([]byte(`{"id": 3}`))
	if !strings.Contains(string(out), `"id":3,"error":{"code":"invalid_request"`) {
		t.Errorf("missing command: %s", out)
	}
	callFails(t, c, "teleport", nil, api.ErrorCodeUnknownCommand)
	callFails(t, c, api.CommandMessagesSend, "not an object", api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: "c.bm9wZQ", Text: "x"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: "!room:musubee.local", Text: "x"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: "nope"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: "nope"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: "p.nope", Values: map[string]string{}}, api.ErrorCodeNotFound)

	conv := func() api.Conversation { login(t, c); return conversationNamed(t, c, "Instant Echo") }()
	callFails(t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesList, map[string]any{"conversation_id": conv.ConversationID, "limit": 500}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID, Before: "k.bad"}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID, Before: "c.x"}, api.ErrorCodeInvalidParams)
}

func TestNetworksList(t *testing.T) {
	c := open(t)
	networks := call[api.NetworksListResult](t, c, api.CommandNetworksList, nil).Networks
	if len(networks) != 2 || networks[0].NetworkID != "echo" || networks[0].Name != "Echo" || len(networks[0].LoginFlows) != 2 {
		t.Fatalf("networks = %+v", networks)
	}
	if m := networks[1]; m.NetworkID != "matrix" || m.Name != "Matrix" || len(m.LoginFlows) != 1 || m.LoginFlows[0].FlowID != "password" {
		t.Fatalf("matrix network = %+v", m)
	}
	// The password step asks for the server, the username and the password.
	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "matrix", FlowID: "password"})
	var fields []string
	for _, f := range step.Fields {
		fields = append(fields, f.FieldID+":"+string(f.Type))
	}
	if step.Type != api.LoginStepTypeUserInput || strings.Join(fields, ",") != "server:domain,username:username,password:password" {
		t.Fatalf("matrix login step = %+v", step)
	}
	call[api.Empty](t, c, api.CommandLoginCancel, api.LoginProcessParams{ProcessID: step.ProcessID})
}

// With application credentials, the core also offers Telegram, without its
// manual login flow. Nothing connects to Telegram before a login.
func TestTelegramIsOfferedWithCredentials(t *testing.T) {
	// Fake credentials: Telegram is never contacted here.
	cfg, err := json.Marshal(Config{DataDir: t.TempDir(), Telegram: &TelegramConfig{APIID: 1, APIHash: "0123456789abcdef0123456789abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	networks := call[api.NetworksListResult](t, c, api.CommandNetworksList, nil).Networks
	if len(networks) != 3 || networks[2].NetworkID != "telegram" || networks[2].Name != "Telegram" {
		t.Fatalf("networks = %+v", networks)
	}
	var flows []string
	for _, flow := range networks[2].LoginFlows {
		flows = append(flows, flow.FlowID)
	}
	if strings.Join(flows, ",") != "phone,qr,bot" {
		t.Errorf("telegram flows = %v", flows)
	}
	callFails(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "telegram", FlowID: "manual"}, api.ErrorCodeNotFound)
}

func TestLoginWithUsername(t *testing.T) {
	c := open(t)
	callFails(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "telegram", FlowID: "qr"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "qr"}, api.ErrorCodeNotFound)

	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username"})
	if step.Type != api.LoginStepTypeUserInput || len(step.Fields) != 1 || step.Fields[0].Type != api.LoginFieldTypeUsername || step.Fields[0].Pattern == "" {
		t.Fatalf("first step = %+v", step)
	}
	field := step.Fields[0].FieldID
	callFails(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{}}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{field: "Alice!"}}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandLoginWait, api.LoginProcessParams{ProcessID: step.ProcessID}, api.ErrorCodeInvalidParams)

	done := call[api.LoginStep](t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{field: "alice"}})
	if done.Type != api.LoginStepTypeComplete || done.AccountID == "" || done.ProcessID != step.ProcessID {
		t.Fatalf("last step = %+v", done)
	}
	// A finished process is forgotten.
	callFails(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{field: "alice"}}, api.ErrorCodeNotFound)

	waitEvent(t, c, "connected account", func(e rawEvent) bool {
		a := decode[api.AccountEvent](t, e.Data).Account
		return e.Type == api.EventAccountUpdated && a.AccountID == done.AccountID && a.State == api.AccountStateConnected
	})
	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts
	if len(accounts) != 1 || accounts[0].AccountID != done.AccountID || accounts[0].Name != "alice" || accounts[0].NetworkID != "echo" || accounts[0].State != api.AccountStateConnected {
		t.Errorf("accounts = %+v", accounts)
	}
	convs := waitConversations(t, c, conversationsPerLogin)
	groups := 0
	for _, conv := range convs {
		if conv.AccountID != done.AccountID || conv.NetworkID != "echo" || conv.Name == "" {
			t.Errorf("conversation %+v", conv)
		}
		if conv.Kind == api.ConversationKindGroup {
			groups++
			if conv.Name != "Echo Group" {
				t.Errorf("group %+v, want Echo Group", conv)
			}
		}
	}
	if groups != 1 {
		t.Errorf("%d groups, want 1", groups)
	}
	mine := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: done.AccountID}).Conversations
	others := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: accountID("echo", "bob")}).Conversations
	if len(mine) != conversationsPerLogin || len(others) != 0 {
		t.Errorf("filtered conversations: %d for alice, %d for bob; want %d and 0", len(mine), len(others), conversationsPerLogin)
	}
}

func TestConversationEvents(t *testing.T) {
	c := open(t)
	login(t, c)
	names := map[string]bool{}
	for len(names) < conversationsPerLogin {
		e := waitEvent(t, c, "conversation event", func(e rawEvent) bool { return e.Type == api.EventConversationUpdated })
		names[decode[api.ConversationEvent](t, e.Data).Conversation.Name] = true
	}
	if !names["Instant Echo"] || !names["Delayed Echo"] || !names["Unreachable Contact"] || !names["Echo Group"] {
		t.Errorf("conversation events for %v", names)
	}
}

// Closing the core right after a login: the login's connection, which
// starts in the background, is over once Close returns. Before, it could still
// use the login's bridge state (a data race with Close) and the database,
// whose file Windows then refused to delete.
func TestCloseRightAfterLogin(t *testing.T) {
	for i := range 10 {
		dir, err := os.MkdirTemp("", "musubee-close-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		cfg, _ := json.Marshal(Config{DataDir: dir})
		c, err := Open(cfg)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username"})
		call[api.LoginStep](t, c, api.CommandLoginSubmit, api.LoginSubmitParams{
			ProcessID: step.ProcessID, Values: map[string]string{step.Fields[0].FieldID: "closer"},
		})
		if err := c.Close(); err != nil {
			t.Fatalf("run %d: Close: %v", i, err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("run %d: the data directory is still in use after Close: %v", i, err)
		}
	}
}

func TestLoginWithCode(t *testing.T) {
	c := open(t)
	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "code"})
	if step.Type != api.LoginStepTypeDisplayAndWait || step.Display == nil || step.Display.Type != api.LoginDisplayTypeCode || step.Display.Data == "" || !step.Display.CanCancel {
		t.Fatalf("first step = %+v", step)
	}
	callFails(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{}}, api.ErrorCodeInvalidParams)
	done := call[api.LoginStep](t, c, api.CommandLoginWait, api.LoginProcessParams{ProcessID: step.ProcessID})
	if done.Type != api.LoginStepTypeComplete || done.AccountID != accountID("echo", networkid.UserLoginID("code"+step.Display.Data)) {
		t.Fatalf("last step = %+v", done)
	}
}

// login.cancel ends a login.wait that is running in another call.
func TestLoginCancelEndsWait(t *testing.T) {
	cfg, _ := json.Marshal(Config{DataDir: t.TempDir(), EchoDelayMS: int(time.Hour / time.Millisecond)})
	c, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "code"})
	waited := make(chan *api.CoreError, 1)
	go func() {
		out := c.Call(fmt.Appendf(nil, `{"id": 7, "command": "login.wait", "params": {"process_id": %q}}`, step.ProcessID))
		var resp struct {
			Error *api.CoreError `json:"error"`
		}
		_ = json.Unmarshal(out, &resp)
		waited <- resp.Error
	}()
	time.Sleep(100 * time.Millisecond)
	call[api.Empty](t, c, api.CommandLoginCancel, api.LoginProcessParams{ProcessID: step.ProcessID})
	select {
	case coreErr := <-waited:
		if coreErr == nil || coreErr.Code != api.ErrorCodeCancelled {
			t.Errorf("login.wait ended with %v, want cancelled", coreErr)
		}
	case <-time.After(eventTimeout):
		t.Fatal("login.cancel did not end login.wait")
	}
	// Cancelling again, or an unknown process, is not an error.
	call[api.Empty](t, c, api.CommandLoginCancel, api.LoginProcessParams{ProcessID: step.ProcessID})
	callFails(t, c, api.CommandLoginWait, api.LoginProcessParams{ProcessID: step.ProcessID}, api.ErrorCodeNotFound)
}

// Close ends a running login.wait.
func TestCloseEndsLoginWait(t *testing.T) {
	cfg, _ := json.Marshal(Config{DataDir: t.TempDir(), EchoDelayMS: int(time.Hour / time.Millisecond)})
	c, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "code"})
	waited := make(chan []byte, 1)
	go func() {
		waited <- c.Call(fmt.Appendf(nil, `{"id": 7, "command": "login.wait", "params": {"process_id": %q}}`, step.ProcessID))
	}()
	time.Sleep(100 * time.Millisecond)
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-waited:
		if err = schema(t).ValidateResponse(api.CommandLoginWait, out); err != nil || !strings.Contains(string(out), `"code":"closed"`) {
			t.Errorf("login.wait after Close: %s (%v)", out, err)
		}
	case <-time.After(eventTimeout):
		t.Fatal("Close did not end login.wait")
	}
}

// A request that Close overtakes answers closed, whatever error closing
// caused: Close forgets the login processes, so a login.wait that arrives
// while the core closes no longer finds its process (it answered not_found,
// a flake of the desktop app's crash test).
func TestRequestsOvertakenByCloseAnswerClosed(t *testing.T) {
	c := open(t)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		newError(api.ErrorCodeNotFound, "no login process"),
		errLoginCancelled,
		context.Canceled,
	} {
		if got := c.coreError(err); got.Code != api.ErrorCodeClosed {
			t.Errorf("coreError(%v) after Close = %s, want closed", err, got.Code)
		}
	}
}

func TestSendAndEcho(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "hello"}).Message
	if !sent.FromMe || sent.Text != "hello" || sent.Kind != api.MessageKindText || sent.ConversationID != conv.ConversationID ||
		(sent.Status != api.MessageStatusSending && sent.Status != api.MessageStatusSent) {
		t.Fatalf("sent message = %+v", sent)
	}
	waitEvent(t, c, "own message", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool {
		return m.MessageID == sent.MessageID && m.FromMe && m.Text == "hello"
	}))
	waitEvent(t, c, "delivery", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool {
		return m.MessageID == sent.MessageID && m.Status == api.MessageStatusSent
	}))
	echo := waitEvent(t, c, "echo", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe }))
	msg := decode[api.MessageEvent](t, echo.Data).Message
	if msg.ConversationID != conv.ConversationID || !strings.Contains(msg.Text, "hello") || msg.Status != api.MessageStatusReceived || msg.SenderName != "Instant Echo" {
		t.Errorf("echo = %+v", msg)
	}
}

func TestFailedSendIsAnEvent(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Unreachable Contact")
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "anyone?"}).Message
	failure := waitEvent(t, c, "failure", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool {
		return m.MessageID == sent.MessageID && m.Status != api.MessageStatusSending
	}))
	if msg := decode[api.MessageEvent](t, failure.Data).Message; msg.Status != api.MessageStatusFailed || msg.Error == "" {
		t.Errorf("failed message = %+v, want failed with an error", msg)
	}
}

func TestMessagesListPages(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Unreachable Contact")
	const n = 7
	for i := range n {
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: fmt.Sprint(i)})
	}
	var texts []string
	before := ""
	for pages := 0; ; pages++ {
		if pages > n {
			t.Fatal("paging does not end")
		}
		page := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID, Before: before, Limit: 3})
		if len(page.Messages) > 3 {
			t.Fatalf("page of %d messages, limit 3", len(page.Messages))
		}
		var pageTexts []string
		for _, m := range page.Messages {
			pageTexts = append(pageTexts, m.Text)
		}
		texts = append(pageTexts, texts...)
		if page.Before == "" {
			break
		}
		before = page.Before
	}
	if got := strings.Join(texts, ","); got != "0,1,2,3,4,5,6" {
		t.Errorf("messages, oldest first = %s", got)
	}
	latest := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID})
	if len(latest.Messages) != n || latest.Before != "" || latest.Messages[n-1].Text != "6" {
		t.Errorf("default page = %d messages, before %q", len(latest.Messages), latest.Before)
	}
}

func TestConversationsByActivity(t *testing.T) {
	c := open(t)
	alice := login(t, c)
	loginAs(t, c, "bob", 2*conversationsPerLogin)
	all := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil)
	if len(all.Conversations) != 2*conversationsPerLogin || all.Before != "" {
		t.Fatalf("all conversations = %d, before %q; want %d and no cursor", len(all.Conversations), all.Before, 2*conversationsPerLogin)
	}
	for i, conv := range all.Conversations {
		if conv.ActivityMS == 0 {
			t.Errorf("%s has no activity", conv.Name)
		}
		if i > 0 && conv.ActivityMS > all.Conversations[i-1].ActivityMS {
			t.Errorf("%s (%d) is after a less active conversation (%d)", conv.Name, conv.ActivityMS, all.Conversations[i-1].ActivityMS)
		}
	}

	// A message makes its conversation the most recent one, and its last.
	var target api.Conversation
	for _, conv := range all.Conversations {
		if conv.AccountID == alice && conv.Name == "Unreachable Contact" {
			target = conv
		}
	}
	if target.LastMessage != nil {
		t.Errorf("a new conversation has the last message %+v", target.LastMessage)
	}
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: target.ConversationID, Text: "latest"}).Message
	all = call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil)
	first := all.Conversations[0]
	if first.ConversationID != target.ConversationID || first.LastMessage == nil || first.LastMessage.MessageID != sent.MessageID ||
		first.LastMessage.Text != "latest" || first.ActivityMS != sent.TimestampMS {
		t.Fatalf("first conversation = %+v, want %s with the message just sent", first, target.Name)
	}

	// Pages of 3 give the same conversations in the same order.
	var paged []string
	before := ""
	for pages := 0; ; pages++ {
		if pages > 2*conversationsPerLogin {
			t.Fatal("paging does not end")
		}
		page := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{Before: before, Limit: 3})
		if len(page.Conversations) > 3 {
			t.Fatalf("page of %d conversations, limit 3", len(page.Conversations))
		}
		for _, conv := range page.Conversations {
			paged = append(paged, conv.ConversationID)
		}
		if page.Before == "" {
			break
		}
		before = page.Before
	}
	var want []string
	for _, conv := range all.Conversations {
		want = append(want, conv.ConversationID)
	}
	if !slices.Equal(paged, want) {
		t.Errorf("paged conversations = %v, want %v", paged, want)
	}

	// With an account, pages hold that account's conversations only.
	page := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: alice, Limit: 2})
	if len(page.Conversations) == 0 || page.Before == "" {
		t.Fatalf("first page of alice = %+v, want conversations and a cursor", page)
	}
	for _, conv := range page.Conversations {
		if conv.AccountID != alice {
			t.Errorf("page of alice holds %s of another account", conv.Name)
		}
	}

	callFails(t, c, api.CommandConversationsList, map[string]any{"limit": 201}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandConversationsList, api.ConversationsListParams{Before: "nonsense"}, api.ErrorCodeInvalidParams)
	// A cursor of messages is not one of conversations.
	call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: target.ConversationID, Text: "again"})
	messages := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: target.ConversationID, Limit: 1})
	callFails(t, c, api.CommandConversationsList, api.ConversationsListParams{Before: messages.Before}, api.ErrorCodeInvalidParams)
}

func TestGroupEcho(t *testing.T) {
	c := open(t)
	login(t, c)
	group := conversationNamed(t, c, "Echo Group")
	call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: group.ConversationID, Text: "hi all"})
	echo := waitEvent(t, c, "echo in the group", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool {
		return !m.FromMe && m.ConversationID == group.ConversationID
	}))
	if msg := decode[api.MessageEvent](t, echo.Data).Message; msg.SenderName != "Instant Echo" || !strings.Contains(msg.Text, "hi all") {
		t.Errorf("echo in the group = %+v, want Instant Echo's", msg)
	}
}

// A crowd login has CrowdSize more contacts, each with one message, the
// last contact most recently: the conversations come by activity, not by
// name. (The other conversations, created after these messages, come
// first.)
func TestCrowdIsOrderedByActivity(t *testing.T) {
	c := open(t)
	loginAs(t, c, "crowd", conversationsPerLogin+echo.CrowdSize)
	deadline := time.Now().Add(timeoutFor(echo.CrowdSize))
	var crowd []api.Conversation
	for {
		crowd = crowd[:0]
		for _, conv := range call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations {
			if strings.HasPrefix(conv.Name, "Crowd Contact") && conv.LastMessage != nil {
				crowd = append(crowd, conv)
			}
		}
		if len(crowd) == echo.CrowdSize {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d crowd contacts with a message, want %d", len(crowd), echo.CrowdSize)
		}
		time.Sleep(listPollInterval)
	}
	for i, conv := range crowd {
		want := fmt.Sprintf("Crowd Contact %03d", echo.CrowdSize-i)
		if conv.Name != want || conv.LastMessage.Text != "Hello from "+want || conv.ActivityMS != conv.LastMessage.TimestampMS {
			t.Fatalf("crowd conversation %d = %+v, want %s with its message", i, conv, want)
		}
	}
	if gap := crowd[0].ActivityMS - crowd[1].ActivityMS; gap != 60_000 {
		t.Errorf("activities %d ms apart, want a minute", gap)
	}
}

// An offline login connects once, then stays disconnected; its
// conversations and messages are still listed from the device.
func TestOfflineLoginKeepsItsConversations(t *testing.T) {
	dir := t.TempDir()
	c := openDir(t, dir)
	account := loginAs(t, c, "offline", conversationsPerLogin)
	conv := conversationNamed(t, c, "Instant Echo")
	// An echo in every conversation that echoes tells that bridgev2 is done
	// creating them: a creation that Close interrupts is only completed when
	// the account connects again, which an offline account never does.
	group := conversationNamed(t, c, "Echo Group")
	for _, id := range []string{group.ConversationID, conv.ConversationID} {
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: id, Text: "before"})
		waitEvent(t, c, "echo", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.ConversationID == id }))
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	c = openDir(t, dir)
	waitEvent(t, c, "offline state", func(e rawEvent) bool {
		a := decode[api.AccountEvent](t, e.Data).Account
		return e.Type == api.EventAccountUpdated && a.AccountID == account && a.State == api.AccountStateTransientDisconnect
	})
	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts
	if len(accounts) != 1 || accounts[0].State != api.AccountStateTransientDisconnect || accounts[0].Error == "" {
		t.Errorf("accounts while offline = %+v", accounts)
	}
	convs := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations
	if len(convs) != conversationsPerLogin || convs[0].ConversationID != conv.ConversationID || convs[0].LastMessage == nil || convs[0].LastMessage.FromMe {
		t.Fatalf("conversations while offline = %+v, want Instant Echo first with its echo", convs)
	}
	messages := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID}).Messages
	if len(messages) != 2 || messages[0].Text != "before" {
		t.Errorf("messages while offline = %+v", messages)
	}
	// Sending fails: there is no network.
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "now"}).Message
	failure := waitEvent(t, c, "failure", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool {
		return m.MessageID == sent.MessageID && m.Status != api.MessageStatusSending
	}))
	if msg := decode[api.MessageEvent](t, failure.Data).Message; msg.Status != api.MessageStatusFailed {
		t.Errorf("message sent offline = %+v, want failed", msg)
	}
}

func TestStats(t *testing.T) {
	c := open(t)
	if s := call[api.StatsResult](t, c, api.CommandDebugStats, nil); s.HeapAllocBytes == 0 || s.Goroutines == 0 {
		t.Errorf("stats = %+v", s)
	}
}

func TestCloseEndsTheEventStream(t *testing.T) {
	c := open(t)
	login(t, c)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	waitEvent(t, c, "closed event", func(e rawEvent) bool { return e.Type == api.EventCoreClosed })
	if data, ok := c.NextEvent(time.Millisecond); !ok || string(data) != `{"type":"core.closed","data":{}}` {
		t.Errorf("event after the end = %s, %v; want core.closed again", data, ok)
	}
	callFails(t, c, api.CommandAccountsList, nil, api.ErrorCodeClosed)
}

// TestReopenKeepsTheLogin opens a core twice on the same directory, as the
// application does on every launch.
func TestReopenKeepsTheLogin(t *testing.T) {
	dir := t.TempDir()
	c := openDir(t, dir)
	account := login(t, c)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c = openDir(t, dir)
	waitEvent(t, c, "reconnection", func(e rawEvent) bool {
		a := decode[api.AccountEvent](t, e.Data).Account
		return e.Type == api.EventAccountUpdated && a.AccountID == account && a.State == api.AccountStateConnected
	})
	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts
	if len(accounts) != 1 || accounts[0].AccountID != account {
		t.Errorf("accounts after reopening = %+v", accounts)
	}
	// A portal whose creation the first Close interrupted is created again
	// when the account reconnects.
	waitConversations(t, c, conversationsPerLogin)
}

// Logging out removes the account and its conversations, reports the
// logged_out state, and survives a reopening.
func TestLogout(t *testing.T) {
	dir := t.TempDir()
	c := openDir(t, dir)
	account := login(t, c)
	call[api.Empty](t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: account})
	if accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts; len(accounts) != 0 {
		t.Errorf("accounts after logout = %+v", accounts)
	}
	if convs := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations; len(convs) != 0 {
		t.Errorf("conversations after logout = %+v", convs)
	}
	waitEvent(t, c, "logged_out state", func(e rawEvent) bool {
		a := decode[api.AccountEvent](t, e.Data).Account
		return e.Type == api.EventAccountUpdated && a.AccountID == account && a.State == api.AccountStateLoggedOut
	})
	callFails(t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: account}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: "nope"}, api.ErrorCodeNotFound)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c = openDir(t, dir)
	if accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts; len(accounts) != 0 {
		t.Errorf("accounts after reopening = %+v", accounts)
	}
	if convs := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations; len(convs) != 0 {
		t.Errorf("conversations after reopening = %+v", convs)
	}
}

// TestSlowReaderGetsResync checks that the core never blocks on an
// application that stops reading events.
func TestSlowReaderGetsResync(t *testing.T) {
	c := &Core{events: make(chan []byte, 4)}
	for i := range 10 {
		c.push(fmt.Appendf(nil, `{"n":%d}`, i))
	}
	var got []string
	for len(c.events) > 0 {
		got = append(got, string(<-c.events))
	}
	if len(got) != 4 || got[len(got)-1] != string(resyncEvent) {
		t.Errorf("queue = %v, want 4 events ending with resync.required", got)
	}
	if err := schema(t).ValidateEvent(resyncEvent); err != nil {
		t.Error(err)
	}
}

func TestIDsAreOpaqueAndRoundTrip(t *testing.T) {
	id := conversationID("!abc:musubee.local")
	if strings.Contains(id, "!") || strings.Contains(id, ":") {
		t.Errorf("conversation ID %q shows the Matrix room ID", id)
	}
	if room, err := parseConversationID(id); err != nil || room != "!abc:musubee.local" {
		t.Errorf("parseConversationID(%q) = %q, %v", id, room, err)
	}
	if _, err := parseConversationID(accountID("echo", "alice")); err == nil {
		t.Error("an account ID was accepted as a conversation ID")
	}
	network, login, err := parseAccountID(accountID("echo", "alice"))
	if err != nil || network != "echo" || login != "alice" {
		t.Errorf("account ID round trip: %q %q %v", network, login, err)
	}
}

// BenchmarkRoundTrip measures messages.send, then the echo read from the
// event stream, without the schema checks of the tests.
func BenchmarkRoundTrip(b *testing.B) {
	cfg, _ := json.Marshal(Config{DataDir: b.TempDir()})
	c, err := Open(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	start := c.Call([]byte(`{"id":1,"command":"login.start","params":{"network_id":"echo","flow_id":"username"}}`))
	var step struct {
		Result api.LoginStep `json:"result"`
	}
	_ = json.Unmarshal(start, &step)
	c.Call(fmt.Appendf(nil, `{"id":2,"command":"login.submit","params":{"process_id":%q,"values":{"username":"bench"}}}`, step.Result.ProcessID))
	var conv string
	for conv == "" {
		data, ok := c.NextEvent(eventTimeout)
		if !ok {
			b.Fatal("no conversation")
		}
		var evt struct {
			Type string `json:"type"`
			Data struct {
				Conversation api.Conversation `json:"conversation"`
			} `json:"data"`
		}
		_ = json.Unmarshal(data, &evt)
		if evt.Type == api.EventConversationUpdated && evt.Data.Conversation.Name == "Instant Echo" {
			conv = evt.Data.Conversation.ConversationID
		}
	}
	var send time.Duration
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		marker := fmt.Sprintf("bench-%d", i)
		t0 := time.Now()
		c.Call(fmt.Appendf(nil, `{"id":3,"command":"messages.send","params":{"conversation_id":%q,"text":%q}}`, conv, marker))
		send += time.Since(t0)
		for {
			data, ok := c.NextEvent(eventTimeout)
			if !ok {
				b.Fatal("no echo")
			}
			if strings.Contains(string(data), `"from_me":false`) && strings.Contains(string(data), marker) {
				break
			}
		}
	}
	b.ReportMetric(float64(send.Microseconds())/float64(b.N), "send-us/op")
}
