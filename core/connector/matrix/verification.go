// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"context"
	"errors"
	"sync"

	"maunium.net/go/mautrix/crypto/verificationhelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Interactive verification of the device with another device of the same
// account, by comparing emoji (Matrix's SAS method, docs/ADR/0049), with
// mautrix-go's verificationhelper. One verification at a time per account;
// each step is reported through Config.OnVerification.

// VerificationStep is where a verification is.
type VerificationStep string

const (
	// VerificationIncoming: another device asks to verify; Accept or Cancel.
	VerificationIncoming VerificationStep = "incoming"
	// VerificationWaiting: Musubee asked; waiting for another device.
	VerificationWaiting VerificationStep = "waiting"
	// VerificationEmoji: compare the emoji; Confirm when they match, else Cancel.
	VerificationEmoji VerificationStep = "emoji"
	// VerificationConfirmed: confirmed here; waiting for the other device.
	VerificationConfirmed VerificationStep = "confirmed"
	// VerificationDone: the device is verified.
	VerificationDone VerificationStep = "done"
	// VerificationCancelled: ended without verifying (Reason says why).
	VerificationCancelled VerificationStep = "cancelled"
)

// Emoji is one emoji to compare, and its name.
type Emoji struct {
	Emoji, Name string
}

// VerificationState is what a verification shows.
type VerificationState struct {
	Step   VerificationStep
	Emoji  []Emoji
	Reason string
}

// ErrNoVerification: there is no verification to act on.
var ErrNoVerification = errors.New("no verification in progress")

// verification is the account's verification in progress.
type verification struct {
	mu     sync.Mutex
	helper *verificationhelper.VerificationHelper
	txnID  id.VerificationTransactionID
	state  VerificationState
}

// startVerifier makes the verification helper once the encryption is up.
func (c *client) startVerifier(ctx context.Context) error {
	helper := verificationhelper.NewVerificationHelper(c.cli, c.crypto.Machine(), nil, verificationCallbacks{c}, false, false, true)
	if err := helper.Init(ctx); err != nil {
		return err
	}
	c.verification.mu.Lock()
	c.verification.helper = helper
	c.verification.mu.Unlock()
	return nil
}

// report records a verification's step and tells the core.
func (c *client) report(txnID id.VerificationTransactionID, state VerificationState) {
	c.verification.mu.Lock()
	c.verification.txnID, c.verification.state = txnID, state
	c.verification.mu.Unlock()
	if state.Step == VerificationDone {
		c.recovery.mu.Lock()
		c.recovery.checked, c.recovery.verified = true, true
		c.recovery.mu.Unlock()
	}
	if c.connector.Config.OnVerification != nil {
		c.connector.Config.OnVerification(c.login.ID, state)
	}
}

func (c *client) current() (*verificationhelper.VerificationHelper, id.VerificationTransactionID, VerificationStep) {
	c.verification.mu.Lock()
	defer c.verification.mu.Unlock()
	return c.verification.helper, c.verification.txnID, c.verification.state.Step
}

// StartVerification asks the account's other devices to verify this one.
func (c *client) StartVerification(ctx context.Context) error {
	if err := c.isReady(); err != nil {
		return err
	}
	helper, txnID, step := c.current()
	if helper == nil {
		return errNotReady
	}
	if txnID != "" && (step == VerificationIncoming || step == VerificationWaiting || step == VerificationEmoji || step == VerificationConfirmed) {
		_ = helper.CancelVerification(ctx, txnID, event.VerificationCancelCodeUser, "a new verification started")
	}
	newTxn, err := helper.StartVerification(ctx, c.self)
	if err != nil {
		return err
	}
	c.report(newTxn, VerificationState{Step: VerificationWaiting})
	return nil
}

// AcceptVerification accepts another device's request.
func (c *client) AcceptVerification(ctx context.Context) error {
	helper, txnID, step := c.current()
	if helper == nil || step != VerificationIncoming {
		return ErrNoVerification
	}
	return helper.AcceptVerification(ctx, txnID)
}

// ConfirmVerification says that the emoji match.
func (c *client) ConfirmVerification(ctx context.Context) error {
	helper, txnID, step := c.current()
	if helper == nil || step != VerificationEmoji {
		return ErrNoVerification
	}
	if err := helper.ConfirmSAS(ctx, txnID); err != nil {
		return err
	}
	c.report(txnID, VerificationState{Step: VerificationConfirmed})
	return nil
}

// CancelVerification ends the verification in progress: the emoji did not
// match, or the user gave up.
func (c *client) CancelVerification(ctx context.Context) error {
	helper, txnID, step := c.current()
	if helper == nil || txnID == "" || step == VerificationDone || step == VerificationCancelled {
		return ErrNoVerification
	}
	return helper.CancelVerification(ctx, txnID, event.VerificationCancelCodeUser, "cancelled by the user")
}

// verificationCallbacks receives the helper's steps.
type verificationCallbacks struct{ c *client }

// VerificationRequested: only the account's own devices may verify this
// one; requests from other users are cancelled.
func (v verificationCallbacks) VerificationRequested(ctx context.Context, txnID id.VerificationTransactionID, from id.UserID, _ id.DeviceID) {
	if from != v.c.self {
		if helper, _, _ := v.c.current(); helper != nil {
			_ = helper.CancelVerification(ctx, txnID, event.VerificationCancelCodeUser, "Musubee verifies its own devices only")
		}
		return
	}
	v.c.report(txnID, VerificationState{Step: VerificationIncoming})
}

// VerificationReady: both devices are ready; the emoji comparison starts
// (whichever side starts it first wins, as the specification allows).
func (v verificationCallbacks) VerificationReady(ctx context.Context, txnID id.VerificationTransactionID, _ id.DeviceID, supportsSAS, _ bool, _ *verificationhelper.QRCode) {
	helper, _, _ := v.c.current()
	if helper == nil {
		return
	}
	if !supportsSAS {
		_ = helper.CancelVerification(ctx, txnID, event.VerificationCancelCodeUnknownMethod, "the other device does not compare emoji")
		return
	}
	v.c.report(txnID, VerificationState{Step: VerificationWaiting})
	_ = helper.StartSAS(ctx, txnID)
}

func (v verificationCallbacks) ShowSAS(_ context.Context, txnID id.VerificationTransactionID, emojis []rune, descriptions []string, _ []int) {
	state := VerificationState{Step: VerificationEmoji}
	for i, r := range emojis {
		name := ""
		if i < len(descriptions) {
			name = descriptions[i]
		}
		state.Emoji = append(state.Emoji, Emoji{Emoji: string(r), Name: name})
	}
	v.c.report(txnID, state)
}

func (v verificationCallbacks) VerificationCancelled(_ context.Context, txnID id.VerificationTransactionID, _ event.VerificationCancelCode, reason string) {
	v.c.report(txnID, VerificationState{Step: VerificationCancelled, Reason: reason})
}

func (v verificationCallbacks) VerificationDone(_ context.Context, txnID id.VerificationTransactionID, _ event.VerificationMethod) {
	v.c.report(txnID, VerificationState{Step: VerificationDone})
}
