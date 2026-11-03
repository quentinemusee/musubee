// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"context"
	"errors"
	"fmt"
	"io"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
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

// maxSentMediaBytes is the largest media the user sends: Synapse's default
// max_upload_size (50 MiB). A homeserver that takes less refuses the
// upload, and the message fails with its answer.
const maxSentMediaBytes = 50 << 20

// mediaFeatures are the media a Matrix room takes: every kind and type,
// with a caption, up to maxSentMediaBytes.
func mediaFeatures() event.FileFeatureMap {
	features := event.FileFeatureMap{}
	for _, kind := range []event.CapabilityMsgType{event.MsgImage, event.MsgVideo, event.MsgAudio, event.CapMsgVoice, event.MsgFile} {
		features[kind] = &event.FileFeatures{
			MimeTypes: map[string]event.CapabilitySupportLevel{"*/*": event.CapLevelFullySupported},
			Caption:   event.CapLevelFullySupported,
			MaxSize:   maxSentMediaBytes,
		}
	}
	return features
}

// uploadMedia reads the user's media from the device and uploads it to the
// homeserver, encrypted as Matrix clients encrypt attachments when the room
// is encrypted (the event then names it in file, not url). It returns the
// content to send, without the device's URI.
func (c *client) uploadMedia(ctx context.Context, room id.RoomID, local *event.MessageEventContent) (*event.MessageEventContent, error) {
	data, err := c.connector.br.Bot.DownloadMedia(ctx, local.URL, local.File)
	if err != nil {
		return nil, fmt.Errorf("reading the media on the device: %w", err)
	}
	content := &event.MessageEventContent{
		MsgType:      local.MsgType,
		Body:         local.Body,
		FileName:     local.FileName,
		MSC3245Voice: local.MSC3245Voice,
		MSC1767Audio: local.MSC1767Audio,
	}
	mimeType := ""
	if local.Info != nil {
		info := *local.Info
		info.ThumbnailURL, info.ThumbnailFile, info.ThumbnailInfo = "", nil, nil
		content.Info, mimeType = &info, info.MimeType
	}
	encrypted, err := c.cli.StateStore.IsEncrypted(ctx, room)
	if err != nil {
		return nil, fmt.Errorf("checking whether the room is encrypted: %w", err)
	}
	uploadType := mimeType
	var file *attachment.EncryptedFile
	if encrypted {
		file = attachment.NewEncryptedFile()
		file.EncryptInPlace(data)
		uploadType = "application/octet-stream"
	}
	resp, err := c.cli.UploadBytes(ctx, data, uploadType)
	if err != nil {
		return nil, fmt.Errorf("uploading the media: %w", err)
	}
	if file != nil {
		content.File = &event.EncryptedFileInfo{EncryptedFile: *file, URL: resp.ContentURI.CUString()}
	} else {
		content.URL = resp.ContentURI.CUString()
	}
	return content, nil
}
