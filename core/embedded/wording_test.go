// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"encoding/json"
	"slices"
	"testing"

	tgconnector "go.mau.fi/mautrix-telegram/pkg/connector"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/telegram"
)

// Every network says what it brings, and Telegram's flows and first step
// come in the core's words, with the bot flow set apart.
func TestTelegramLoginInPlainWords(t *testing.T) {
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
	for _, n := range networks {
		if n.Description == "" {
			t.Errorf("network %s has no description", n.NetworkID)
		}
	}
	tg := networks[2]
	type flow struct {
		id, name string
		advanced bool
	}
	var flows []flow
	for _, f := range tg.LoginFlows {
		flows = append(flows, flow{f.FlowID, f.Name, f.Advanced})
	}
	want := []flow{{"phone", "With your phone number", false}, {"qr", "By scanning a QR code", false}, {"bot", "As a bot", true}}
	if !slices.Equal(flows, want) {
		t.Errorf("telegram flows = %+v, want %+v", flows, want)
	}

	step := call[api.LoginStep](t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "telegram", FlowID: "phone"})
	if len(step.Fields) != 1 || step.Fields[0].FieldID != tgconnector.LoginStepIDPhoneNumber || step.Fields[0].Description != "With your country's code, starting with +." {
		t.Errorf("phone number step = %+v", step)
	}
	call[api.Empty](t, c, api.CommandLoginCancel, api.LoginProcessParams{ProcessID: step.ProcessID})
}

// The table names only flows that the connectors offer: a flow that a
// connector renamed would keep its own wording without anyone noticing.
func TestWordingFollowsTheConnectors(t *testing.T) {
	tc, err := telegram.New(telegram.Config{APIID: 1, APIHash: "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	var offered []string
	for _, f := range tc.GetLoginFlows() {
		offered = append(offered, f.ID)
	}
	for id := range wordings[telegramBridge].flows {
		if !slices.Contains(offered, id) {
			t.Errorf("the wording names Telegram's flow %q, which is not offered (%v)", id, offered)
		}
	}
}

// A step keeps the connector's words where the table has none, and the
// connector's fields are not changed in place: mautrix-telegram shares some
// steps between logins.
func TestWordingOfAStep(t *testing.T) {
	w := wordings[telegramBridge]
	qr := w.step(api.LoginStep{StepID: tgconnector.LoginStepIDShowQR, Instructions: "Scan the QR code on your phone to log in"})
	if qr.Instructions != "On your phone, open Telegram, go to Settings, then Devices, tap Link Desktop Device (or Scan QR Code), and scan this code." {
		t.Errorf("QR code step = %q", qr.Instructions)
	}
	unknown := w.step(api.LoginStep{StepID: "fi.mau.telegram.login.new", Instructions: "Upstream words"})
	if unknown.Instructions != "Upstream words" {
		t.Errorf("unknown step = %q", unknown.Instructions)
	}
	fields := []api.LoginField{{FieldID: tgconnector.LoginStepIDCode, Name: "Code", Description: "Upstream"}, {FieldID: "other", Name: "Other"}}
	code := w.step(api.LoginStep{StepID: tgconnector.LoginStepIDCode, Fields: fields})
	if code.Fields[0].Description == "Upstream" || code.Fields[1].Name != "Other" {
		t.Errorf("code step fields = %+v", code.Fields)
	}
	if fields[0].Description != "Upstream" {
		t.Error("the connector's fields were changed in place")
	}
	// A network without wording keeps everything.
	echo := wordings[echoBridge].step(api.LoginStep{StepID: "s", Instructions: "i"})
	if echo.Instructions != "i" {
		t.Errorf("echo step = %q", echo.Instructions)
	}
}
