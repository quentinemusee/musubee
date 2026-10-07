// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command tgsession logs in to (or creates) an account on Telegram's TEST
// environment, which is separate from production Telegram, and saves the
// session for the end-to-end tests.
//
//	go run ./infra/cmd/tgsession
//
// It asks for the phone number, then shows how Telegram says it delivered the
// login code (SMS, call, another app, payment required...), which helps when
// no code arrives. Type "resend" instead of the code to ask for another
// delivery method. If the number has no account on the test environment yet,
// it creates one. The session is written to
// infra/.testenv/telegram/session.b64 (ignored by Git) and never printed.
// Store it as a CI secret with:
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
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
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
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	creds, err := telegramtest.CredentialsFromEnv()
	if err != nil {
		return err
	}
	if creds.IsPublicTestApp() {
		fmt.Println("Using the public test application credentials (set MUSUBEE_TG_API_ID and MUSUBEE_TG_API_HASH to use your own).")
	}
	fmt.Println("This connects to Telegram's TEST environment, not to your usual Telegram account.")

	in := bufio.NewReader(os.Stdin)
	phone, err := prompt(in, "Phone number (international format, e.g. +33612345678): ")
	if err != nil {
		return err
	}

	storage := &session.StorageMemory{}
	client := telegramtest.NewClient(creds, storage)
	var self *tg.User
	err = client.Run(ctx, func(ctx context.Context) error {
		if err := login(ctx, client, in, phone); err != nil {
			return err
		}
		self, err = client.Self(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("test environment: %w", err)
	}
	return save(ctx, storage, self, creds)
}

func login(ctx context.Context, client *telegram.Client, in *bufio.Reader, phone string) error {
	sent, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		return fmt.Errorf("asking for a login code: %w", err)
	}
	for {
		fmt.Println("Telegram says:", telegramtest.DescribeSentCode(sent))
		code, ok := sent.(*tg.AuthSentCode)
		if !ok {
			if _, success := sent.(*tg.AuthSentCodeSuccess); success {
				return nil
			}
			return errors.New("cannot continue with this answer")
		}

		answer, err := prompt(in, "Login code (or \"resend\"): ")
		if err != nil {
			return err
		}
		if strings.EqualFold(answer, "resend") {
			next, err := client.API().AuthResendCode(ctx, &tg.AuthResendCodeRequest{PhoneNumber: phone, PhoneCodeHash: code.PhoneCodeHash})
			if wait, ok := tgerr.AsFloodWait(err); ok {
				fmt.Printf("Too early: wait %s, then type %q again.\n", wait.Round(time.Second), "resend")
				continue
			}
			if err != nil {
				return fmt.Errorf("asking for another delivery method: %w", err)
			}
			sent = next
			continue
		}

		_, err = client.Auth().SignIn(ctx, phone, answer, code.PhoneCodeHash)
		var signUp *auth.SignUpRequired
		switch {
		case err == nil:
			return nil
		case errors.Is(err, auth.ErrPasswordAuthNeeded):
			password, err := readPassword(in)
			if err != nil {
				return err
			}
			_, err = client.Auth().Password(ctx, password)
			return err
		case errors.As(err, &signUp):
			fmt.Println("This number has no account on the test environment yet: creating one.")
			firstName, err := prompt(in, "First name for the test account: ")
			if err != nil {
				return err
			}
			if signUp.TermsOfService.ID.Data != "" {
				if _, err := client.API().HelpAcceptTermsOfService(ctx, signUp.TermsOfService.ID); err != nil {
					return fmt.Errorf("accepting the terms of service: %w", err)
				}
			}
			_, err = client.Auth().SignUp(ctx, auth.SignUp{PhoneNumber: phone, PhoneCodeHash: code.PhoneCodeHash, FirstName: firstName})
			if err != nil {
				return fmt.Errorf("creating the account: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("signing in: %w", err)
		}
	}
}

func save(ctx context.Context, storage *session.StorageMemory, self *tg.User, creds telegramtest.Credentials) error {
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

func readPassword(in *bufio.Reader) (string, error) {
	fmt.Print("2FA password of the test account: ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(pw), err
	}
	return readLine(in)
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
