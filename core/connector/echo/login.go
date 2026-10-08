// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package echo

import (
	"context"
	"errors"
	"fmt"
	"regexp"

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
	login, err := lp.user.NewLogin(ctx, &database.UserLogin{
		ID:         networkid.UserLoginID(username),
		RemoteName: username,
	}, &bridgev2.NewLoginParams{DeleteOnConflict: true})
	if err != nil {
		return nil, fmt.Errorf("saving the login: %w", err)
	}
	go login.Client.Connect(login.Log.WithContext(lp.connector.br.BackgroundCtx))
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
