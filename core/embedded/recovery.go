// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"strings"

	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/matrix"
)

// Verifying a Matrix account's device with its recovery key (API 1.15,
// docs/ADR/0044). The recovery key goes from the request to the Matrix
// connector and nowhere else: never stored, never logged, never in an
// error message.

// deviceVerifier is implemented by network clients whose devices are
// verified: the Matrix connector.
type deviceVerifier interface {
	DeviceVerified() (verified, known bool)
	Verify(ctx context.Context, recoveryKey string) (backupRestored bool, err error)
}

// verification says whether an account's device is verified, or nothing
// for networks without verification and while unknown.
func verification(client any) api.DeviceVerification {
	v, ok := client.(deviceVerifier)
	if !ok {
		return ""
	}
	verified, known := v.DeviceVerified()
	switch {
	case !known:
		return ""
	case verified:
		return api.DeviceVerificationVerified
	default:
		return api.DeviceVerificationUnverified
	}
}

func (c *Core) accountsVerify(ctx context.Context, p api.AccountsVerifyParams) (api.AccountsVerifyResult, error) {
	network, loginID, err := parseAccountID(p.AccountID)
	if err != nil {
		return api.AccountsVerifyResult{}, err
	}
	br := c.host.Bridge(network)
	if br == nil || !c.hasNetwork(network) {
		return api.AccountsVerifyResult{}, newError(api.ErrorCodeNotFound, "no account %q", p.AccountID)
	}
	login := br.GetCachedUserLoginByID(loginID)
	if login == nil {
		return api.AccountsVerifyResult{}, newError(api.ErrorCodeNotFound, "no account %q", p.AccountID)
	}
	verifier, ok := login.Client.(deviceVerifier)
	if !ok {
		return api.AccountsVerifyResult{}, newError(api.ErrorCodeUnsupported, "this account's network has no device verification")
	}
	key := strings.TrimSpace(p.RecoveryKey)
	if key == "" {
		return api.AccountsVerifyResult{}, newError(api.ErrorCodeInvalidParams, "the recovery key is empty")
	}
	restored, err := verifier.Verify(ctx, key)
	switch {
	case errors.Is(err, matrix.ErrWrongRecoveryKey):
		return api.AccountsVerifyResult{}, newError(api.ErrorCodeInvalidParams, "this is not the recovery key of this account")
	case errors.Is(err, matrix.ErrNoRecovery):
		return api.AccountsVerifyResult{}, newError(api.ErrorCodeUnsupported, "this account has no secure backup set up: set it up in another Matrix app first")
	case err != nil:
		// The error names no key: the connector's errors carry none.
		return api.AccountsVerifyResult{}, newError(api.ErrorCodeNetwork, "could not verify this device: %v", err)
	}
	return api.AccountsVerifyResult{BackupRestored: restored}, nil
}

// interactiveVerifier is implemented by network clients that verify their
// device with another device (the Matrix connector, docs/ADR/0049).
type interactiveVerifier interface {
	StartVerification(ctx context.Context) error
	AcceptVerification(ctx context.Context) error
	ConfirmVerification(ctx context.Context) error
	CancelVerification(ctx context.Context) error
}

// onVerification passes a step of a Matrix account's verification to the
// interface (API 1.22). The emoji are no secret: both screens show them.
func (c *Core) onVerification(login networkid.UserLoginID, state matrix.VerificationState) {
	data := api.VerificationEvent{
		AccountID: accountID(matrixBridge, login),
		Step:      api.VerificationStep(state.Step),
		Reason:    state.Reason,
	}
	for _, e := range state.Emoji {
		data.Emoji = append(data.Emoji, api.VerificationEmoji{Emoji: e.Emoji, Name: e.Name})
	}
	c.pushEvent(&api.Event{Type: api.EventAccountVerification, Data: data})
}

func (c *Core) accountsVerification(ctx context.Context, p api.AccountsVerificationParams) (api.Empty, error) {
	network, loginID, err := parseAccountID(p.AccountID)
	if err != nil {
		return api.Empty{}, err
	}
	br := c.host.Bridge(network)
	if br == nil || !c.hasNetwork(network) {
		return api.Empty{}, newError(api.ErrorCodeNotFound, "no account %q", p.AccountID)
	}
	login := br.GetCachedUserLoginByID(loginID)
	if login == nil {
		return api.Empty{}, newError(api.ErrorCodeNotFound, "no account %q", p.AccountID)
	}
	verifier, ok := login.Client.(interactiveVerifier)
	if !ok {
		return api.Empty{}, newError(api.ErrorCodeUnsupported, "this account's network has no device verification")
	}
	switch p.Action {
	case api.VerificationActionStart:
		err = verifier.StartVerification(ctx)
	case api.VerificationActionAccept:
		err = verifier.AcceptVerification(ctx)
	case api.VerificationActionConfirm:
		err = verifier.ConfirmVerification(ctx)
	case api.VerificationActionCancel:
		err = verifier.CancelVerification(ctx)
	default:
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "unknown action %q", p.Action)
	}
	if errors.Is(err, matrix.ErrNoVerification) {
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "no verification at the step this action needs")
	} else if err != nil {
		return api.Empty{}, newError(api.ErrorCodeNetwork, "the verification failed: %v", err)
	}
	return api.Empty{}, nil
}
