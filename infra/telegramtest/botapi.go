// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package telegramtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BotAPI is a minimal client for Telegram's Bot API, used by the tests to
// drive the peer bot.
type BotAPI struct {
	Token string
	// BaseURL defaults to https://api.telegram.org; tests may override it.
	BaseURL string
}

// BotUser is the result of getMe.
type BotUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Chat is a Telegram chat as seen by the Bot API.
type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// Post is a message or channel post.
type Post struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      Chat   `json:"chat"`
}

// Update is a Bot API update; only the fields the tests use are decoded.
type Update struct {
	UpdateID     int64 `json:"update_id"`
	Message      *Post `json:"message"`
	ChannelPost  *Post `json:"channel_post"`
	MyChatMember *struct {
		Chat Chat `json:"chat"`
	} `json:"my_chat_member"`
}

// Chat returns the chat an update is about, if any.
func (u Update) Chat() (Chat, bool) {
	switch {
	case u.ChannelPost != nil:
		return u.ChannelPost.Chat, true
	case u.Message != nil:
		return u.Message.Chat, true
	case u.MyChatMember != nil:
		return u.MyChatMember.Chat, true
	}
	return Chat{}, false
}

// Text returns the text of a message or channel post update.
func (u Update) Text() string {
	switch {
	case u.ChannelPost != nil:
		return u.ChannelPost.Text
	case u.Message != nil:
		return u.Message.Text
	}
	return ""
}

// ErrNoChannel is returned when no channel shows up in the bot's updates.
var ErrNoChannel = errors.New("no channel found in the bot's recent updates")

// FindChannel picks the most recent channel among updates. Groups are
// rejected on purpose: Telegram never delivers a bot's group messages to
// other bots, so the test needs a channel.
func FindChannel(updates []Update) (Chat, error) {
	var group *Chat
	for i := len(updates) - 1; i >= 0; i-- {
		chat, ok := updates[i].Chat()
		if !ok {
			continue
		}
		if chat.Type == "channel" {
			return chat, nil
		}
		if chat.Type == "group" || chat.Type == "supergroup" {
			group = &chat
		}
	}
	if group != nil {
		return Chat{}, fmt.Errorf("%q (id %d) is a %s, not a channel: Telegram does not deliver messages between bots in groups; create a channel and add both bots as administrators", group.Title, group.ID, group.Type)
	}
	return Chat{}, ErrNoChannel
}

func (b BotAPI) call(ctx context.Context, method string, params, out any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	base := b.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+b.Token+"/"+method, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		// The URL contains the token: never report it.
		return fmt.Errorf("Bot API %s: request failed", method)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("Bot API %s: HTTP %d", method, resp.StatusCode)
	}
	if !envelope.OK {
		return fmt.Errorf("Bot API %s: %s", method, envelope.Description)
	}
	return json.Unmarshal(envelope.Result, out)
}

// GetMe returns the bot's own user.
func (b BotAPI) GetMe(ctx context.Context) (BotUser, error) {
	var u BotUser
	err := b.call(ctx, "getMe", map[string]any{}, &u)
	return u, err
}

// Updates returns pending updates without confirming them, so that a later
// call (or run) still sees them.
func (b BotAPI) Updates(ctx context.Context, timeoutSeconds int) ([]Update, error) {
	var updates []Update
	err := b.call(ctx, "getUpdates", map[string]any{
		"timeout":         timeoutSeconds,
		"allowed_updates": []string{"message", "channel_post", "my_chat_member"},
	}, &updates)
	return updates, err
}

// SendMessage sends a text message to a chat and returns its message ID.
func (b BotAPI) SendMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	var post Post
	err := b.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, &post)
	return post.MessageID, err
}

// WaitForText polls the updates until a message or channel post with this
// exact text appears in the chat.
func (b BotAPI) WaitForText(ctx context.Context, chatID int64, text string, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		updates, err := b.Updates(ctx, 10)
		if err != nil {
			return err
		}
		for _, u := range updates {
			if chat, ok := u.Chat(); ok && chat.ID == chatID && u.Text() == text {
				return nil
			}
		}
		if err := sleep(ctx, 2*time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("no message %q in chat %d within %s", text, chatID, within)
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
