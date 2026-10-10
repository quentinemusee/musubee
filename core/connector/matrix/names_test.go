// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestPersonName(t *testing.T) {
	for _, tc := range []struct {
		user        id.UserID
		displayName string
		want        string
	}{
		{"@alice:example.org", "Alice", "Alice"},
		{"@alice:example.org", "  Alice  ", "Alice"},
		{"@alice:example.org", "", "alice"},
		{"@alice:example.org", "   ", "alice"},
		// A display name that is the identifier is no name.
		{"@alice:example.org", "@alice:example.org", "alice"},
		{"not-an-id", "", "not-an-id"},
	} {
		if got := personName(tc.user, tc.displayName); got != tc.want {
			t.Errorf("personName(%q, %q) = %q, want %q", tc.user, tc.displayName, got, tc.want)
		}
	}
}

func TestAccountName(t *testing.T) {
	if got := accountName("@alice:example.org", "Alice"); got != "Alice (example.org)" {
		t.Errorf("accountName = %q", got)
	}
	if got := accountName("@alice:example.org", ""); got != "alice (example.org)" {
		t.Errorf("accountName without a display name = %q", got)
	}
}

func TestRoomName(t *testing.T) {
	const self id.UserID = "@me:example.org"
	members := func(names ...string) []member {
		var out []member
		for i, name := range names {
			out = append(out, member{user: id.UserID("@u" + string(rune('a'+i)) + ":example.org"), displayName: name})
		}
		return out
	}
	withSelf := append(members("Bob"), member{user: self, displayName: "Me"})
	sortMembers(withSelf)
	for _, tc := range []struct {
		desc    string
		name    string
		members []member
		want    string
	}{
		{"named room", " Book club ", members("Bob", "Carol"), "Book club"},
		{"direct conversation", "", withSelf, "Bob"},
		{"no display name", "", members(""), "ua"},
		{"three others", "", members("Bob", "Carol", "Dan"), "Bob, Carol, Dan"},
		{"more than three", "", members("Bob", "Carol", "Dan", "Erin", "Frank"), "Bob, Carol, Dan and 2 others"},
		{"alone", "", []member{{user: self, displayName: "Me"}}, "Empty conversation"},
	} {
		if got := roomName(tc.name, self, tc.members); got != tc.want {
			t.Errorf("%s: roomName = %q, want %q", tc.desc, got, tc.want)
		}
	}
}

func TestSortMembers(t *testing.T) {
	m := []member{{user: "@c:x"}, {user: "@a:x"}, {user: "@b:x"}}
	sortMembers(m)
	if m[0].user != "@a:x" || m[1].user != "@b:x" || m[2].user != "@c:x" {
		t.Errorf("sorted = %v", m)
	}
}

func TestHomeserverURL(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"matrix.org", "https://matrix.org"},
		{" example.org:8448 ", "https://example.org:8448"},
		{"http://localhost:8008", "http://localhost:8008"},
		{"https://matrix.example.org/", "https://matrix.example.org/"},
	} {
		u, err := homeserverURL(tc.input)
		if err != nil || u.String() != tc.want {
			t.Errorf("homeserverURL(%q) = %v, %v; want %s", tc.input, u, err, tc.want)
		}
	}
	for _, bad := range []string{"", "  ", "ftp://example.org", "https://", "http://exa mple.org"} {
		if u, err := homeserverURL(bad); err == nil {
			t.Errorf("homeserverURL(%q) = %v, want an error", bad, u)
		}
	}
}

func TestIsTextMessage(t *testing.T) {
	for _, mt := range []event.MessageType{event.MsgText, event.MsgNotice, event.MsgEmote} {
		if !isTextMessage(mt) {
			t.Errorf("%s is text", mt)
		}
	}
	for _, mt := range []event.MessageType{event.MsgImage, event.MsgFile, event.MsgVideo, event.MsgAudio, event.MsgLocation, ""} {
		if isTextMessage(mt) {
			t.Errorf("%s is not text", mt)
		}
	}
}

func TestIncoming(t *testing.T) {
	reply := &event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          "> <@bob:x> earlier\n\nanswer",
		Format:        event.FormatHTML,
		FormattedBody: "<mx-reply><blockquote>earlier</blockquote></mx-reply>answer",
		RelatesTo:     (&event.RelatesTo{}).SetReplyTo("$earlier"),
		Mentions:      &event.Mentions{UserIDs: []id.UserID{"@bob:x"}},
	}
	out, replyTo := incoming(reply)
	if out.Body != "answer" || out.FormattedBody != "answer" || out.RelatesTo != nil || out.Mentions != nil {
		t.Errorf("reply converted to %+v", out)
	}
	if replyTo == nil || replyTo.MessageID != "$earlier" {
		t.Errorf("replyTo = %+v", replyTo)
	}
	if reply.Body != "> <@bob:x> earlier\n\nanswer" {
		t.Error("incoming changed its input")
	}

	image := &event.MessageEventContent{MsgType: event.MsgImage, Body: "cat.png", URL: "mxc://x/cat", Info: &event.FileInfo{Size: 3}}
	out, replyTo = incoming(image)
	if out.MsgType != event.MsgImage || out.Body != "cat.png" || out.URL != "" || out.Info != nil || replyTo != nil {
		t.Errorf("image converted to %+v, %+v", out, replyTo)
	}
}

func TestCryptoAccountID(t *testing.T) {
	a := cryptoAccountID("@alice:example.org", "DEVICE1")
	b := cryptoAccountID("@alice:example.org", "DEVICE2")
	if a == b || a != "@alice:example.org/DEVICE1" {
		t.Errorf("cryptoAccountID = %q, %q", a, b)
	}
}
