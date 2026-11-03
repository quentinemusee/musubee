// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package matrixtest

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/crypto/verificationhelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
	"github.com/quentinemusee/musubee/infra/coretest"
)

// otherDevice is alice's other Matrix app: it accepts verification
// requests, and shows the emoji it compares.
type otherDevice struct {
	t      *testing.T
	helper *verificationhelper.VerificationHelper
	mu     sync.Mutex
	emoji  []string
	done   bool
}

func (o *otherDevice) VerificationRequested(ctx context.Context, txnID id.VerificationTransactionID, _ id.UserID, _ id.DeviceID) {
	if err := o.helper.AcceptVerification(ctx, txnID); err != nil {
		o.t.Errorf("the other device accepting: %v", err)
	}
}

func (o *otherDevice) VerificationReady(context.Context, id.VerificationTransactionID, id.DeviceID, bool, bool, *verificationhelper.QRCode) {
}

func (o *otherDevice) VerificationCancelled(_ context.Context, _ id.VerificationTransactionID, code event.VerificationCancelCode, reason string) {
	o.t.Errorf("the other device saw the verification cancelled: %s %s", code, reason)
}

func (o *otherDevice) VerificationDone(context.Context, id.VerificationTransactionID, event.VerificationMethod) {
	o.mu.Lock()
	o.done = true
	o.mu.Unlock()
}

func (o *otherDevice) ShowSAS(ctx context.Context, txnID id.VerificationTransactionID, emojis []rune, _ []string, _ []int) {
	o.mu.Lock()
	for _, r := range emojis {
		o.emoji = append(o.emoji, string(r))
	}
	o.mu.Unlock()
	if err := o.helper.ConfirmSAS(ctx, txnID); err != nil {
		o.t.Errorf("the other device confirming: %v", err)
	}
}

func startOtherDevice(t *testing.T, ctx context.Context, localpart, password string) *otherDevice {
	t.Helper()
	cli, err := env.Login(ctx, localpart, password)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "other-device.db"))
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := cryptohelper.NewCryptoHelper(cli, []byte("alice's other device"), db)
	if err != nil {
		t.Fatal(err)
	}
	if err = crypto.Init(ctx); err != nil {
		t.Fatal(err)
	}
	cli.Crypto = crypto
	other := &otherDevice{t: t}
	other.helper = verificationhelper.NewVerificationHelper(cli, crypto.Machine(), nil, other, false, false, true)
	if err = other.helper.Init(ctx); err != nil {
		t.Fatal(err)
	}
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
		_ = crypto.Close()
	})
	return other
}

// TestMatrixInteractiveVerification verifies the core's device with
// alice's other device by comparing emoji (T2.43, docs/ADR/0049): both
// show the same seven, both confirm, and the core's device is verified.
func TestMatrixInteractiveVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	run := coretest.RandomHex(t)
	aliceLocal, alicePassword := "alice-v-"+run, "alice password "+run
	if _, err := env.RegisterUser(ctx, aliceLocal, alicePassword, false); err != nil {
		t.Fatal(err)
	}
	other := startOtherDevice(t, ctx, aliceLocal, alicePassword)

	c := coretest.Open(t, embedded.Config{DataDir: t.TempDir(), LogLevel: "debug"})
	events := coretest.Pump(t, c)
	step := startLogin(t, c)
	coretest.Call(t, c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: map[string]string{
		"server": env.HomeserverURL, "username": aliceLocal, "password": alicePassword,
	}}, &step)
	account := step.AccountID
	waitConnected(t, ctx, events, account)

	coretest.Call(t, c, api.CommandAccountsVerification, api.AccountsVerificationParams{AccountID: account, Action: api.VerificationActionStart}, nil)
	stepOf := func(want api.VerificationStep) api.VerificationEvent {
		t.Helper()
		e := events.Wait(t, ctx, "verification "+string(want), time.Minute, func(e coretest.Event) bool {
			var data api.VerificationEvent
			return e.Type == api.EventAccountVerification && e.Decode(t, &data) && data.AccountID == account && data.Step == want
		})
		var data api.VerificationEvent
		e.Decode(t, &data)
		return data
	}
	shown := stepOf(api.VerificationStepEmoji)
	var ours []string
	for _, e := range shown.Emoji {
		ours = append(ours, e.Emoji)
		if e.Name == "" {
			t.Errorf("emoji %q has no name", e.Emoji)
		}
	}
	deadline := time.Now().Add(time.Minute)
	for {
		other.mu.Lock()
		theirs := slices.Clone(other.emoji)
		other.mu.Unlock()
		if len(theirs) > 0 {
			if !slices.Equal(ours, theirs) || len(ours) != 7 {
				t.Fatalf("emoji: ours %q, the other device's %q", ours, theirs)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the other device showed no emoji")
		}
		time.Sleep(200 * time.Millisecond)
	}
	coretest.Call(t, c, api.CommandAccountsVerification, api.AccountsVerificationParams{AccountID: account, Action: api.VerificationActionConfirm}, nil)
	stepOf(api.VerificationStepDone)

	var accounts api.AccountsListResult
	coretest.Call(t, c, api.CommandAccountsList, nil, &accounts)
	if v := accounts.Accounts[0].Verification; v != api.DeviceVerificationVerified {
		t.Errorf("verification after = %q, want verified", v)
	}
	// Nothing left to confirm.
	if err := coretest.TryCall(t, c, api.CommandAccountsVerification, api.AccountsVerificationParams{AccountID: account, Action: api.VerificationActionConfirm}, nil); err == nil || err.Code != api.ErrorCodeInvalidParams {
		t.Errorf("confirming again: %+v", err)
	}
}
