// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

// Integration tests against a real Synapse and PostgreSQL started with Docker
// Compose. Run them with:
//
//	go test -tags=integration ./infra/...
//
// Without Docker they are skipped, unless MUSUBEE_REQUIRE_INTEGRATION=1 (set
// in CI), in which case they fail.

package testenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// env is shared by the tests of this package: starting Synapse takes several
// seconds, and each test uses its own users and rooms.
var env *Env

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := DockerAvailable(ctx); err != nil {
		if os.Getenv("MUSUBEE_REQUIRE_INTEGRATION") == "1" {
			fmt.Fprintln(os.Stderr, "integration tests required but Docker is unavailable:", err)
			return 1
		}
		fmt.Fprintln(os.Stderr, "skipping integration tests, Docker is unavailable:", err)
		return 0
	}

	project := "musubee-it-" + randomSuffix()
	secretsDir, err := os.MkdirTemp("", project+"-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	started := time.Now()
	env, err = Start(ctx, project, secretsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "test environment %s ready at %s in %s\n", project, env.HomeserverURL, time.Since(started).Round(time.Second))

	code := m.Run()

	if err := env.Stop(context.WithoutCancel(ctx)); err != nil {
		fmt.Fprintln(os.Stderr, "stopping the environment:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func TestHomeserverIsHealthy(t *testing.T) {
	resp, err := http.Get(env.HomeserverURL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "OK" {
		t.Fatalf("/health = %d %q, want 200 \"OK\"", resp.StatusCode, body)
	}

	client, err := mautrix.NewClient(env.HomeserverURL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	versions, err := client.Versions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !versions.ContainsGreaterOrEqual(mautrix.SpecV111) {
		t.Errorf("homeserver does not support Matrix v1.11: %v", versions.Versions)
	}
}

// TestRoomMessageRoundTrip is the T0.3 acceptance test: a test client creates
// a room, sends a message and reads it back; a second user joins and reads it
// too.
func TestRoomMessageRoundTrip(t *testing.T) {
	ctx := t.Context()
	alice := newUser(t, "alice")
	bob := newUser(t, "bob")

	room, err := alice.CreateRoom(ctx, &mautrix.ReqCreateRoom{
		Preset: "private_chat",
		Name:   "T0.3 round trip",
		Invite: []id.UserID{bob.UserID},
	})
	if err != nil {
		t.Fatalf("creating the room: %v", err)
	}

	text := "hello from the integration test " + randomSuffix()
	sent, err := alice.SendText(ctx, room.RoomID, text)
	if err != nil {
		t.Fatalf("sending the message: %v", err)
	}

	assertLatestMessage(t, alice, room.RoomID, sent.EventID, alice.UserID, text)

	if _, err := bob.JoinRoomByID(ctx, room.RoomID); err != nil {
		t.Fatalf("bob joining the room: %v", err)
	}
	assertLatestMessage(t, bob, room.RoomID, sent.EventID, alice.UserID, text)
}

// TestRegistrationRejectsAWrongSharedSecret checks that test users can only be
// created with the per-run secret, not with any guessable value.
func TestRegistrationRejectsAWrongSharedSecret(t *testing.T) {
	_, err := env.register(t.Context(), "mallory-"+randomSuffix(), "password", true, "not-the-secret")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden {
		t.Fatalf("registration with a wrong secret: err = %v, want HTTP 403", err)
	}
}

func newUser(t *testing.T, name string) *mautrix.Client {
	t.Helper()
	localpart := name + "-" + randomSuffix()
	password := randomSuffix() + randomSuffix()
	userID, err := env.RegisterUser(t.Context(), localpart, password, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := id.NewUserID(localpart, ServerName); userID != want {
		t.Fatalf("registered user ID = %s, want %s", userID, want)
	}
	client, err := env.Login(t.Context(), localpart, password)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertLatestMessage(t *testing.T, client *mautrix.Client, roomID id.RoomID, eventID id.EventID, sender id.UserID, text string) {
	t.Helper()
	filter := &mautrix.FilterPart{Types: []event.Type{event.EventMessage}}
	resp, err := client.Messages(t.Context(), roomID, "", "", mautrix.DirectionBackward, filter, 10)
	if err != nil {
		t.Fatalf("%s reading messages: %v", client.UserID, err)
	}
	for _, evt := range resp.Chunk {
		if evt.ID != eventID {
			continue
		}
		if evt.Sender != sender {
			t.Errorf("%s: sender = %s, want %s", client.UserID, evt.Sender, sender)
		}
		if err := evt.Content.ParseRaw(evt.Type); err != nil {
			t.Fatalf("%s: parsing the message: %v", client.UserID, err)
		}
		if got := evt.Content.AsMessage().Body; got != text {
			t.Errorf("%s: body = %q, want %q", client.UserID, got, text)
		}
		return
	}
	t.Fatalf("%s: message %s not found among %d events", client.UserID, eventID, len(resp.Chunk))
}

func randomSuffix() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
