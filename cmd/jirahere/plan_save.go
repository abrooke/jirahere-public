package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/plan"
	"github.com/aslanbrooke/jirahere/internal/quarter"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

const planSaveUsage = "usage: jirahere plan save <day|week|month> --body-file <path|-> [--date YYYY-MM-DD] [--profile <name>]"

var planSaveValueFlags = map[string]bool{"body-file": true, "date": true, "profile": true}

type planSaveArgs struct {
	kind plan.Kind
	body string
	from time.Time
	prof string
}

func runPlanSave(args []string) error {
	pa, err := parsePlanSaveArgs(args, os.Stdin)
	if err != nil {
		return err
	}

	prof := pa.prof
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

	items, err := quarter.RefreshInventory(ctx, newQuarterSearcher(creds), prof)
	if err != nil {
		var unset *quarter.ErrSettingUnset
		if errors.As(err, &unset) {
			return quarterSettingUnsetFailure(unset, prof)
		}
		return failf("jirahere: plan save: %s.", renderJiraError(err))
	}
	epic, found, err := quarter.FindPlanEpic(items, s.Defaults.CurrentQuarter)
	if err != nil {
		return failf("jirahere: plan save: %s.", sanitizeSingleLine(err.Error()))
	}
	if !found {
		return failf("jirahere: plan save: no plan epic (a work item with \"_Plan %s\" in its summary) in the current quarter. Run \"jirahere plan init\" to create one.", s.Defaults.CurrentQuarter)
	}

	summary := planSummary(pa.kind, plan.Window(pa.kind, pa.from).Start)
	var matches []string
	for _, it := range items {
		if it.Summary == summary {
			matches = append(matches, it.Key)
		}
	}
	client := jira.NewClient(creds.BaseURL, creds.AuthHeader)
	switch len(matches) {
	case 0:
		return createPlanItem(ctx, client, s, summary, pa.body, epic.Key)
	case 1:
		return updatePlanItem(ctx, client, matches[0], pa.body, epic.Key)
	}
	return failf("jirahere: plan save: %d work items are titled %q (%s); keep exactly one.",
		len(matches), sanitizeSingleLine(summary), sanitizeSingleLine(strings.Join(matches, ", ")))
}

func planSummary(kind plan.Kind, start time.Time) string {
	k := string(kind)
	return "Plan " + strings.ToUpper(k[:1]) + k[1:] + " " + start.Format("20060102")
}

func createPlanItem(ctx context.Context, client *jira.Client, s *settings.Settings, summary, body, epicKey string) error {
	res, err := command.Create(ctx, client, command.CreateInput{
		ProjectKey:     s.Defaults.Project,
		IssueTypeName:  "Task",
		Summary:        summary,
		Labels:         createLabels(nil, s.Defaults.CurrentQuarter),
		HasParent:      true,
		ParentKey:      epicKey,
		HasDescription: true,
		Description:    body,
	})
	if err != nil {
		return mapCreateError(err)
	}
	fmt.Println(sanitizeSingleLine(res.Key))
	if res.ParentErr != nil {

		return failf("jirahere: plan save: created %s but could not set %s as its parent: %s. Re-run the same plan save to repair the parent.",
			sanitizeSingleLine(res.Key), sanitizeSingleLine(epicKey), renderJiraError(res.ParentErr))
	}
	fmt.Println("Done.")
	return nil
}

func updatePlanItem(ctx context.Context, client *jira.Client, key, body, epicKey string) error {
	issue, err := client.GetIssue(ctx, key)
	if err != nil {
		return failf("jirahere: plan save: could not read %s: %s.", sanitizeSingleLine(key), renderJiraError(err))
	}
	if err := client.SetDescription(ctx, key, adf.FromPlainText(body)); err != nil {
		return failf("jirahere: plan save: could not set the description of %s: %s.", sanitizeSingleLine(key), renderJiraError(err))
	}
	if issue.Fields.Parent == nil {
		if err := client.SetParent(ctx, key, epicKey); err != nil {
			return failf("jirahere: plan save: could not set %s as the parent of %s: %s.",
				sanitizeSingleLine(epicKey), sanitizeSingleLine(key), renderJiraError(err))
		}
	}
	fmt.Printf("Plan saved: %s (updated)\n", sanitizeSingleLine(key))
	return nil
}

func parsePlanSaveArgs(args []string, stdin io.Reader) (planSaveArgs, error) {
	if hasHelpToken(args) {
		args = []string{"--help"}
	}
	positional, flagArgs := splitPositionalFlagArgs(args, planSaveValueFlags)

	fs := flag.NewFlagSet("plan save", flag.ContinueOnError)
	bodyFile := fs.String("body-file", "", "read the plan body from `path`, or from stdin when \"-\" (required; there is no inline body flag, file/stdin is the safe path)")
	date := fs.String("date", "", "window start date, YYYY-MM-DD (default today)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<day|week|month>"}}); err != nil {
		return planSaveArgs{}, err
	}
	positional = append(positional, fs.Args()...)
	prof, err := resolveProfile()
	if err != nil {
		return planSaveArgs{}, err
	}

	switch {
	case len(positional) == 0:
		return planSaveArgs{}, usagef("jirahere: plan save: a kind (day, week or month) is required. %s", planSaveUsage)
	case len(positional) > 1:
		return planSaveArgs{}, usagef("jirahere: plan save: unexpected argument %q. %s", positional[1], planSaveUsage)
	}
	var kind plan.Kind
	switch plan.Kind(positional[0]) {
	case plan.Day, plan.Week, plan.Month:
		kind = plan.Kind(positional[0])
	default:
		return planSaveArgs{}, usagef("jirahere: plan save: unknown kind %q: want day, week or month. %s", positional[0], planSaveUsage)
	}

	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if !visited["body-file"] {
		return planSaveArgs{}, usagef("jirahere: plan save: --body-file is required. %s", planSaveUsage)
	}
	start := time.Now()
	if visited["date"] {
		t, err := plan.ParseFrom(*date)
		if err != nil {
			return planSaveArgs{}, usagef("jirahere: plan save: --date: %s.", err)
		}
		start = t
	}

	body, err := command.ReadBodyFile(*bodyFile, stdin)
	if err != nil {
		return planSaveArgs{}, failf("jirahere: plan save: %s.", sanitizeSingleLine(err.Error()))
	}

	return planSaveArgs{kind: kind, body: body, from: start, prof: prof}, nil
}
