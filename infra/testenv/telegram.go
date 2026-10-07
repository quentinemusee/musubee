// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package testenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"maunium.net/go/mautrix/id"
)

const (
	// TelegramComposeFileName is the overlay adding the Telegram bridge.
	TelegramComposeFileName = "compose.telegram.yml"

	telegramDirEnv           = "MUSUBEE_TEST_TELEGRAM_DIR"
	telegramDirName          = "telegram"
	telegramConfigTmpl       = "config.yaml.tmpl"
	telegramRegistrationTmpl = "registration.yaml.tmpl"
)

// TelegramOptions enables the mautrix-telegram bridge, connected to Telegram's
// test environment with the given application credentials.
type TelegramOptions struct {
	APIID   int
	APIHash string
}

// Bridge talks to the provisioning API of a running bridge.
type Bridge struct {
	// URL is the base URL of the bridge's appservice on localhost.
	URL string

	secret     string
	httpClient *http.Client
}

// TelegramSecrets are the per-run secrets shared by the bridge configuration
// and its appservice registration.
type TelegramSecrets struct {
	ASToken            string
	HSToken            string
	SenderLocalpart    string
	ProvisioningSecret string
}

func newTelegramSecrets() (TelegramSecrets, error) {
	var s TelegramSecrets
	for _, field := range []*string{&s.ASToken, &s.HSToken, &s.SenderLocalpart, &s.ProvisioningSecret} {
		value, err := newSharedSecret()
		if err != nil {
			return s, err
		}
		*field = value
	}
	return s, nil
}

type telegramTemplateData struct {
	TelegramOptions
	TelegramSecrets
}

// RenderTelegramFiles renders the bridge configuration and its appservice
// registration from infra/telegram/*.tmpl.
func RenderTelegramFiles(infraDir string, opts TelegramOptions, secrets TelegramSecrets) (config, registration []byte, err error) {
	data := telegramTemplateData{opts, secrets}
	config, err = renderTemplate(filepath.Join(infraDir, telegramDirName, telegramConfigTmpl), data)
	if err != nil {
		return nil, nil, err
	}
	registration, err = renderTemplate(filepath.Join(infraDir, telegramDirName, telegramRegistrationTmpl), data)
	return config, registration, err
}

func renderTemplate(path string, data any) ([]byte, error) {
	tmpl, err := template.New(filepath.Base(path)).Option("missingkey=error").ParseFiles(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", filepath.Base(path), err)
	}
	return buf.Bytes(), nil
}

// prepareTelegram writes the bridge configuration and appservice
// registration (with random tokens) before Synapse starts and loads the
// registration, and builds the bridge image.
func (e *Env) prepareTelegram(ctx context.Context, opts TelegramOptions) error {
	if opts.APIID == 0 || opts.APIHash == "" {
		return fmt.Errorf("telegram: api_id and api_hash are required")
	}
	secrets, err := newTelegramSecrets()
	if err != nil {
		return err
	}
	config, registration, err := RenderTelegramFiles(e.infraDir, opts, secrets)
	if err != nil {
		return err
	}
	dir := e.telegramDir()
	// The bridge (root in its container) and Synapse (UID 991) read these
	// files through bind mounts.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // throw-away test files, read by containers
		return err
	}
	for name, content := range map[string][]byte{"config.yaml": config, "registration.yaml": registration} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil { //nolint:gosec // see above
			return err
		}
	}
	if err := e.writeSynapseConfigWithAppservice(dir); err != nil {
		return err
	}
	if _, err := e.compose(ctx, "build", "--quiet", "telegram"); err != nil {
		return fmt.Errorf("building the bridge image: %w", err)
	}
	e.Bridge = &Bridge{secret: secrets.ProvisioningSecret, httpClient: &http.Client{Timeout: 2 * time.Minute}}
	return nil
}

// writeSynapseConfigWithAppservice copies infra/synapse/ into
// dir/synapse-config and adds the appservice fragment. Docker cannot add a
// file inside the read-only /config bind mount, so the overlay mounts this
// copy at /config instead.
func (e *Env) writeSynapseConfigWithAppservice(dir string) error {
	target := filepath.Join(dir, "synapse-config")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(target, 0o755); err != nil { //nolint:gosec // read by the Synapse container
		return err
	}
	sources := map[string]string{
		"homeserver.yaml":          filepath.Join(e.infraDir, "synapse", "homeserver.yaml"),
		"log.config":               filepath.Join(e.infraDir, "synapse", "log.config"),
		"appservice-telegram.yaml": filepath.Join(e.infraDir, telegramDirName, "synapse-appservice.yaml"),
	}
	for name, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(target, name), data, 0o644); err != nil { //nolint:gosec // read by the Synapse container
			return err
		}
	}
	return nil
}

func (e *Env) telegramDir() string {
	return filepath.Join(e.SecretsDir, telegramDirName)
}

func (e *Env) hasTelegram() bool {
	_, err := os.Stat(e.telegramDir())
	return err == nil
}

// LoginStep is a step of a bridge login (bridgev2 provisioning API v3).
type LoginStep struct {
	LoginID        string `json:"login_id"`
	Type           string `json:"type"`
	StepID         string `json:"step_id"`
	Instructions   string `json:"instructions"`
	DisplayAndWait *struct {
		Type string `json:"type"`
		Data string `json:"data"`
	} `json:"display_and_wait,omitempty"`
	Complete *struct {
		UserLoginID string `json:"user_login_id"`
	} `json:"complete,omitempty"`
}

// LoginFlow is a login method offered by the bridge.
type LoginFlow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LoginFlows lists the bridge's login methods.
func (b *Bridge) LoginFlows(ctx context.Context, user id.UserID) ([]LoginFlow, error) {
	var resp struct {
		Flows []LoginFlow `json:"flows"`
	}
	err := b.call(ctx, http.MethodGet, "/v3/login/flows", user, nil, &resp)
	return resp.Flows, err
}

// StartLogin starts a login with the given flow for a Matrix user.
func (b *Bridge) StartLogin(ctx context.Context, user id.UserID, flowID string) (*LoginStep, error) {
	var step LoginStep
	err := b.call(ctx, http.MethodPost, "/v3/login/start/"+url.PathEscape(flowID), user, map[string]any{}, &step)
	return &step, err
}

// WaitStep waits for a display-and-wait step to finish and returns the next
// step.
func (b *Bridge) WaitStep(ctx context.Context, user id.UserID, step *LoginStep) (*LoginStep, error) {
	var next LoginStep
	path := fmt.Sprintf("/v3/login/step/%s/%s/display_and_wait", url.PathEscape(step.LoginID), url.PathEscape(step.StepID))
	err := b.call(ctx, http.MethodPost, path, user, map[string]any{}, &next)
	if next.LoginID == "" {
		next.LoginID = step.LoginID
	}
	return &next, err
}

// CancelLogin abandons a login in progress.
func (b *Bridge) CancelLogin(ctx context.Context, user id.UserID, loginID string) error {
	return b.call(ctx, http.MethodPost, "/v3/login/cancel/"+url.PathEscape(loginID), user, map[string]any{}, nil)
}

// Logout logs a user's bridge login out of Telegram.
func (b *Bridge) Logout(ctx context.Context, user id.UserID, userLoginID string) error {
	return b.call(ctx, http.MethodPost, "/v3/logout/"+url.PathEscape(userLoginID), user, map[string]any{}, nil)
}

func (b *Bridge) call(ctx context.Context, method, path string, user id.UserID, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	endpoint := b.URL + "/_matrix/provision" + path + "?user_id=" + url.QueryEscape(string(user))
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.secret)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
