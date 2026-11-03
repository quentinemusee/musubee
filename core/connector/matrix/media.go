// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"context"
	"errors"
	"fmt"
	"io"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

// maxMediaBytes is the largest media copied to the device: 100 MiB, more
// than homeservers accept by default (Synapse's max_upload_size is 50 MiB).
const maxMediaBytes = 100 << 20

var errMediaTooLarge = errors.New("the media is larger than the device keeps")

// copyMedia copies a received message's media from the homeserver to the
// device, decrypting it when the room is encrypted, so that the interface
// reads it like any network's (docs/ADR/0036): the content then names the
// device's copy, never the homeserver's. Media that cannot be copied (too
// large, gone, refused) is left out: the message keeps its caption or file
// name, and the interface shows it as a message it cannot show.
func (c *client) copyMedia(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, content *event.MessageEventContent) {
	if content == nil || !isMediaMessage(content.MsgType) || (content.URL == "" && content.File == nil) {
		return
	}
	data, err := c.downloadMedia(ctx, content)
	if err == nil {
		mimeType := ""
		if content.Info != nil {
			mimeType = content.Info.MimeType
		}
		content.URL, content.File, err = intent.UploadMedia(ctx, portal.MXID, data, content.GetFileName(), mimeType)
	}
	if err != nil {
		// No URL nor file name: they may be personal.
		c.login.Log.Warn().Err(err).Str("msgtype", string(content.MsgType)).Msg("Failed to copy the media of a message")
		content.URL, content.File = "", nil
	}
}

// downloadMedia reads media from the homeserver (authenticated media, as
// mautrix-go's Client.Download does), decrypting it if it is encrypted.
func (c *client) downloadMedia(ctx context.Context, content *event.MessageEventContent) ([]byte, error) {
	if content.Info != nil && content.Info.Size > maxMediaBytes {
		return nil, errMediaTooLarge
	}
	source := content.URL
	if content.File != nil {
		source = content.File.URL
		if err := content.File.PrepareForDecryption(); err != nil {
			return nil, err
		}
	}
	uri, err := source.Parse()
	if err != nil {
		return nil, err
	}
	resp, err := c.cli.Download(ctx, uri)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the media: %w", err)
	}
	if len(data) > maxMediaBytes {
		return nil, errMediaTooLarge
	}
	if content.File != nil {
		if err = content.File.DecryptInPlace(data); err != nil {
			return nil, fmt.Errorf("decrypting the media: %w", err)
		}
	}
	return data, nil
}
