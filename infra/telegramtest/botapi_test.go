// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package telegramtest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindChannel(t *testing.T) {
	channel := Chat{ID: -1001, Type: "channel", Title: "Musubee CI"}
	older := Chat{ID: -1002, Type: "channel", Title: "Old"}
	group := Chat{ID: -42, Type: "group", Title: "Musubee CI group"}

	tests := []struct {
		name    string
		updates []Update
		want    Chat
		wantErr string
	}{
		{name: "channel post", updates: []Update{{ChannelPost: &Post{Chat: channel}}}, want: channel},
		{name: "added as admin", updates: []Update{{MyChatMember: &struct {
			Chat Chat `json:"chat"`
		}{Chat: channel}}}, want: channel},
		{name: "most recent wins", updates: []Update{{ChannelPost: &Post{Chat: older}}, {ChannelPost: &Post{Chat: channel}}}, want: channel},
		{name: "channel preferred over group", updates: []Update{{ChannelPost: &Post{Chat: channel}}, {Message: &Post{Chat: group}}}, want: channel},
		{name: "group only", updates: []Update{{Message: &Post{Chat: group}}}, wantErr: "not a channel"},
		{name: "nothing", updates: nil, wantErr: ErrNoChannel.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindChannel(tt.updates)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("FindChannel() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("FindChannel() = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestFindChannelNothingIsErrNoChannel(t *testing.T) {
	if _, err := FindChannel(nil); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("error = %v, want ErrNoChannel", err)
	}
}

// Pure unit test with a local fake server: the token is part of the URL and
// must never appear in errors, since CI logs are kept.
func TestBotAPIErrorsDoNotLeakTheToken(t *testing.T) {
	const token = "123456:SECRET-TOKEN"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, token) {
			t.Errorf("token not used in the request path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	defer server.Close()

	bot := BotAPI{Token: token, BaseURL: server.URL}
	_, err := bot.GetMe(t.Context())
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("GetMe() error = %v, want Unauthorized", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error leaks the token: %v", err)
	}

	unreachable := BotAPI{Token: token, BaseURL: "http://127.0.0.1:1"}
	_, err = unreachable.GetMe(t.Context())
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("network error = %v, want an error without the token", err)
	}
}
