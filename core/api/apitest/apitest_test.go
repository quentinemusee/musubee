// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package apitest

import (
	"slices"
	"testing"
)

func TestMatrixIDs(t *testing.T) {
	for _, tc := range []struct {
		document, request string
		want              []string
	}{
		{`{"conversation_id":"c.IWFiYzptdXN1YmVlLmxvY2Fs","name":"Alice"}`, "", nil},
		{`{"message":{"text":"hello @alice, see you at 10:30"}}`, "", nil},
		{`{"room":"!abc:example.org"}`, "", []string{"!abc:example.org"}},
		{`{"sender":"@alice:example.org"}`, "", []string{"@alice:example.org"}},
		{`{"alias":"#room:example.org"}`, "", []string{"#room:example.org"}},
		{`{"event_id":"$abcdef"}`, "", []string{"$abcdef"}},
		{`{"avatar":"mxc://example.org/abc"}`, "", []string{"mxc://example.org/abc"}},
		{`{"error":{"message":"room x on musubee.local"}}`, "", []string{"room x on musubee.local"}},
		{`{"!abc:example.org":1}`, "", []string{"!abc:example.org"}},
		// What the request said may be quoted back.
		{`{"error":{"message":"no conversation \"!room:musubee.local\""}}`, `{"params":{"conversation_id":"!room:musubee.local"}}`, nil},
		{`{"error":{"message":"no conversation \"!room:musubee.local\""}}`, `{"params":{"text":"x"}}`, []string{`no conversation "!room:musubee.local"`}},
	} {
		var request []byte
		if tc.request != "" {
			request = []byte(tc.request)
		}
		got, err := MatrixIDs([]byte(tc.document), request)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("MatrixIDs(%s, %s) = %q, %v; want %q", tc.document, tc.request, got, err, tc.want)
		}
	}
}
