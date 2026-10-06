package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

const quarterListUsage = "usage: jirahere quarter list [--json] [--previous | --next | --label <value>] [--profile <name>]"

func quarterSettingUnsetFailure(unset *quarter.ErrSettingUnset, prof string) error {
	flagName := map[string]string{
		"defaults.project":          "--project",
		"defaults.current_quarter":  "--current-quarter",
		"defaults.previous_quarter": "--previous-quarter",
		"defaults.next_quarter":     "--next-quarter",
	}[unset.Setting]
	placeholder := "<label>"
	if flagName == "--project" {
		placeholder = "<key>"
	}
	remediation := ""
	if flagName != "" {
		remediation = configureSetRemediation(prof, flagName, placeholder)
	}
	return failf("jirahere: %s%s.", unset, remediation)
}

var newQuarterSearcher = func(creds auth.Credentials) quarter.Searcher {
	return jira.NewClient(creds.BaseURL, creds.AuthHeader)
}

type quarterListDoc struct {
	Items []quarterListItem `json:"items"`
}

type quarterListItem struct {
	Key      string    `json:"key"`
	Summary  string    `json:"summary"`
	Status   string    `json:"status"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Resolved *string   `json:"resolved"`
	Assignee string    `json:"assignee"`
}

func runQuarter(args []string) error {
	if hasHelpToken(args) {
		return runQuarterList([]string{"--help"})
	}
	if len(args) == 0 || args[0] != "list" {
		return usagef(quarterListUsage)
	}
	return runQuarterList(args[1:])
}

func runQuarterList(args []string) error {
	fs := flag.NewFlagSet("quarter list", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "render JSON output")
	previous := fs.Bool("previous", false, "list settings.json's defaults.previous_quarter instead of defaults.current_quarter")
	next := fs.Bool("next", false, "list settings.json's defaults.next_quarter instead of defaults.current_quarter")
	label := fs.String("label", "", "list this quarter label verbatim instead of defaults.current_quarter (accepted as-is, not validated)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(quarterListUsage)
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	var previousGiven, nextGiven, labelGiven bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "previous":
			previousGiven = true
		case "next":
			nextGiven = true
		case "label":
			labelGiven = true
		}
	})

	previousGiven = previousGiven && *previous
	nextGiven = nextGiven && *next
	givenCount := 0
	for _, g := range []bool{previousGiven, nextGiven, labelGiven} {
		if g {
			givenCount++
		}
	}
	if givenCount > 1 {
		return usagef(quarterListUsage)
	}
	if labelGiven && *label == "" {
		return usagef("jirahere: --label was given but is empty; an empty value is not allowed.")
	}

	s, err := settings.Load(prof)
	if err != nil {
		return failf("jirahere: could not load settings: %s.", err)
	}

	quarterFlagGiven := previousGiven || nextGiven || labelGiven
	var resolvedLabel string
	switch {
	case previousGiven:
		if s.Defaults.PreviousQuarter == "" {
			return quarterSettingUnsetFailure(&quarter.ErrSettingUnset{Setting: "defaults.previous_quarter"}, prof)
		}
		resolvedLabel = s.Defaults.PreviousQuarter
	case nextGiven:
		if s.Defaults.NextQuarter == "" {
			return quarterSettingUnsetFailure(&quarter.ErrSettingUnset{Setting: "defaults.next_quarter"}, prof)
		}
		resolvedLabel = s.Defaults.NextQuarter
	case labelGiven:
		resolvedLabel = *label
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
	client := newQuarterSearcher(creds)

	var issues []quarter.QuarterIssue
	if quarterFlagGiven {
		issues, err = quarter.InventoryForQuarter(ctx, client, resolvedLabel, prof)
	} else {
		issues, err = quarter.Inventory(ctx, client, prof)
	}
	if err != nil {
		var unset *quarter.ErrSettingUnset
		if errors.As(err, &unset) {

			return quarterSettingUnsetFailure(unset, prof)
		}

		return failf("jirahere: quarter list failed: %s.", renderJiraError(err))
	}

	if *jsonOutput {
		return renderQuarterListJSON(issues)
	}
	displayLabel := s.Defaults.CurrentQuarter
	if quarterFlagGiven {
		displayLabel = resolvedLabel
	}
	return renderQuarterListText(issues, displayLabel)
}

func renderQuarterListText(issues []quarter.QuarterIssue, quarterLabel string) error {
	if len(issues) == 0 {
		quarterName := "the current quarter"
		if quarterLabel != "" {
			quarterName = sanitizeContextField(quarterLabel)
		}
		_, err := fmt.Printf("no work items in %s\n", quarterName)
		return err
	}
	for _, it := range issues {
		resolved := "-"
		if it.Resolved != nil {
			resolved = it.Resolved.Format(time.RFC3339)
		}
		if _, err := fmt.Printf(
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			sanitizeContextField(it.Key),
			sanitizeContextField(it.Summary),
			it.Created.Format(time.RFC3339),
			it.Updated.Format(time.RFC3339),
			sanitizeContextField(it.Status.Name),
			resolved,
			assigneeOrUnassigned(sanitizeContextField(it.Assignee)),
		); err != nil {
			return err
		}
	}
	return nil
}

func renderQuarterListJSON(issues []quarter.QuarterIssue) error {
	items := make([]quarterListItem, 0, len(issues))
	for _, it := range issues {
		var resolved *string
		if it.Resolved != nil {
			s := it.Resolved.Format(time.RFC3339)
			resolved = &s
		}
		items = append(items, quarterListItem{
			Key:      sanitizeContextField(it.Key),
			Summary:  sanitizeContextField(it.Summary),
			Status:   sanitizeContextField(it.Status.Name),
			Created:  it.Created,
			Updated:  it.Updated,
			Resolved: resolved,
			Assignee: assigneeOrUnassigned(sanitizeContextField(it.Assignee)),
		})
	}
	b, _ := json.MarshalIndent(quarterListDoc{Items: items}, "", "  ")
	_, err := fmt.Println(string(b))
	return err
}
