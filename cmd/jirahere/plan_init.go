package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

const planEpicIssueType = "Epic"

func runPlanInit(args []string) error {
	if hasHelpToken(args) {
		args = []string{"--help"}
	}
	fs := flag.NewFlagSet("plan init", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print what would be created without writing anything")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(planInitUsage)
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	if prof != "" {
		if _, err := auth.LoadProvider(prof); errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(prof))
		}
	}
	s, err := settings.Load(prof)
	if err != nil {
		return failf("jirahere: could not load settings: %s.", err)
	}
	if s.Defaults.Project == "" {
		return quarterSettingUnsetFailure(&quarter.ErrSettingUnset{Setting: "defaults.project"}, prof)
	}
	if s.Defaults.CurrentQuarter == "" {
		return quarterSettingUnsetFailure(&quarter.ErrSettingUnset{Setting: "defaults.current_quarter"}, prof)
	}
	project, quarterLabel := s.Defaults.Project, s.Defaults.CurrentQuarter

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

	items, err := quarter.FreshInventory(ctx, newQuarterSearcher(creds), prof)
	if err != nil {
		var unset *quarter.ErrSettingUnset
		if errors.As(err, &unset) {
			return quarterSettingUnsetFailure(unset, prof)
		}
		return failf("jirahere: plan init: could not list the quarter's work items: %s.", renderJiraError(err))
	}

	epic, found, err := quarter.FindPlanEpic(items, quarterLabel)
	if err != nil {

		return failf("jirahere: plan init: %s. Nothing was created.", err)
	}
	if found {
		fmt.Printf("Plan epic exists: %s\n", sanitizeSingleLine(epic.Key))
		return nil
	}

	summary := "_Plan " + quarterLabel
	labels := createLabels(nil, quarterLabel)
	if *dryRun {
		fmt.Printf("Dry run: would create a new %s in %s with summary %q.\n",
			planEpicIssueType, sanitizeSingleLine(project), sanitizeSingleLine(summary))
		fmt.Println(createLabelsLine(labels, quarterLabel, true))
		fmt.Println("Nothing was created.")
		return nil
	}

	client := jira.NewClient(creds.BaseURL, creds.AuthHeader)
	res, err := command.Create(ctx, client, command.CreateInput{
		ProjectKey:    project,
		IssueTypeName: planEpicIssueType,
		Summary:       summary,
		Labels:        labels,
	})
	if err != nil {
		return mapCreateError(err)
	}
	printCreateResult(project, planEpicIssueType, res, labels, quarterLabel, true)
	return nil
}
