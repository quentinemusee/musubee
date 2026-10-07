// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command tgsession logs in to a Telegram TEST-environment account (created
// beforehand in an official Telegram app, see infra/README.md) and saves the
// resulting session for the end-to-end tests.
//
//	go run ./infra/cmd/tgsession
//
// It asks for the phone number, the login code (Telegram sends it to the
// test account in the official app) and, if the account has one, the 2FA
// password. The session is written to infra/.testenv/telegram/session.b64
// (ignored by Git) and never printed. Store it as a CI secret with:
//
//	gh secret set MUSUBEE_TG_SESSION < infra/.testenv/telegram/session.b64
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"

	"github.com/quentinemusee/musubee/infra/telegramtest"
	"github.com/quentinemusee/musubee/infra/testenv"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tgsession:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	creds, err := telegramtest.CredentialsFromEnv()
	if err != nil {
		return err
	}
	if creds.IsPublicTestApp() {
		fmt.Println("Using the public test application credentials (set MUSUBEE_TG_API_ID and MUSUBEE_TG_API_HASH to use your own).")
	}

	in := bufio.NewReader(os.Stdin)
	phone, err := prompt(in, "Phone number of the TEST account (international format, e.g. +33612345678): ")
	if err != nil {
		return err
	}

	storage := &session.StorageMemory{}
	client := telegramtest.NewClient(creds, storage)
	var self *tg.User
	err = client.Run(ctx, func(ctx context.Context) error {
		codeAsker := auth.CodeAuthenticatorFunc(func(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
			return prompt(in, "Login code (sent by Telegram to the test account in the official app): ")
		})
		flow := auth.NewFlow(passwordPrompt{phone: phone, code: codeAsker, in: in}, auth.SendCodeOptions{})
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return err
		}
		self, err = client.Self(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("logging in to the test environment: %w", err)
	}

	encoded, err := telegramtest.EncodeSession(ctx, storage)
	if err != nil {
		return err
	}
	infraDir, err := testenv.InfraDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(infraDir, ".testenv", "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "session.b64")
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return err
	}

	fmt.Printf("\nLogged in to the test environment as user ID %d (%s).\n", self.ID, self.FirstName)
	fmt.Printf("Session saved to %s (ignored by Git; do not share it).\n\n", path)
	fmt.Println("Store it as a GitHub Actions secret:")
	fmt.Println("  gh secret set MUSUBEE_TG_SESSION < infra/.testenv/telegram/session.b64")
	if !creds.IsPublicTestApp() {
		fmt.Println("  gh secret set MUSUBEE_TG_API_ID     (then type the value)")
		fmt.Println("  gh secret set MUSUBEE_TG_API_HASH   (then type the value)")
	}
	return nil
}

// passwordPrompt asks for the 2FA password only when Telegram requires it.
type passwordPrompt struct {
	phone string
	code  auth.CodeAuthenticator
	in    *bufio.Reader
}

func (p passwordPrompt) Phone(context.Context) (string, error) { return p.phone, nil }

func (p passwordPrompt) Password(context.Context) (string, error) {
	fmt.Print("2FA password of the test account: ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(pw), err
	}
	return readLine(p.in)
}

func (p passwordPrompt) Code(ctx context.Context, sent *tg.AuthSentCode) (string, error) {
	return p.code.Code(ctx, sent)
}

func (p passwordPrompt) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return nil
}

func (p passwordPrompt) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("this account does not exist on the test environment yet: create it in an official Telegram app first (see infra/README.md)")
}

func prompt(in *bufio.Reader, question string) (string, error) {
	fmt.Print(question)
	return readLine(in)
}

func readLine(in *bufio.Reader) (string, error) {
	line, err := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" && err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	if line == "" {
		return "", errors.New("empty answer")
	}
	return line, nil
}
