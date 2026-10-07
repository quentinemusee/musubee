// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package telegramtest

import (
	"strings"
	"testing"

	"github.com/gotd/td/tg"
)

func TestDescribeSentCode(t *testing.T) {
	withNext := &tg.AuthSentCode{Type: &tg.AuthSentCodeTypeSMS{Length: 5}}
	withNext.SetNextType(&tg.AuthCodeTypeCall{})
	withNext.SetTimeout(60)

	tests := []struct {
		name  string
		sent  tg.AuthSentCodeClass
		wants []string
	}{
		{name: "app", sent: &tg.AuthSentCode{Type: &tg.AuthSentCodeTypeApp{Length: 5}}, wants: []string{"5 digits", "another Telegram app"}},
		{name: "sms with fallback", sent: withNext, wants: []string{"by SMS", "phone call", "resend", "60 seconds"}},
		{name: "payment required", sent: &tg.AuthSentCodePaymentRequired{}, wants: []string{"payment"}},
		{name: "firebase", sent: &tg.AuthSentCode{Type: &tg.AuthSentCodeTypeFirebaseSMS{Length: 6}}, wants: []string{"Firebase", "official mobile apps"}},
		{name: "success", sent: &tg.AuthSentCodeSuccess{}, wants: []string{"without a code"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeSentCode(tt.sent)
			for _, want := range tt.wants {
				if !strings.Contains(got, want) {
					t.Errorf("DescribeSentCode() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}
