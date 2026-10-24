package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

const statusRecursiveHelpNote = "--dry-run prints what status recursive would set and makes no changes.\nIt does not check transition reachability: a real run can still fail on a\nnode whose workflow has no direct transition to the computed status."

const statusRecursiveUsage = "usage: jirahere status recursive <id> [--dry-run] [--profile <name>]"

var statusRecursiveValueFlags = map[string]bool{"profile": true}

func runStatusRecursive(args []string) error {
	positional, flagArgs := splitPositionalFlagArgs(args, statusRecursiveValueFlags)

	fs := flag.NewFlagSet("status recursive", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "report what would change without transitioning anything")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return err
	}
	if fs.NArg() != 0 || len(positional) != 1 {
		return usagef(statusRecursiveUsage)
	}
	id := positional[0]
	if id == "" || moveIssueKey(id) != id {
		return usagef("jirahere: status recursive: <id> must be a well-formed Jira issue key")
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}

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
	return statusRecursive(ctx, jira.NewClient(creds.BaseURL, creds.AuthHeader), os.Stdout, id, *dryRun)
}

type statusRecursiveClient interface {
	jira.IssueReader
	jira.TransitionService
	WalkDescendants(ctx context.Context, rootKey string) (*jira.DescendantWalkResult, error)
}

func statusRecursive(ctx context.Context, client statusRecursiveClient, out io.Writer, id string, dryRun bool) error {
	root, err := client.GetIssue(ctx, id)
	if err != nil {
		return failf("jirahere: status recursive could not read %q: %s.", id, renderJiraError(err))
	}
	walk, err := client.WalkDescendants(ctx, id)
	if err != nil {
		return failf("jirahere: status recursive could not walk the descendants of %q: %s.", id, renderJiraError(err))
	}
	if len(walk.Failures) > 0 {
		f := walk.Failures[0]
		return failf("jirahere: status recursive could not list the children of %q: %s.", f.Key, renderJiraError(f.Err))
	}

	header := "Rolling up %s ...\n"
	if dryRun {
		header = "Rolling up %s (dry run) ...\n"
	}
	if _, err := fmt.Fprintf(out, header, sanitizeSingleLine(id)); err != nil {
		return err
	}
	for _, r := range command.EvaluateRollup(id, root.Fields.Status.Name, walk.Nodes) {
		key := sanitizeSingleLine(r.Key)
		var line string
		switch {
		case !r.Matched:
			line = fmt.Sprintf("No rule matched %s.", key)
		case r.Target == r.Current:
			line = fmt.Sprintf("Unchanged %s (%s).", key, sanitizeSingleLine(r.Current))
		case dryRun:
			line = fmt.Sprintf("Would set: %s %s -> %s", key, sanitizeSingleLine(r.Current), sanitizeSingleLine(r.Target))
		default:
			if err := transitionByName(ctx, client, "status recursive", r.Key, r.Target); err != nil {
				return err
			}
			line = fmt.Sprintf("Status set: %s -> %s", key, sanitizeSingleLine(r.Target))
		}
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(out, "Done.")
	return err
}
