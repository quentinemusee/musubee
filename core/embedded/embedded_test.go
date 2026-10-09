// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/api/apitest"
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
	})
	return c
}

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
		aborted := strings.Contains(entry.Error, "context canceled") || strings.Contains(entry.Error, "database is closed")
		// Nested loggers repeat the "action" key, and json.Unmarshal keeps
		// only the last one: look for the handler's in the raw line.
		inHandler := strings.Contains(line, `"action":"handle `)
		if !(aborted || inHandler) || strings.Contains(strings.ToLower(entry.Message), "panic") {
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
	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username"})
	step = call[api.LoginStep](t, c, api.CommandLoginSubmit, api.LoginSubmitParams{
		ProcessID: step.ProcessID, Values: map[string]string{step.Fields[0].FieldID: "alice"},
	})
	if step.Type != api.LoginStepTypeComplete || step.AccountID == "" {
		t.Fatalf("login ended with %+v", step)
	}
	waitConversations(t, c, 3)
	return step.AccountID
}

func waitConversations(t *testing.T, c *Core, n int) []api.Conversation {
	t.Helper()
	deadline := time.Now().Add(eventTimeout)
	for {
		convs := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations
		if len(convs) >= n {
			return convs
		}
		if time.Now().After(deadline) {
			t.Fatalf("conversations = %+v, want %d", convs, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func conversationNamed(t *testing.T, c *Core, name string) api.Conversation {
	t.Helper()
	for _, conv := range waitConversations(t, c, 3) {
		if conv.Name == name {
			return conv
		}
	}
	t.Fatalf("no conversation named %q", name)
	return api.Conversation{}
}

func TestOpenRejectsInvalidConfig(t *testing.T) {
	for _, cfg := range []string{``, `{}`, `{"data_dir":`, `{"data_dir":"x","log_level":"loud"}`} {
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
	if len(networks) != 1 || networks[0].NetworkID != "echo" || networks[0].Name != "Echo" || len(networks[0].LoginFlows) != 2 {
		t.Fatalf("networks = %+v", networks)
	}
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
	convs := waitConversations(t, c, 3)
	for _, conv := range convs {
		if conv.AccountID != done.AccountID || conv.NetworkID != "echo" || conv.Kind != api.ConversationKindDirect || conv.Name == "" {
			t.Errorf("conversation %+v", conv)
		}
	}
	mine := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: done.AccountID}).Conversations
	others := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{AccountID: accountID("echo", "bob")}).Conversations
	if len(mine) != 3 || len(others) != 0 {
		t.Errorf("filtered conversations: %d for alice, %d for bob; want 3 and 0", len(mine), len(others))
	}
}

func TestConversationEvents(t *testing.T) {
	c := open(t)
	login(t, c)
	names := map[string]bool{}
	for len(names) < 3 {
		e := waitEvent(t, c, "conversation event", func(e rawEvent) bool { return e.Type == api.EventConversationUpdated })
		names[decode[api.ConversationEvent](t, e.Data).Conversation.Name] = true
	}
	if !names["Instant Echo"] || !names["Delayed Echo"] || !names["Unreachable Contact"] {
		t.Errorf("conversation events for %v", names)
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
		if err = schema(t).ValidateResponse(api.CommandLoginWait, out); err != nil || !strings.Contains(string(out), `"error"`) {
			t.Errorf("login.wait after Close: %s (%v)", out, err)
		}
	case <-time.After(eventTimeout):
		t.Fatal("Close did not end login.wait")
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
	waitConversations(t, c, 3)
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
