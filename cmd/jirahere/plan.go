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

const planUsage = "usage: jirahere plan [--json|--md] [--profile <name>]"

const planInitUsage = "usage: jirahere plan init [--dry-run] [--profile <name>]"

func runPlan(args []string) error {
	if len(args) > 0 && args[0] == "init" {
		return runPlanInit(args[1:])
	}
	if len(args) > 0 && args[0] == "context" {
		return runPlanContext(args[1:])
	}
	if len(args) > 0 && args[0] == "save" {
		return runPlanSave(args[1:])
	}
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		return usagef(planUsage)
	}
	return runPlanShow(args)
}

func runPlanShow(args []string) error {
	if hasHelpToken(args) {
		args = []string{"--help"}
	}
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "render JSON output")
	fs.Bool("md", false, "render Markdown output (default)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(planUsage)
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["json"] && visited["md"] {
		return usagef("jirahere: plan: --json and --md may not be used together.")
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
		return failf("jirahere: plan: could not list the quarter's work items: %s.", renderJiraError(err))
	}

	epic, found, err := quarter.FindPlanEpic(items, s.Defaults.CurrentQuarter)
	if err != nil {

		return failf("jirahere: plan: %s.", err)
	}
	if !found {
		return failf("jirahere: plan: no plan epic found for quarter %s: no work item title contains %q. Run \"jirahere plan init\" to create one.",
			s.Defaults.CurrentQuarter, "_Plan "+s.Defaults.CurrentQuarter)
	}

	client := jira.NewClient(creds.BaseURL, creds.AuthHeader)
	issue, err := client.GetIssue(ctx, epic.Key)
	if err != nil {
		if jira.IsNotFound(err) {
			return failf("jirahere: plan: plan epic %s was not found (404).", epic.Key)
		}
		return failf("jirahere: plan: could not read plan epic %s: %s.", epic.Key, renderJiraError(err))
	}
	return renderContextGetOutput(newContextGetOutput(issue), format)
}
