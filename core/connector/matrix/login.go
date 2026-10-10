// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// Login flows and steps.
const (
	FlowPassword   = "password"
	stepPassword   = "app.musubee.matrix.password" //nolint:gosec // A step ID, not a credential.
	stepComplete   = "app.musubee.matrix.complete"
	fieldServer    = "server"
	fieldUsername  = "username"
	fieldPassword  = "password"
	loginNotice    = "Sign in with your Matrix account: your server (for example matrix.org), your username and your password."
	wrongPassword  = "wrong username or password"
	defaultBaseURL = "https://"
)

// GetLoginFlows lists the login flows: a password only, for now. Single
// sign-on (OIDC) is for a later task.
func (c *Connector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Password",
		Description: "Sign in with your server, username and password.",
		ID:          FlowPassword,
	}}
}

// CreateLogin starts a login.
func (c *Connector) CreateLogin(_ context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	if flowID != FlowPassword {
		return nil, fmt.Errorf("unknown login flow %q", flowID)
	}
	return &passwordLogin{connector: c, user: user}, nil
}

type passwordLogin struct {
	connector *Connector
	user      *bridgev2.User
}

var _ bridgev2.LoginProcessUserInput = (*passwordLogin)(nil)

func (lp *passwordLogin) Start(context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       stepPassword,
		Instructions: loginNotice,
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{{
				Type:        bridgev2.LoginInputFieldTypeDomain,
				ID:          fieldServer,
				Name:        "Server",
				Description: "matrix.org, or the address of your server",
			}, {
				Type: bridgev2.LoginInputFieldTypeUsername,
				ID:   fieldUsername,
				Name: "Username",
			}, {
				Type: bridgev2.LoginInputFieldTypePassword,
				ID:   fieldPassword,
				Name: "Password",
			}},
		},
	}, nil
}

func (lp *passwordLogin) Cancel() {}

func (lp *passwordLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	base, err := discover(ctx, input[fieldServer])
	if err != nil {
		return nil, err
	}
	cli, err := mautrix.NewClient(base, "", "")
	if err != nil {
		return nil, err
	}
	username := strings.TrimSpace(input[fieldUsername])
	resp, err := cli.Login(ctx, &mautrix.ReqLogin{
		Type:                     mautrix.AuthTypePassword,
		Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: username},
		Password:                 input[fieldPassword],
		InitialDeviceDisplayName: lp.connector.Config.DeviceName,
		StoreCredentials:         true,
	})
	if errors.Is(err, mautrix.MForbidden) {
		return nil, errors.New(wrongPassword)
	} else if err != nil {
		return nil, fmt.Errorf("signing in: %w", err)
	}
	if resp.WellKnown != nil && resp.WellKnown.Homeserver.BaseURL != "" {
		base = resp.WellKnown.Homeserver.BaseURL
	}
	displayName := ""
	if own, err := cli.GetOwnDisplayName(ctx); err == nil {
		displayName = own.DisplayName
	}
	// Signing in again to an account already on the device replaces its
	// session: the old device is logged out and its keys deleted.
	if old := lp.connector.br.GetCachedUserLoginByID(networkid.UserLoginID(resp.UserID)); old != nil {
		if oldClient, ok := old.Client.(*client); ok {
			oldClient.endSession(ctx)
		}
	}
	login, err := lp.user.NewLogin(ctx, &database.UserLogin{
		ID:         networkid.UserLoginID(resp.UserID),
		RemoteName: accountName(resp.UserID, displayName),
		Metadata: &LoginMetadata{
			HomeserverURL: base,
			AccessToken:   resp.AccessToken,
			DeviceID:      string(resp.DeviceID),
		},
	}, &bridgev2.NewLoginParams{DeleteOnConflict: true})
	if err != nil {
		// The session exists on the server: end it rather than leave it.
		_, _ = cli.Logout(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("saving the login: %w", err)
	}
	lp.connector.connectLater(login)
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       stepComplete,
		Instructions: "Signed in as " + login.RemoteName + ".",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: login.ID,
			UserLogin:   login,
		},
	}, nil
}

// discover returns the base URL of a homeserver: the URL typed by the user,
// or for a server name, the one its .well-known file announces, or else
// https:// and the name.
func discover(ctx context.Context, server string) (string, error) {
	u, err := homeserverURL(server)
	if err != nil {
		return "", err
	}
	if strings.Contains(strings.TrimSpace(server), "://") {
		return u.String(), nil
	}
	wellKnown, err := mautrix.DiscoverClientAPI(ctx, u.Host)
	if err == nil && wellKnown != nil && wellKnown.Homeserver.BaseURL != "" {
		return wellKnown.Homeserver.BaseURL, nil
	}
	return defaultBaseURL + u.Host, nil
}
