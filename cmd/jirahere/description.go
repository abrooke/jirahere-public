package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

const descriptionGetUsage = "usage: jirahere description get <id> [--json|--md] [--profile <name>]"
const descriptionSetUsage = "usage: jirahere description set <id> (--body <text> | --body-file <path|->) [--profile <name>]"

var descriptionGetValueFlags = map[string]bool{"profile": true}
var descriptionSetValueFlags = map[string]bool{"body": true, "body-file": true, "profile": true}

func runDescription(args []string) error {
	if hasHelpToken(args) {
		if len(args) > 0 && args[0] == "set" {
			return runDescriptionSet([]string{"--help"})
		}
		return runDescriptionGet([]string{"--help"})
	}
	if len(args) > 0 {
		switch args[0] {
		case "get":
			return runDescriptionGet(args[1:])
		case "set":
			return runDescriptionSet(args[1:])
		}
	}
	return usagef("usage: jirahere description <get|set> ...")
}

func runDescriptionSet(args []string) error {
	positional, flagArgs := splitPositionalFlagArgs(args, descriptionSetValueFlags)

	fs := flag.NewFlagSet("description set", flag.ContinueOnError)
	body := fs.String("body", "", "new description text; replaces the whole description (give exactly one of --body / --body-file)")
	bodyFile := fs.String("body-file", "", "read the new description text from `path`, or from stdin when \"-\" (give exactly one of --body / --body-file)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<id>"}}); err != nil {
		return err
	}
	if fs.NArg() != 0 || len(positional) != 1 || positional[0] == "" {
		return usagef(descriptionSetUsage)
	}
	var bodySet, bodyFileSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "body":
			bodySet = true
		case "body-file":
			bodyFileSet = true
		}
	})
	text := *body
	switch {
	case bodySet && bodyFileSet:
		return usagef("jirahere: description set: --body and --body-file may not be used together.")
	case bodyFileSet:
		read, err := command.ReadBodyFile(*bodyFile, os.Stdin)
		if err != nil {
			return failf("jirahere: description set: %s.", sanitizeSingleLine(err.Error()))
		}
		text = read
	case strings.TrimSpace(text) == "":
		return usagef("jirahere: description set: --body is required and must not be empty or whitespace-only.")
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	id := positional[0]

	provider, err := auth.LoadProvider(prof)
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(prof))
		}
		return failf("jirahere: could not load Jira credentials. Run \"%s\" again.", loginRemediation(prof))
	}
	ctx := context.Background()
	creds, err := provider.Credentials(ctx)
	if err != nil {
		return refreshCredentialsFailureFor(err, prof)
	}
	var client jira.IssueWriter = jira.NewClient(creds.BaseURL, creds.AuthHeader)
	if err := client.SetDescription(ctx, id, adf.FromPlainText(text)); err != nil {
		return failf("jirahere: description set failed: %s.", renderJiraError(err))
	}
	_, err = fmt.Printf("Description set: %s\n", sanitizeSingleLine(id))
	return err
}

func runDescriptionGet(args []string) error {
	var positional []string
	var flagArgs []string
	if hasHelpToken(args) {
		flagArgs = []string{"--help"}
	} else {
		positional, flagArgs = splitPositionalFlagArgs(args, descriptionGetValueFlags)
	}

	fs := flag.NewFlagSet("description get", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "render JSON output: {\"key\", \"description\"}")
	mdOutput := fs.Bool("md", false, "render Markdown output: key header plus a Description section")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<id>"}}); err != nil {
		return err
	}
	if fs.NArg() != 0 || len(positional) != 1 || positional[0] == "" {
		return usagef(descriptionGetUsage)
	}
	if *jsonOutput && *mdOutput {
		return usagef("jirahere: description get: --json and --md may not be used together")
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	key := positional[0]

	provider, err := auth.LoadProvider(prof)
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(prof))
		}
		return failf("jirahere: could not load Jira credentials. Run \"%s\" again.", loginRemediation(prof))
	}
	ctx := context.Background()
	creds, err := provider.Credentials(ctx)
	if err != nil {
		return refreshCredentialsFailureFor(err, prof)
	}
	var client jira.IssueReader = jira.NewClient(creds.BaseURL, creds.AuthHeader)
	issue, err := client.GetIssue(ctx, key)
	if err != nil {
		return failf("jirahere: description get failed: %s.", renderJiraError(err))
	}
	text := adf.PlainText(issue.Fields.Description)

	if issue.Key != "" {
		key = issue.Key
	}
	if *jsonOutput {
		b, err := json.MarshalIndent(struct {
			Key         string `json:"key"`
			Description string `json:"description"`
		}{key, text}, "", "  ")
		if err != nil {
			return failf("jirahere: description get failed: %v.", err)
		}
		_, err = fmt.Println(string(b))
		return err
	}
	if *mdOutput {
		_, err = fmt.Printf("# %s\n\n## Description\n\n%s\n", sanitizeSingleLine(key), text)
		return err
	}
	if text == "" {
		return nil
	}
	_, err = fmt.Println(text)
	return err
}
