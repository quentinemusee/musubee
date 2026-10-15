// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	tgconnector "go.mau.fi/mautrix-telegram/pkg/connector"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/quentinemusee/musubee/core/api"
)

// The words that the core gives the user interfaces for its networks' logins
// (docs/ADR/0032). The connectors of mautrix were written for Matrix users,
// who log in by talking to a bridge bot: their wording speaks of bridges, bot
// tokens and two-factor authentication, and does not say where to find a
// network's QR code scanner. The core replaces it here, keyed by the IDs of
// flows, steps and fields, which the connectors keep stable; an ID that a
// connector renames or adds keeps its own wording until this table follows.
// The interface itself knows no network: it shows these words as they come.

type networkWording struct {
	// description says what an account of the network brings.
	description string
	// flows, by flow ID.
	flows map[string]flowWording
	// instructions of the login steps, by step ID.
	steps map[string]string
	// fields of the login steps, by field ID.
	fields map[string]fieldWording
}

type flowWording struct {
	name, description string
	advanced          bool
}

type fieldWording struct {
	name, description string
}

// The QR code steps say where each phone app keeps its scanner, in the menu
// names of the apps' English versions in October 2026, which differ between
// Android and iOS (docs/ADR/0032).
var wordings = map[networkid.BridgeID]networkWording{
	echoBridge: {
		description: "Pretend contacts that answer you, to try Musubee without an account.",
	},
	matrixBridge: {
		description: "Your account on a Matrix server, such as matrix.org.",
	},
	telegramBridge: {
		description: "Your Telegram chats, groups and channels.",
		flows: map[string]flowWording{
			tgconnector.LoginFlowIDPhone: {
				name:        "With your phone number",
				description: "Telegram sends you a code to type here.",
			},
			tgconnector.LoginFlowIDQR: {
				name:        "By scanning a QR code",
				description: "With the Telegram app of your phone.",
			},
			tgconnector.LoginFlowIDBotToken: {
				name:        "As a bot",
				description: "For developers: with the token that BotFather gave you.",
				advanced:    true,
			},
		},
		steps: map[string]string{
			tgconnector.LoginStepIDCodeIncorrect:     "This code is not the right one. Type the latest code that Telegram sent you.",
			tgconnector.LoginStepIDPassword:          "Your Telegram account also has a password (Two-Step Verification). Type it to finish.",
			tgconnector.LoginStepIDPasswordIncorrect: "This password is not the right one. If you forgot it, you can reset it in the Telegram app: Settings, Privacy and Security, Two-Step Verification.",
			tgconnector.LoginStepIDShowQR:            "On your phone, open Telegram, go to Settings, then Devices, tap Link Desktop Device (or Scan QR Code), and scan this code.",
			tgconnector.LoginStepIDBotToken:          "Type the token of your bot, as BotFather gave it to you.",
		},
		fields: map[string]fieldWording{
			tgconnector.LoginStepIDPhoneNumber: {
				name:        "Phone number",
				description: "With your country's code, starting with +.",
			},
			tgconnector.LoginStepIDCode: {
				name:        "Code",
				description: "Telegram sent it to you: look in the Telegram app, on your phone or computer.",
			},
		},
	},
	signalBridge: {
		description: "Your Signal chats. Musubee becomes a linked device of the Signal app of your phone, as Signal Desktop does.",
		flows: map[string]flowWording{
			"qr": {
				name:        "By linking this device",
				description: "With the Signal app of your phone, which scans a QR code.",
			},
		},
		steps: map[string]string{
			// mautrix-signal's LoginStepQR, which a core built without
			// Signal does not link.
			"fi.mau.signal.login.qr": "On your phone, open Signal, go to Settings, then Linked devices, tap the button that links a new device, and scan this code.",
		},
	},
}

// network returns the network as the core shows it.
func (w networkWording) network(n api.Network) api.Network {
	if w.description != "" {
		n.Description = w.description
	}
	for i, flow := range n.LoginFlows {
		if fw, ok := w.flows[flow.FlowID]; ok {
			n.LoginFlows[i].Name = fw.name
			n.LoginFlows[i].Description = fw.description
			n.LoginFlows[i].Advanced = fw.advanced
		}
	}
	return n
}

// step returns the login step as the core shows it.
func (w networkWording) step(s api.LoginStep) api.LoginStep {
	if instructions, ok := w.steps[s.StepID]; ok {
		s.Instructions = instructions
	}
	if len(s.Fields) > 0 && len(w.fields) > 0 {
		fields := make([]api.LoginField, len(s.Fields))
		for i, f := range s.Fields {
			if fw, ok := w.fields[f.FieldID]; ok {
				f.Name = fw.name
				f.Description = fw.description
			}
			fields[i] = f
		}
		s.Fields = fields
	}
	return s
}
