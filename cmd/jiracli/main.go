// Command jiracli exercises jiraclient against a live Jira site. Each subcommand runs one fixed
// action on the ticket it is given, so a feature can be checked by hand before it is relied on.
//
// The client is configured from configs.Jira, read from the environment:
//
//	JIRA_BASE_URL  the site URL, such as https://my-domain.atlassian.net
//	JIRA_EMAIL     the account the API token belongs to
//	JIRA_API_KEY   the API token
//	JIRA_TIMEOUT   optional, the HTTP timeout (default 30s)
//
// Usage:
//
//	go run ./cmd/jiracli comment INIT-1
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"

	"github.com/project-init/gommon/pkg/cfg"
	"github.com/project-init/gommon/pkg/cfg/configs"
	"github.com/project-init/gommon/pkg/jiraclient"
)

// command is one subcommand. Add an entry to commands to test a new client feature.
type command struct {
	usage string
	run   func(ctx context.Context, client *jiraclient.Client, args []string) error
}

var commands = map[string]command{
	"comment": {
		usage: "comment <ISSUE-KEY>  leave a test comment on the issue",
		run:   addComment,
	},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "jiracli:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q\n%w", args[0], usageError())
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return cmd.run(ctx, client, args[1:])
}

func newClient() (*jiraclient.Client, error) {
	var config configs.Jira
	if err := cfg.LoadConfigs(&config, cfg.NewEnvOption()); err != nil {
		return nil, fmt.Errorf("load Jira config: %w", err)
	}
	if config.BaseURL == "" || config.Email == "" || config.APIKey == "" {
		return nil, errors.New("JIRA_BASE_URL, JIRA_EMAIL and JIRA_API_KEY must be set")
	}

	httpClient := &http.Client{Timeout: config.Timeout}
	return jiraclient.NewClient(httpClient, config.BaseURL, config.Email, config.APIKey), nil
}

func usageError() error {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	message := "usage: jiracli <command> [args]\n\ncommands:"
	for _, name := range names {
		message += "\n  " + commands[name].usage
	}
	return errors.New(message)
}

// issueKey returns the single issue key a command takes.
func issueKey(args []string) (string, error) {
	if len(args) != 1 || args[0] == "" {
		return "", errors.New("expected exactly one issue key, such as INIT-1")
	}
	return args[0], nil
}

func addComment(ctx context.Context, client *jiraclient.Client, args []string) error {
	key, err := issueKey(args)
	if err != nil {
		return err
	}

	comment, err := client.AddSimpleComment(ctx, key, "Comment by jiraclient.")
	if err != nil {
		return fmt.Errorf("comment on %s: %w", key, err)
	}

	fmt.Printf("added comment %s to %s\n", comment.ID, key)
	return nil
}
