// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package echo

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

// The media that the media commands send, made here so that the tests need
// no file: small, but real enough for a browser to show and play.
const (
	ImageWidth, ImageHeight = 96, 64
	// VoiceDurationMS is the length of the voice message.
	VoiceDurationMS = 500
	// FileText is the content of the text file.
	FileText = "A file from the echo network.\n"
)

// MaxMediaSize is the largest media the echo network takes: 10 MiB, so
// that the tests can try a larger one.
const MaxMediaSize = 10 << 20

// convertMediaEcho sends back a copy of the user's media, with the same
// type, caption and description.
func convertMediaEcho(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, content *event.MessageEventContent) (*bridgev2.ConvertedMessage, error) {
	data, err := intent.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return nil, err
	}
	echo := *content
	mimeType := ""
	if content.Info != nil {
		info := *content.Info
		echo.Info, mimeType = &info, info.MimeType
	}
	echo.URL, echo.File, err = intent.UploadMedia(ctx, portal.MXID, data, content.GetFileName(), mimeType)
	if err != nil {
		return nil, err
	}
	return &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{Type: event.EventMessage, Content: &echo}},
	}, nil
}

// convertMedia returns a conversion that uploads the media of a media
// command and sends it, with the message's text as its caption, as a
// network connector converts an incoming photo or file.
func convertMedia(cmd string) func(context.Context, *bridgev2.Portal, bridgev2.MatrixAPI, *event.MessageEventContent) (*bridgev2.ConvertedMessage, error) {
	return func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, content *event.MessageEventContent) (*bridgev2.ConvertedMessage, error) {
		media := &event.MessageEventContent{Info: &event.FileInfo{}}
		var data []byte
		var err error
		switch cmd {
		case CommandImage:
			media.MsgType, media.FileName, media.Info.MimeType = event.MsgImage, "echo.png", "image/png"
			media.Info.Width, media.Info.Height = ImageWidth, ImageHeight
			data, err = makePNG()
		case CommandGIF:
			media.MsgType, media.FileName, media.Info.MimeType = event.MsgImage, "echo.gif", "image/gif"
			media.Info.Width, media.Info.Height, media.Info.IsAnimated = 32, 32, true
			data, err = makeGIF()
		case CommandFile:
			media.MsgType, media.FileName, media.Info.MimeType = event.MsgFile, "echo.txt", "text/plain"
			data = []byte(FileText)
		case CommandVoice:
			media.MsgType, media.FileName, media.Info.MimeType = event.MsgAudio, "voice.wav", "audio/wav"
			media.Info.Duration = VoiceDurationMS
			media.MSC1767Audio = &event.MSC1767Audio{Duration: VoiceDurationMS}
			media.MSC3245Voice = &event.MSC3245Voice{}
			data = makeWAV()
		}
		if err != nil {
			return nil, err
		}
		media.Info.Size = len(data)
		media.Body = media.FileName
		if content.Body != "" {
			media.Body = content.Body
		}
		media.URL, media.File, err = intent.UploadMedia(ctx, portal.MXID, data, media.FileName, media.Info.MimeType)
		if err != nil {
			return nil, err
		}
		return &bridgev2.ConvertedMessage{
			Parts: []*bridgev2.ConvertedMessagePart{{Type: event.EventMessage, Content: media}},
		}, nil
	}
}

// makePNG draws a gradient.
func makePNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, ImageWidth, ImageHeight))
	for y := range ImageHeight {
		for x := range ImageWidth {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / ImageWidth), G: uint8(y * 255 / ImageHeight), B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

// makeGIF draws two frames that alternate forever.
func makeGIF() ([]byte, error) {
	palette := color.Palette{color.White, color.RGBA{R: 200, G: 60, B: 90, A: 255}}
	anim := &gif.GIF{LoopCount: 0}
	for frame := range 2 {
		img := image.NewPaletted(image.Rect(0, 0, 32, 32), palette)
		for y := range 32 {
			for x := range 32 {
				if (x < 16) == (frame == 0) {
					img.SetColorIndex(x, y, 1)
				}
			}
		}
		anim.Image = append(anim.Image, img)
		anim.Delay = append(anim.Delay, 50)
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, anim)
	return buf.Bytes(), err
}

// makeWAV makes a 440 Hz tone: 8 kHz, 16-bit mono PCM.
func makeWAV() []byte {
	const rate = 8000
	samples := rate * VoiceDurationMS / 1000
	var buf bytes.Buffer
	write := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	buf.WriteString("RIFF")
	write(uint32(36 + samples*2))
	buf.WriteString("WAVEfmt ")
	write(uint32(16))
	write(uint16(1)) // PCM
	write(uint16(1)) // mono
	write(uint32(rate))
	write(uint32(rate * 2))
	write(uint16(2))
	write(uint16(16))
	buf.WriteString("data")
	write(uint32(samples * 2))
	for i := range samples {
		write(int16(math.Sin(2*math.Pi*440*float64(i)/rate) * 8000))
	}
	return buf.Bytes()
}
