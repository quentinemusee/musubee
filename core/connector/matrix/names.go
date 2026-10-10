// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// The user interface never shows a Matrix identifier (CLAUDE.md §2): rooms
// and people are named by their display names, and when they have none, by
// the parts of their identifiers a person recognises.

func idDevice(device string) id.DeviceID { return id.DeviceID(device) }

// personName names a Matrix user: their display name, or the local part of
// their identifier ("alice" for @alice:example.org).
func personName(user id.UserID, displayName string) string {
	if name := strings.TrimSpace(displayName); name != "" && name != string(user) {
		return name
	}
	localpart, _, err := user.Parse()
	if err != nil || localpart == "" {
		return strings.TrimPrefix(string(user), "@")
	}
	return localpart
}

// accountName names the user's own account: their name and their server,
// so that two accounts of the same name on different servers stay apart.
func accountName(user id.UserID, displayName string) string {
	_, server, err := user.Parse()
	if err != nil || server == "" {
		return personName(user, displayName)
	}
	return fmt.Sprintf("%s (%s)", personName(user, displayName), server)
}

// member is a room member as the naming needs it.
type member struct {
	user        id.UserID
	displayName string
}

// roomName names a room: its name, or the other member of a direct
// conversation, or up to three of its other members, like the "heroes" of
// the Matrix specification. The members must be in a stable order.
func roomName(name string, self id.UserID, members []member) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	var others []string
	for _, m := range members {
		if m.user != self {
			others = append(others, personName(m.user, m.displayName))
		}
	}
	switch {
	case len(others) == 0:
		return "Empty conversation"
	case len(others) <= 3:
		return strings.Join(others, ", ")
	default:
		return fmt.Sprintf("%s and %d others", strings.Join(others[:3], ", "), len(others)-3)
	}
}

// sortMembers orders members by user ID, so that names built from them do
// not change between reads.
func sortMembers(members []member) {
	slices.SortFunc(members, func(a, b member) int { return strings.Compare(string(a.user), string(b.user)) })
}

// homeserverURL turns what the user typed into the URL of a homeserver: a
// URL is taken as is; a server name ("example.org") becomes an HTTPS URL,
// which the caller tries after the server's .well-known discovery.
func homeserverURL(input string) (*url.URL, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("the server is empty")
	}
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("%q is not a server address", input)
	}
	return u, nil
}

// isTextMessage reports whether a message type is one the connector sends
// as is. Media need the media support of later tasks.
func isTextMessage(t event.MessageType) bool {
	return t == event.MsgText || t == event.MsgNotice || t == event.MsgEmote
}
