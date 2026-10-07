// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command testenv starts and stops the local integration test environment
// (Synapse + PostgreSQL) for manual work. The automated tests do not need it:
// they start their own environment.
//
//	go run ./infra/cmd/testenv up              start, print the homeserver URL
//	go run ./infra/cmd/testenv status          print the homeserver URL
//	go run ./infra/cmd/testenv user NAME PASS  create a test user
//	go run ./infra/cmd/testenv down            stop and delete everything
//
// The -project flag selects another Compose project (default musubee-dev).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/quentinemusee/musubee/infra/testenv"
)

const usage = `usage: testenv [-project NAME] up | status | user NAME PASSWORD | down

Starts and stops Musubee's local test environment (Synapse + PostgreSQL).
Requires Docker with the Compose plugin.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("testenv", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprint(flags.Output(), usage) }
	project := flags.String("project", "musubee-dev", "Docker Compose project name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return errors.New("missing command")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	secretsDir, err := secretsDirFor(*project)
	if err != nil {
		return err
	}

	switch cmd := flags.Arg(0); cmd {
	case "up":
		if err := testenv.DockerAvailable(ctx); err != nil {
			return err
		}
		env, err := testenv.Start(ctx, *project, secretsDir)
		if err != nil {
			return err
		}
		fmt.Printf("Synapse is ready.\n  homeserver URL: %s\n  server name:    %s\n", env.HomeserverURL, testenv.ServerName)
		fmt.Printf("Create a user:  go run ./infra/cmd/testenv -project %s user NAME PASSWORD\n", *project)
		fmt.Printf("Stop:           go run ./infra/cmd/testenv -project %s down\n", *project)
		return nil
	case "status":
		env, err := testenv.Attach(ctx, *project, secretsDir)
		if err != nil {
			return err
		}
		fmt.Println(env.HomeserverURL)
		return nil
	case "user":
		if flags.NArg() != 3 {
			return errors.New("usage: testenv user NAME PASSWORD")
		}
		env, err := testenv.Attach(ctx, *project, secretsDir)
		if err != nil {
			return err
		}
		userID, err := env.RegisterUser(ctx, flags.Arg(1), flags.Arg(2), false)
		if err != nil {
			return err
		}
		fmt.Printf("created %s on %s\n", userID, env.HomeserverURL)
		return nil
	case "down":
		if err := testenv.Down(ctx, *project, secretsDir); err != nil {
			return err
		}
		fmt.Println("environment removed")
		return nil
	default:
		flags.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// secretsDirFor keeps the per-project shared secret in infra/.testenv/, which
// Git ignores. The user's cache directory is not used on purpose: on Windows,
// packaged (MSIX) applications see a redirected AppData\Local, so Docker
// Desktop would not find the file there.
func secretsDirFor(project string) (string, error) {
	infraDir, err := testenv.InfraDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(infraDir, ".testenv", project), nil
}
