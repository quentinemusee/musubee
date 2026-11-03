// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"testing"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/quentinemusee/musubee/core/api"
)

// matrixClient answers RoomEncrypted like the Matrix connector; the rest
// of bridgev2.NetworkAPI is never called.
type matrixClient struct {
	bridgev2.NetworkAPI
	encrypted bool
	err       error
}

func (m matrixClient) RoomEncrypted(context.Context, *bridgev2.Portal) (bool, error) {
	return m.encrypted, m.err
}

func TestPrivacy(t *testing.T) {
	ctx := t.Context()
	portal := &bridgev2.Portal{}
	of := func(network networkid.BridgeID, login *bridgev2.UserLogin) api.ConversationPrivacy {
		return *privacy(ctx, &bridgev2.Bridge{ID: network}, portal, login)
	}
	for _, tc := range []struct {
		what  string
		got   api.ConversationPrivacy
		level api.PrivacyLevel
		who   string
	}{
		{"echo", of(echoBridge, nil), api.PrivacyLevelDeviceOnly, ""},
		{"Signal", of(signalBridge, nil), api.PrivacyLevelEndToEnd, ""},
		{"Telegram", of(telegramBridge, nil), api.PrivacyLevelNetworkCanRead, "Telegram"},
		{"an encrypted Matrix room", of(matrixBridge, &bridgev2.UserLogin{Client: matrixClient{encrypted: true}}), api.PrivacyLevelEndToEnd, ""},
		{"a Matrix room in clear", of(matrixBridge, &bridgev2.UserLogin{Client: matrixClient{}}), api.PrivacyLevelNetworkCanRead, matrixReaders},
		// What the core cannot tell is unknown, never safe.
		{"a Matrix room whose state fails", of(matrixBridge, &bridgev2.UserLogin{Client: matrixClient{encrypted: true, err: errors.New("no state")}}), api.PrivacyLevelUnknown, ""},
		{"a Matrix room without its login", of(matrixBridge, nil), api.PrivacyLevelUnknown, ""},
		{"another network", of("whatsapp", nil), api.PrivacyLevelUnknown, ""},
	} {
		if tc.got.Level != tc.level || tc.got.Readers != tc.who {
			t.Errorf("%s: privacy = %+v, want %s %q", tc.what, tc.got, tc.level, tc.who)
		}
	}
}

// TestPrivacyInTheAPI reads the echo network's conversations and account:
// on the device, and nothing leaves it.
func TestPrivacyInTheAPI(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	if conv.Privacy == nil || conv.Privacy.Level != api.PrivacyLevelDeviceOnly {
		t.Errorf("privacy = %+v, want device_only", conv.Privacy)
	}
	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, api.Empty{})
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Connection != api.AccountConnectionOnDevice {
		t.Errorf("accounts = %+v, want one on the device", accounts.Accounts)
	}
}
