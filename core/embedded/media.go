// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// This file translates the media of messages (photos, videos, voice
// messages, files) and reads their bytes for the interface (API 1.9,
// docs/ADR/0036).

// maxMediaRead is the most media.read returns at once: 1 MiB, 1.4 MB of
// base64 in one JSON response, which every transport carries in one piece.
const maxMediaRead = 1 << 20

var mediaKinds = map[event.MessageType]api.MessageKind{
	event.MsgImage: api.MessageKindImage,
	event.MsgVideo: api.MessageKindVideo,
	event.MsgAudio: api.MessageKindAudio,
	event.MsgFile:  api.MessageKindFile,
}

// messageContent returns the kind, text and media of a message, given its
// original content and its latest (the original unless edited).
func messageContent(original, latest *event.MessageEventContent) (api.MessageKind, string, *api.MessageMedia) {
	if kind, ok := messageKinds[latest.MsgType]; ok {
		return kind, latest.Body, nil
	}
	kind, ok := mediaKinds[latest.MsgType]
	if !ok {
		return api.MessageKindUnsupported, latest.Body, nil
	}
	source, caption := latest, latest.GetCaption()
	if latest != original && latest.URL == "" && latest.File == nil && original.URL != "" {
		// An edit of the caption alone may not repeat the media: its body
		// is the new caption.
		source, caption = original, latest.Body
	}
	if source.URL == "" {
		// Encrypted media is never stored on the device (localmatrix), and
		// media without a file cannot be shown: its fallback text then.
		return api.MessageKindUnsupported, source.Body, nil
	}
	media := &api.MessageMedia{MediaID: mediaID(source.URL), FileName: source.GetFileName()}
	if info := source.Info; info != nil {
		media.MimeType = info.MimeType
		media.Size = nonNegative(info.Size)
		media.Width, media.Height = nonNegative(info.Width), nonNegative(info.Height)
		media.DurationMS = nonNegative(info.Duration)
		media.Animated = info.MauGIF || info.IsAnimated || strings.EqualFold(info.MimeType, "image/gif")
		if info.ThumbnailURL != "" && info.ThumbnailFile == nil {
			media.ThumbnailID = mediaID(info.ThumbnailURL)
		}
	}
	if kind == api.MessageKindAudio && source.MSC3245Voice != nil {
		kind = api.MessageKindVoice
	}
	if media.DurationMS == 0 && source.MSC1767Audio != nil {
		media.DurationMS = nonNegative(source.MSC1767Audio.Duration)
	}
	if kind != api.MessageKindImage && kind != api.MessageKindVideo {
		media.Animated = false
	}
	return kind, caption, media
}

func nonNegative(n int) int64 {
	return int64(max(n, 0))
}

func (c *Core) mediaRead(ctx context.Context, p api.MediaReadParams) (api.MediaReadResult, error) {
	length := p.Length
	if length == 0 {
		length = maxMediaRead
	}
	if p.Offset < 0 || length < 1 || length > maxMediaRead {
		return api.MediaReadResult{}, newError(api.ErrorCodeInvalidParams, "offset must be at least 0 and length between 1 and %d", maxMediaRead)
	}
	uri, err := parseMediaID(p.MediaID)
	if err != nil {
		return api.MediaReadResult{}, err
	}
	part, err := c.host.Matrix.ReadMedia(ctx, uri, p.Offset, length)
	if errors.Is(err, localmatrix.ErrNotFound) {
		return api.MediaReadResult{}, newError(api.ErrorCodeNotFound, "no media %q", p.MediaID)
	} else if err != nil {
		return api.MediaReadResult{}, err
	}
	return api.MediaReadResult{
		MimeType: part.MimeType,
		Size:     part.Size,
		Data:     base64.StdEncoding.EncodeToString(part.Data),
	}, nil
}
