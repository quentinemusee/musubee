// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package testenv starts and stops Musubee's integration test environment
// (Synapse and PostgreSQL, see infra/compose.test.yml) with Docker Compose,
// and creates throw-away Matrix users on it.
//
// Every environment gets its own Compose project name, a random localhost
// port and a random registration shared secret, so several can run side by
// side and nothing secret is ever stored in the repository.
package testenv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Synapse's shared-secret registration API mandates HMAC-SHA1.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

const (
	// ComposeFileName is the Compose file, relative to the infra directory.
	ComposeFileName = "compose.test.yml"
	// ServerName is the Matrix server name configured in synapse/homeserver.yaml.
	ServerName = "musubee.test"

	secretFileName = "registration_shared_secret"
	secretsDirEnv  = "MUSUBEE_TEST_SECRETS_DIR" //nolint:gosec // The name of an environment variable, not a secret.
	infraDirEnv    = "MUSUBEE_INFRA_DIR"
	waitTimeout    = 180 * time.Second
)

// Env is a running test environment.
type Env struct {
	// Project is the Docker Compose project name.
	Project string
	// HomeserverURL is the base URL of Synapse's client API on localhost.
	HomeserverURL string
	// SecretsDir holds the registration shared secret mounted into Synapse,
	// and the bridge files when Telegram is enabled.
	SecretsDir string
	// Bridge is the Telegram bridge, or nil when it is not enabled.
	Bridge *Bridge

	infraDir     string
	sharedSecret string
	httpClient   *http.Client
}

// InfraDir returns the directory holding compose.test.yml. It honors the
// MUSUBEE_INFRA_DIR environment variable, and otherwise uses the location of
// this source file, which works for "go test" and "go run" in a checkout.
func InfraDir() (string, error) {
	if dir := os.Getenv(infraDirEnv); dir != "" {
		return dir, nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate the infra directory; set %s", infraDirEnv)
	}
	return filepath.Dir(filepath.Dir(file)), nil
}

// DockerAvailable reports whether the Docker daemon and the Compose plugin
// can be reached.
func DockerAvailable(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		return fmt.Errorf("docker daemon not reachable: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, "docker", "compose", "version").CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose not available: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Config describes an environment to start.
type Config struct {
	// Project is the Docker Compose project name.
	Project string
	// SecretsDir receives the per-run secrets and generated files.
	SecretsDir string
	// Telegram, when set, adds the mautrix-telegram bridge connected to
	// Telegram's test environment (compose.telegram.yml).
	Telegram *TelegramOptions
}

// Start creates a new environment (Synapse and PostgreSQL) under the given
// Compose project name; see StartConfig.
func Start(ctx context.Context, project, secretsDir string) (*Env, error) {
	return StartConfig(ctx, Config{Project: project, SecretsDir: secretsDir})
}

// StartConfig creates a new environment and waits until every service is
// healthy. The secrets directory is created if needed and receives a fresh
// random registration shared secret. On error, whatever was started is torn
// down.
func StartConfig(ctx context.Context, cfg Config) (*Env, error) {
	project, secretsDir := cfg.Project, cfg.SecretsDir
	infraDir, err := InfraDir()
	if err != nil {
		return nil, err
	}
	secret, err := newSharedSecret()
	if err != nil {
		return nil, err
	}
	if err := writeSecret(secretsDir, secret); err != nil {
		return nil, err
	}

	env := &Env{
		Project:      project,
		SecretsDir:   secretsDir,
		infraDir:     infraDir,
		sharedSecret: secret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
	if cfg.Telegram != nil {
		if err := env.prepareTelegram(ctx, *cfg.Telegram); err != nil {
			_ = env.Stop(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	if _, err := env.compose(ctx, "up", "--detach", "--wait", "--wait-timeout", strconv.Itoa(int(waitTimeout.Seconds()))); err != nil {
		logs, _ := env.compose(context.WithoutCancel(ctx), "logs", "--no-color", "--tail", "50")
		_ = env.Stop(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("starting the environment: %w\n%s", err, logs)
	}
	out, err := env.compose(ctx, "port", "synapse", "8008")
	if err == nil {
		env.HomeserverURL, err = ParsePublishedPort(out)
	}
	if err != nil {
		_ = env.Stop(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("finding Synapse's published port: %w", err)
	}
	if env.Bridge != nil {
		out, err := env.compose(ctx, "port", "telegram", "29317")
		if err == nil {
			env.Bridge.URL, err = ParsePublishedPort(out)
		}
		if err != nil {
			_ = env.Stop(context.WithoutCancel(ctx))
			return nil, fmt.Errorf("finding the bridge's published port: %w", err)
		}
	}
	return env, nil
}

// Stop removes the containers, networks and volumes of the environment, and
// deletes its secrets directory.
func (e *Env) Stop(ctx context.Context) error {
	_, err := e.compose(ctx, "down", "--volumes", "--remove-orphans", "--timeout", "10")
	if rmErr := os.RemoveAll(e.SecretsDir); rmErr != nil {
		err = errors.Join(err, fmt.Errorf("removing the secrets directory: %w", rmErr))
	}
	return err
}

// Attach returns the Env of a project started earlier (for example by
// "testenv up"), reading its shared secret back from secretsDir.
func Attach(ctx context.Context, project, secretsDir string) (*Env, error) {
	infraDir, err := InfraDir()
	if err != nil {
		return nil, err
	}
	secret, err := os.ReadFile(filepath.Join(secretsDir, secretFileName))
	if err != nil {
		return nil, fmt.Errorf("reading the shared secret (is %s running?): %w", project, err)
	}
	env := &Env{
		Project:      project,
		SecretsDir:   secretsDir,
		infraDir:     infraDir,
		sharedSecret: strings.TrimSpace(string(secret)),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
	out, err := env.compose(ctx, "port", "synapse", "8008")
	if err == nil {
		env.HomeserverURL, err = ParsePublishedPort(out)
	}
	if err != nil {
		return nil, fmt.Errorf("finding Synapse's published port (is %s running?): %w", project, err)
	}
	return env, nil
}

// Down tears down a project started earlier (for example by "testenv up")
// without an Env value.
func Down(ctx context.Context, project, secretsDir string) error {
	infraDir, err := InfraDir()
	if err != nil {
		return err
	}
	env := &Env{Project: project, SecretsDir: secretsDir, infraDir: infraDir}
	return env.Stop(ctx)
}

func (e *Env) compose(ctx context.Context, args ...string) (string, error) {
	full := []string{"compose", "--file", filepath.Join(e.infraDir, ComposeFileName)}
	environ := append(os.Environ(), secretsDirEnv+"="+e.SecretsDir)
	if e.hasTelegram() {
		full = append(full, "--file", filepath.Join(e.infraDir, TelegramComposeFileName))
		environ = append(environ, telegramDirEnv+"="+e.telegramDir())
	}
	full = append(full, "--project-name", e.Project)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Env = environ
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ParsePublishedPort turns the output of "docker compose port" into a base
// URL, for example "127.0.0.1:63422" into "http://127.0.0.1:63422".
func ParsePublishedPort(output string) (string, error) {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	line = strings.TrimSpace(line)
	idx := strings.LastIndex(line, ":")
	if idx <= 0 || idx == len(line)-1 {
		return "", fmt.Errorf("unexpected port output %q", output)
	}
	if _, err := strconv.ParseUint(line[idx+1:], 10, 16); err != nil {
		return "", fmt.Errorf("unexpected port in %q: %w", output, err)
	}
	return "http://" + line, nil
}

// RegistrationMAC computes the HMAC expected by Synapse's shared-secret
// registration admin API.
// See https://element-hq.github.io/synapse/latest/admin_api/register_api.html
func RegistrationMAC(secret, nonce, user, password string, admin bool) string {
	mac := hmac.New(sha1.New, []byte(secret))
	role := "notadmin"
	if admin {
		role = "admin"
	}
	for i, part := range []string{nonce, user, password, role} {
		if i > 0 {
			mac.Write([]byte{0})
		}
		mac.Write([]byte(part))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// RegisterUser creates a user through the shared-secret admin API and returns
// its Matrix ID.
func (e *Env) RegisterUser(ctx context.Context, localpart, password string, admin bool) (id.UserID, error) {
	return e.register(ctx, localpart, password, admin, e.sharedSecret)
}

func (e *Env) register(ctx context.Context, localpart, password string, admin bool, secret string) (id.UserID, error) {
	endpoint := e.HomeserverURL + "/_synapse/admin/v1/register"

	var nonce struct {
		Nonce string `json:"nonce"`
	}
	if err := e.doJSON(ctx, http.MethodGet, endpoint, nil, &nonce); err != nil {
		return "", fmt.Errorf("getting a registration nonce: %w", err)
	}
	body := map[string]any{
		"nonce":    nonce.Nonce,
		"username": localpart,
		"password": password,
		"admin":    admin,
		"mac":      RegistrationMAC(secret, nonce.Nonce, localpart, password, admin),
	}
	var resp struct {
		UserID id.UserID `json:"user_id"`
	}
	if err := e.doJSON(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return "", fmt.Errorf("registering %s: %w", localpart, err)
	}
	return resp.UserID, nil
}

// Login logs a user in with a password and returns a ready Matrix client.
func (e *Env) Login(ctx context.Context, localpart, password string) (*mautrix.Client, error) {
	client, err := mautrix.NewClient(e.HomeserverURL, "", "")
	if err != nil {
		return nil, err
	}
	_, err = client.Login(ctx, &mautrix.ReqLogin{
		Type:                     mautrix.AuthTypePassword,
		Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: localpart},
		Password:                 password,
		InitialDeviceDisplayName: "musubee-testenv",
		StoreCredentials:         true,
	})
	if err != nil {
		return nil, fmt.Errorf("logging in as %s: %w", localpart, err)
	}
	return client, nil
}

func (e *Env) doJSON(ctx context.Context, method, url string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.httpClient.Do(req)
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
	return json.Unmarshal(data, out)
}

// HTTPError is returned when Synapse answers with a non-200 status.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// writeSecret stores the shared secret where the Synapse container can read
// it. Synapse runs as an unprivileged user (UID 991) inside the container, so
// on Linux the bind-mounted directory must be traversable and the file
// readable by others; Docker Desktop on Windows and macOS ignores these modes.
// The secret only protects a throw-away homeserver bound to localhost.
func writeSecret(dir, secret string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // see the function comment
		return fmt.Errorf("creating secrets directory: %w", err)
	}
	// MkdirAll keeps the mode of an existing directory (os.MkdirTemp creates
	// 0700), so set it explicitly.
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // see the function comment
		return fmt.Errorf("making the secrets directory readable by Synapse: %w", err)
	}
	path := filepath.Join(dir, secretFileName)
	if err := os.WriteFile(path, []byte(secret), 0o644); err != nil { //nolint:gosec // see the function comment
		return fmt.Errorf("writing the shared secret: %w", err)
	}
	return os.Chmod(path, 0o644) //nolint:gosec // see the function comment
}

func newSharedSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating the shared secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
