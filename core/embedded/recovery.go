// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"strings"

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
