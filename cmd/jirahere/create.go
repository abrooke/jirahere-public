package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"unicode"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

type labelListFlag []string

func (l *labelListFlag) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, ",")
}

func (l *labelListFlag) Set(v string) error {
	*l = append(*l, v)
	return nil
}

func labelHasWhitespace(label string) bool {
	return strings.ContainsFunc(label, unicode.IsSpace)
}

func configureSetRemediation(prof, flag, placeholder string) string {
	if prof == "" {
		return ""
	}
	return ` (run "jirahere configure set --profile ` + sanitizeSingleLine(prof) + ` ` + flag + ` ` + placeholder + `")`
}

func resolveProject(flagProject string, s *settings.Settings, prof string) (string, error) {
	if flagProject != "" {
		return flagProject, nil
	}
	if s != nil && s.Defaults.Project != "" {
		return s.Defaults.Project, nil
	}
	return "", usagef("jirahere: no project given and no default project configured; pass --project or set defaults.project%s", configureSetRemediation(prof, "--project", "<key>"))
}

func resolveIssueType(flagIssueType string, s *settings.Settings, prof string) (string, error) {
	if flagIssueType != "" {
		return flagIssueType, nil
	}
	if s != nil && s.Defaults.IssueType != "" {
		return s.Defaults.IssueType, nil
	}
	return "", usagef("jirahere: no issue type given and no default issue type configured; pass --type or set defaults.issue_type%s", configureSetRemediation(prof, "--type", "<name>"))
}

func createLabels(userLabels []string, currentQuarter string) []string {
	labels := make([]string, 0, len(userLabels)+1)
	seen := make(map[string]struct{}, len(userLabels)+1)
	for _, label := range userLabels {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	if currentQuarter != "" {
		if _, ok := seen[currentQuarter]; !ok {
			labels = append(labels, currentQuarter)
		}
	}
	return labels
}

const createUsage = "usage: jirahere create --summary <text> [--project <key>] [--type <name>] [--description <text>] [--parent <key>] [--label <label>]... [--suppress-auto-quarter] [--profile <name>]"

func runCreate(args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	summary := fs.String("summary", "", "summary/title for the new work item (required, non-empty)")
	project := fs.String("project", "", "project key to create the work item in (falls back to settings.json's defaults.project)")
	issueType := fs.String("type", "", "issue type name for the new work item (falls back to settings.json's defaults.issue_type)")
	description := fs.String("description", "", "plain-text description for the new work item (blank line = new paragraph, single newline = hard break; no markdown)")
	parent := fs.String("parent", "", "key of the work item to set as the new item's parent")
	var labels labelListFlag
	fs.Var(&labels, "label", "label to add to the new work item; repeatable, each value non-empty and whitespace-free. create is not idempotent: re-running it creates another work item")
	suppressAutoQuarter := fs.Bool("suppress-auto-quarter", false, "do not auto-add settings.json's defaults.current_quarter to the label list; pass the target quarter's label explicitly via --label instead")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}

	if fs.NArg() != 0 {
		return usagef(createUsage)
	}

	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	if *summary == "" {
		return usagef("jirahere: --summary is required and must not be empty.")
	}

	for _, label := range labels {
		if label == "" {
			return usagef("jirahere: --label must not be empty.")
		}
		if labelHasWhitespace(label) {
			return usagef("jirahere: %q is not a valid Jira label (labels can't contain whitespace).", label)
		}
	}

	descriptionGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "description" {
			descriptionGiven = true
		}
	})
	if descriptionGiven && *description == "" {
		return usagef("jirahere: --description was given but is empty; an empty description is not allowed.")
	}

	s, err := settings.Load(prof)
	if err != nil {
		return failf("jirahere: could not load settings: %s.", err)
	}
	projectKey, err := resolveProject(*project, s, prof)
	if err != nil {
		return err
	}
	issueTypeName, err := resolveIssueType(*issueType, s, prof)
	if err != nil {
		return err
	}

	quarterLabel := s.Defaults.CurrentQuarter
	if *suppressAutoQuarter {
		quarterLabel = ""
	}
	quarterAutoAdded := quarterLabel != "" && !containsString(labels, quarterLabel)
	labels = labelListFlag(createLabels(labels, quarterLabel))

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
	client := jira.NewClient(creds.BaseURL, creds.AuthHeader)

	res, err := command.Create(ctx, client, command.CreateInput{
		ProjectKey:     projectKey,
		IssueTypeName:  issueTypeName,
		Summary:        *summary,
		Labels:         []string(labels),
		HasParent:      *parent != "",
		ParentKey:      *parent,
		HasDescription: descriptionGiven,
		Description:    *description,
	})
	if err != nil {
		return mapCreateError(err)
	}

	printCreateResult(projectKey, issueTypeName, res, []string(labels), quarterLabel, quarterAutoAdded)

	if res.ParentErr != nil {
		return &errSilent{err: errors.New("create completed with errors")}
	}
	return nil
}

func mapCreateError(err error) error {
	var projectUnavailable *command.ErrCreateProjectUnavailable
	var issueTypeUnavailable *command.ErrCreateIssueTypeUnavailable
	var parentNotFound *command.ErrCreateParentNotFound
	var unreachable *command.PreflightUnreachableError
	var createFailed *command.ErrCreateCallFailed
	var subtaskUndetermined *command.ErrCreateSubtaskUndetermined

	switch {
	case errors.As(err, &unreachable):

		if unreachable.Operation == command.CreatePreflightResolveProject {
			return failf("jirahere: create could not resolve project %q: %s.", unreachable.Key, renderJiraError(unreachable.Err))
		}
		return failf("jirahere: create could not validate parent %q: %s.", unreachable.Key, renderJiraError(unreachable.Err))
	case errors.As(err, &projectUnavailable):
		return failf("jirahere: create project %q is not available for creation.", projectUnavailable.ProjectKey)
	case errors.As(err, &issueTypeUnavailable):
		return failf("jirahere: create issue type %q is not available for project %q; available types: %s.", issueTypeUnavailable.IssueTypeName, issueTypeUnavailable.ProjectKey, strings.Join(issueTypeUnavailable.Available, ", "))
	case errors.As(err, &parentNotFound):
		return failf("jirahere: create parent %q was not found (404).", parentNotFound.Key)
	case errors.As(err, &subtaskUndetermined):
		return failf("jirahere: create cannot tell whether issue type %q in project %q is a subtask type (Jira did not report it); pass --parent only for a type known to accept it, or omit --parent to create without one.", subtaskUndetermined.IssueTypeName, subtaskUndetermined.ProjectKey)
	case errors.As(err, &createFailed):
		return failf("jirahere: create failed: %s. Nothing was created.", renderJiraError(createFailed.Err))
	default:
		return failf("jirahere: create failed: %s. Nothing was created.", renderJiraError(err))
	}
}

func printCreateResult(projectKey, issueType string, res *command.CreateResult, labels []string, quarterLabel string, quarterAutoAdded bool) {
	fmt.Printf("Created a new %s in %s.\n", sanitizeSingleLine(issueType), sanitizeSingleLine(projectKey))
	fmt.Println(sanitizeSingleLine(res.Key))
	fmt.Println(createLabelsLine(labels, quarterLabel, quarterAutoAdded))

	if res.ParentAttempted {
		if res.ParentErr != nil {
			fmt.Printf(
				"Could not set %s as %s's parent: %s. Set one with the parent/child link primitive once you have a valid target.\n",
				sanitizeSingleLine(res.ParentKey), sanitizeSingleLine(res.Key), renderJiraError(res.ParentErr),
			)
		} else {
			fmt.Printf("Parent set: %s -> %s.\n", sanitizeSingleLine(res.Key), sanitizeSingleLine(res.ParentKey))
		}
	}

	if res.ParentErr == nil {
		fmt.Println("Done.")
	}
}

func createLabelsLine(labels []string, quarterLabel string, quarterAutoAdded bool) string {
	if len(labels) == 0 {
		return "Labels: none."
	}
	line := "Labels: " + strings.Join(sanitizeLabels(labels), ", ") + "."
	if quarterAutoAdded && quarterLabel != "" {
		line += fmt.Sprintf(" %s was auto-added for the current quarter.", sanitizeSingleLine(quarterLabel))
	}
	return line
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
