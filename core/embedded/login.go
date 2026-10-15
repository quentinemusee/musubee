// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"sync"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/quentinemusee/musubee/core/api"
)

// loginProcess is a bridgev2 login in progress, between login.* calls.
type loginProcess struct {
	network networkid.BridgeID
	// mu serializes the calls on the process: bridgev2 login processes are
	// not safe for concurrent use.
	mu      sync.Mutex
	process bridgev2.LoginProcess
	step    *bridgev2.LoginStep

	// cancelWait ends a running login.wait; set while one runs.
	waitMu     sync.Mutex
	cancelWait context.CancelCauseFunc
}

// errLoginCancelled is the cause given to a login.wait ended by login.cancel.
var errLoginCancelled = newError(api.ErrorCodeCancelled, "the login was cancelled")

func (c *Core) loginStart(ctx context.Context, p api.LoginStartParams) (api.LoginStep, error) {
	network := networkid.BridgeID(p.NetworkID)
	br := c.host.Bridge(network)
	if br == nil || !c.hasNetwork(network) {
		return api.LoginStep{}, newError(api.ErrorCodeNotFound, "no network %q", p.NetworkID)
	}
	known := false
	for _, flow := range br.Network.GetLoginFlows() {
		known = known || flow.ID == p.FlowID
	}
	if !known {
		return api.LoginStep{}, newError(api.ErrorCodeNotFound, "no login flow %q on %s", p.FlowID, p.NetworkID)
	}
	var override *bridgev2.UserLogin
	if p.AccountID != "" {
		accountNetwork, loginID, err := parseAccountID(p.AccountID)
		if err != nil {
			return api.LoginStep{}, err
		}
		if accountNetwork == network {
			override = br.GetCachedUserLoginByID(loginID)
		}
		if override == nil {
			return api.LoginStep{}, newError(api.ErrorCodeNotFound, "no account %q on %s", p.AccountID, p.NetworkID)
		}
	}
	user, err := c.host.User(ctx, network)
	if err != nil {
		return api.LoginStep{}, err
	}
	process, err := br.Network.CreateLogin(ctx, user, p.FlowID)
	if err != nil {
		return api.LoginStep{}, networkError(err)
	}
	step, err := startLogin(ctx, process, override)
	if err != nil {
		process.Cancel()
		return api.LoginStep{}, networkError(err)
	}
	id := newProcessID()
	lp := &loginProcess{network: network, process: process}
	c.loginsMu.Lock()
	c.logins[id] = lp
	c.loginsMu.Unlock()
	return c.advance(id, lp, step)
}

// startLogin starts a process, telling the connector which login it
// replaces when there is one, as bridgev2's provisioning API does. A
// connector without the override interfaces starts as for a new account:
// logging in to the same remote account still reuses its login.
//
// Unlike the provisioning API, a login that completes on another remote
// account does not delete the replaced one: that would remove its
// conversations without the user asking (ADR 0030).
func startLogin(ctx context.Context, process bridgev2.LoginProcess, override *bridgev2.UserLogin) (*bridgev2.LoginStep, error) {
	if override != nil {
		if withParams, ok := process.(bridgev2.LoginProcessWithParams); ok {
			return withParams.StartWithParams(ctx, bridgev2.LoginStartParams{Override: override})
		}
		if overridable, ok := process.(bridgev2.LoginProcessWithOverride); ok {
			return overridable.StartWithOverride(ctx, override)
		}
	}
	return process.Start(ctx)
}

func (c *Core) loginSubmit(ctx context.Context, p api.LoginSubmitParams) (api.LoginStep, error) {
	lp, err := c.loginProcess(p.ProcessID)
	if err != nil {
		return api.LoginStep{}, err
	}
	lp.mu.Lock()
	defer lp.mu.Unlock()
	submitter, ok := lp.process.(bridgev2.LoginProcessUserInput)
	if lp.step == nil || lp.step.Type != bridgev2.LoginStepTypeUserInput || !ok {
		return api.LoginStep{}, newError(api.ErrorCodeInvalidParams, "the current step does not take input")
	}
	// The checks that bridgev2's provisioning API makes before submitting.
	input := make(map[string]string, len(lp.step.UserInputParams.Fields))
	for _, field := range lp.step.UserInputParams.Fields {
		value, ok := p.Values[field.ID]
		if !ok {
			return api.LoginStep{}, newError(api.ErrorCodeInvalidParams, "missing value for %s", field.ID)
		}
		field.FillDefaultValidate()
		cleaned, err := field.Validate(value)
		if err != nil {
			return api.LoginStep{}, newError(api.ErrorCodeInvalidParams, "invalid value for %s: %v", field.ID, err)
		}
		input[field.ID] = cleaned
	}
	step, err := submitter.SubmitUserInput(ctx, input)
	if err != nil {
		c.endLogin(p.ProcessID)
		return api.LoginStep{}, networkError(err)
	}
	return c.advance(p.ProcessID, lp, step)
}

func (c *Core) loginWait(ctx context.Context, p api.LoginProcessParams) (api.LoginStep, error) {
	lp, err := c.loginProcess(p.ProcessID)
	if err != nil {
		return api.LoginStep{}, err
	}
	lp.mu.Lock()
	defer lp.mu.Unlock()
	waiter, ok := lp.process.(bridgev2.LoginProcessDisplayAndWait)
	if lp.step == nil || lp.step.Type != bridgev2.LoginStepTypeDisplayAndWait || !ok {
		return api.LoginStep{}, newError(api.ErrorCodeInvalidParams, "the current step is not a display_and_wait step")
	}
	waitCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	lp.waitMu.Lock()
	lp.cancelWait = cancel
	lp.waitMu.Unlock()
	step, err := waiter.Wait(waitCtx)
	lp.waitMu.Lock()
	lp.cancelWait = nil
	lp.waitMu.Unlock()
	if err != nil {
		c.endLogin(p.ProcessID)
		if errors.Is(context.Cause(waitCtx), errLoginCancelled) {
			return api.LoginStep{}, errLoginCancelled
		}
		return api.LoginStep{}, networkError(err)
	}
	return c.advance(p.ProcessID, lp, step)
}

func (c *Core) loginCancel(_ context.Context, p api.LoginProcessParams) (api.Empty, error) {
	c.loginsMu.Lock()
	lp := c.logins[p.ProcessID]
	delete(c.logins, p.ProcessID)
	c.loginsMu.Unlock()
	if lp == nil {
		return api.Empty{}, nil
	}
	lp.waitMu.Lock()
	if lp.cancelWait != nil {
		lp.cancelWait(errLoginCancelled)
	}
	lp.waitMu.Unlock()
	// Wait for a running call to end before cancelling the process itself.
	lp.mu.Lock()
	lp.process.Cancel()
	lp.mu.Unlock()
	return api.Empty{}, nil
}

func (c *Core) loginProcess(id string) (*loginProcess, error) {
	c.loginsMu.Lock()
	defer c.loginsMu.Unlock()
	lp := c.logins[id]
	if lp == nil {
		return nil, newError(api.ErrorCodeNotFound, "no login process %q", id)
	}
	return lp, nil
}

func (c *Core) endLogin(id string) {
	c.loginsMu.Lock()
	delete(c.logins, id)
	c.loginsMu.Unlock()
}

// cancelLogins ends every login process, when the core closes.
func (c *Core) cancelLogins() {
	c.loginsMu.Lock()
	ids := make([]string, 0, len(c.logins))
	for id := range c.logins {
		ids = append(ids, id)
	}
	c.loginsMu.Unlock()
	for _, id := range ids {
		_, _ = c.loginCancel(context.Background(), api.LoginProcessParams{ProcessID: id})
	}
}

// advance records the new step of a process and converts it. A finished
// process is forgotten; a step this version of the API cannot express ends
// the process with an unsupported error.
func (c *Core) advance(id string, lp *loginProcess, step *bridgev2.LoginStep) (api.LoginStep, error) {
	lp.step = step
	out := api.LoginStep{ProcessID: id, StepID: step.StepID, Instructions: step.Instructions}
	switch {
	case step.Type == bridgev2.LoginStepTypeUserInput && step.UserInputParams != nil:
		out.Type = api.LoginStepTypeUserInput
		out.Fields = make([]api.LoginField, 0, len(step.UserInputParams.Fields))
		for _, f := range step.UserInputParams.Fields {
			out.Fields = append(out.Fields, api.LoginField{
				FieldID:      f.ID,
				Type:         api.LoginFieldType(f.Type),
				Name:         f.Name,
				Description:  f.Description,
				DefaultValue: f.DefaultValue,
				Pattern:      f.Pattern,
				Options:      f.Options,
			})
		}
		return wordings[lp.network].step(out), nil
	case step.Type == bridgev2.LoginStepTypeDisplayAndWait && step.DisplayAndWaitParams != nil:
		// ImageURL is a Matrix content URI: user interfaces draw the QR
		// code from data instead.
		params := step.DisplayAndWaitParams
		out.Type = api.LoginStepTypeDisplayAndWait
		out.Display = &api.LoginDisplay{Type: api.LoginDisplayType(params.Type), Data: params.Data, CanCancel: params.CanCancel}
		return wordings[lp.network].step(out), nil
	case step.Type == bridgev2.LoginStepTypeComplete && step.CompleteParams != nil:
		c.endLogin(id)
		out.Type = api.LoginStepTypeComplete
		out.AccountID = accountID(lp.network, step.CompleteParams.UserLoginID)
		return out, nil
	default:
		c.endLogin(id)
		lp.process.Cancel()
		return api.LoginStep{}, newError(api.ErrorCodeUnsupported, "login steps of type %q are not supported", step.Type)
	}
}

// networkError reports a failure of a network connector.
func networkError(err error) error {
	var coreErr *api.CoreError
	if errors.As(err, &coreErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	return newError(api.ErrorCodeNetwork, "%v", err)
}
