// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestVerifyNeedsAMatrixAccount: networks without device verification say
// so, and their accounts carry no verification (API 1.15). Matrix accounts
// are verified in the Synapse journey (infra/matrixtest).
func TestVerifyNeedsAMatrixAccount(t *testing.T) {
	c := open(t)
	account := login(t, c)
	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, api.Empty{})
	if v := accounts.Accounts[0].Verification; v != "" {
		t.Errorf("echo account verification = %q, want none", v)
	}
	callFails(t, c, api.CommandAccountsVerify, api.AccountsVerifyParams{AccountID: account, RecoveryKey: "EsTc 1234"}, api.ErrorCodeUnsupported)
	callFails(t, c, api.CommandAccountsVerify, api.AccountsVerifyParams{AccountID: "a.nope", RecoveryKey: "EsTc 1234"}, api.ErrorCodeNotFound)
}
