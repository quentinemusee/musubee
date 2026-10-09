// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package echo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// ErrInvalidUsername is returned for a username that does not match the
// pattern advertised in the login step.
var ErrInvalidUsername = errors.New("invalid username: use 1 to 32 lowercase letters and digits")

var usernameRegexp = regexp.MustCompile(usernamePattern)

type loginProcess struct {
	connector *Connector
	user      *bridgev2.User
}

var _ bridgev2.LoginProcessUserInput = (*loginProcess)(nil)

func (lp *loginProcess) Start(context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       stepUsername,
		Instructions: loginInstruction,
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{{
				Type:    bridgev2.LoginInputFieldTypeUsername,
				ID:      fieldUsername,
				Name:    "Username",
				Pattern: usernamePattern,
			}},
		},
	}, nil
}

func (lp *loginProcess) Cancel() {}

func (lp *loginProcess) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	username := input[fieldUsername]
	if !usernameRegexp.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	return completeLogin(ctx, lp.connector, lp.user, username)
}

// completeLogin saves the login of a username and connects it.
func completeLogin(ctx context.Context, connector *Connector, user *bridgev2.User, username string) (*bridgev2.LoginStep, error) {
	login, err := user.NewLogin(ctx, &database.UserLogin{
		ID:         networkid.UserLoginID(username),
		RemoteName: username,
	}, &bridgev2.NewLoginParams{DeleteOnConflict: true})
	if err != nil {
		return nil, fmt.Errorf("saving the login: %w", err)
	}
	go login.Client.Connect(login.Log.WithContext(connector.br.BackgroundCtx))
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       stepCompleteID,
		Instructions: fmt.Sprintf("Logged in as %s.", username),
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: login.ID,
			UserLogin:   login,
		},
	}, nil
}

// codeLoginProcess shows a code and completes by itself after the echo
// delay, like a QR code login that the user confirms on their phone. It
// exercises the display_and_wait steps of user interfaces.
type codeLoginProcess struct {
	connector *Connector
	user      *bridgev2.User
	code      string
}

var _ bridgev2.LoginProcessDisplayAndWait = (*codeLoginProcess)(nil)

func newCodeLoginProcess(connector *Connector, user *bridgev2.User) *codeLoginProcess {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return &codeLoginProcess{connector: connector, user: user, code: hex.EncodeToString(b)}
}

func (lp *codeLoginProcess) Start(context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeDisplayAndWait,
		StepID:       stepCode,
		Instructions: "The echo network confirms this code by itself in a moment.",
		DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
			Type:      bridgev2.LoginDisplayTypeCode,
			Data:      lp.code,
			CanCancel: true,
		},
	}, nil
}

// Wait completes the login as "code" followed by the code, after the echo
// delay, unless ctx ends first.
func (lp *codeLoginProcess) Wait(ctx context.Context) (*bridgev2.LoginStep, error) {
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-time.After(lp.connector.Config.EchoDelay):
	}
	return completeLogin(ctx, lp.connector, lp.user, "code"+lp.code)
}

func (lp *codeLoginProcess) Cancel() {}
