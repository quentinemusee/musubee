// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package telegramtest

import (
	"fmt"

	"github.com/gotd/td/tg"
)

// DescribeSentCode explains, in plain words, how Telegram says it delivered
// a login code. It is used by the tgsession command to diagnose codes that
// never arrive.
func DescribeSentCode(sent tg.AuthSentCodeClass) string {
	switch s := sent.(type) {
	case *tg.AuthSentCode:
		text := describeCodeType(s.Type)
		if next, ok := s.GetNextType(); ok {
			text += fmt.Sprintf("; you can ask for another delivery method (%s) by typing \"resend\"", describeNextType(next))
		}
		if timeout, ok := s.GetTimeout(); ok {
			text += fmt.Sprintf(" after %d seconds", timeout)
		}
		return text
	case *tg.AuthSentCodeSuccess:
		return "Telegram logged in without a code"
	case *tg.AuthSentCodePaymentRequired:
		return "Telegram requires a payment before sending the code (paid sign-up verification for this number); it cannot be completed from this tool"
	default:
		return fmt.Sprintf("unknown answer from Telegram (%T)", sent)
	}
}

func describeCodeType(t tg.AuthSentCodeTypeClass) string {
	switch c := t.(type) {
	case *tg.AuthSentCodeTypeApp:
		return fmt.Sprintf("code of %d digits sent as a message in another Telegram app logged in to this account (on the test environment)", c.Length)
	case *tg.AuthSentCodeTypeSMS:
		return fmt.Sprintf("code of %d digits sent by SMS", c.Length)
	case *tg.AuthSentCodeTypeCall:
		return fmt.Sprintf("code of %d digits read out in a phone call", c.Length)
	case *tg.AuthSentCodeTypeFlashCall:
		return "code given by the number of an incoming call that hangs up immediately"
	case *tg.AuthSentCodeTypeMissedCall:
		return fmt.Sprintf("code made of the last %d digits of an incoming missed call", c.Length)
	case *tg.AuthSentCodeTypeEmailCode:
		return fmt.Sprintf("code of %d digits sent by email to %s", c.Length, c.EmailPattern)
	case *tg.AuthSentCodeTypeSetUpEmailRequired:
		return "Telegram requires setting up a login email first; it cannot be completed from this tool"
	case *tg.AuthSentCodeTypeFragmentSMS:
		return fmt.Sprintf("code of %d digits sent through Fragment (%s)", c.Length, c.URL)
	case *tg.AuthSentCodeTypeFirebaseSMS:
		return fmt.Sprintf("code of %d digits sent by SMS through Firebase (only official mobile apps can receive it)", c.Length)
	case *tg.AuthSentCodeTypeSMSWord:
		return "code sent by SMS as a word"
	case *tg.AuthSentCodeTypeSMSPhrase:
		return "code sent by SMS as a phrase"
	default:
		return fmt.Sprintf("code sent by an unknown method (%T)", t)
	}
}

func describeNextType(t tg.AuthCodeTypeClass) string {
	switch t.(type) {
	case *tg.AuthCodeTypeSMS:
		return "SMS"
	case *tg.AuthCodeTypeCall:
		return "phone call"
	case *tg.AuthCodeTypeFlashCall:
		return "flash call"
	case *tg.AuthCodeTypeMissedCall:
		return "missed call"
	case *tg.AuthCodeTypeFragmentSMS:
		return "Fragment"
	default:
		return fmt.Sprintf("%T", t)
	}
}
