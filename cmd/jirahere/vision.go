package main

import (
	"context"
	"errors"
	"flag"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

const visionUsage = "usage: jirahere vision [--json|--md] [--profile <name>]"

func runVision(args []string) error {
	if hasHelpToken(args) {

		args = []string{"--help"}
	}
	fs := flag.NewFlagSet("vision", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "render JSON output")
	fs.Bool("md", false, "render Markdown output (default)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(visionUsage)
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["json"] && visited["md"] {
		return usagef("jirahere: vision: --json and --md may not be used together.")
	}
	format := "md"
	if *jsonOutput {
		format = "json"
	}

	s, err := settings.Load(prof)
	if err != nil {
		return failf("jirahere: could not load settings: %s.", err)
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

	items, err := quarter.Inventory(ctx, newQuarterSearcher(creds), prof)
	if err != nil {
		var unset *quarter.ErrSettingUnset
		if errors.As(err, &unset) {
			return quarterSettingUnsetFailure(unset, prof)
		}
		return failf("jirahere: vision: could not list the quarter's work items: %s.", renderJiraError(err))
	}

	epic, found, err := quarter.FindVisionEpic(items)
	if err != nil {

		return failf("jirahere: vision: %s.", err)
	}
	if !found {
		return failf("jirahere: vision: no Vision epic found for quarter %s: no work item title contains %q. Create one whose title contains %q (e.g. \"_Vision Q1'26\") and carries the quarter label.",
			s.Defaults.CurrentQuarter, "_Vision", "_Vision")
	}

	var client jira.AccessStrategy = jira.NewClient(creds.BaseURL, creds.AuthHeader)
	children, err := client.ChildrenOf(ctx, epic.Key)
	if err != nil {
		return failf("jirahere: vision: could not list children of Vision epic %s: %s.", epic.Key, renderJiraError(err))
	}
	active, ok := quarter.SelectActiveVisionItem(children)
	if !ok {
		return failf("jirahere: vision: no active Vision item: Vision epic %s has no non-Done child.", epic.Key)
	}

	issue, err := client.GetIssue(ctx, active.Key)
	if err != nil {
		if jira.IsNotFound(err) {
			return failf("jirahere: vision: active Vision item %s was not found (404).", active.Key)
		}
		return failf("jirahere: vision: could not read active Vision item %s: %s.", active.Key, renderJiraError(err))
	}
	return renderContextGetOutput(newContextGetOutput(issue), format)
}
