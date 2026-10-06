package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

var cloneValueFlags = map[string]bool{
	"summary-old": true,
	"summary-new": true,
	"label-old":   true,
	"label-new":   true,
	"parent":      true,
	"profile":     true,
}

func splitCloneArgs(args []string) (sourceKey string, flagArgs []string, err error) {
	var positional []string
	terminated := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if terminated {

			positional = append(positional, a)
			continue
		}
		if a == "--" {
			terminated = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if cloneValueFlags[name] && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if len(positional) != 1 {
		return "", nil, errors.New("usage: jirahere clone <source-key> [--summary-old <s> --summary-new <s>] [--label-old <l> --label-new <l>] [--parent <key>] [--part] [--profile <name>]")
	}
	return positional[0], flagArgs, nil
}

type repeatCounter struct {
	target *string
	count  int
}

func (c *repeatCounter) String() string { return "" }
func (c *repeatCounter) Set(v string) error {
	c.count++
	*c.target = v
	return nil
}

type boolRepeatCounter struct {
	target *bool
	count  int
}

func (c *boolRepeatCounter) String() string   { return "" }
func (c *boolRepeatCounter) IsBoolFlag() bool { return true }
func (c *boolRepeatCounter) Set(v string) error {
	b, err := strconv.ParseBool(v)
	if err != nil {
		return errors.New("parse error")
	}
	c.count++
	*c.target = b
	return nil
}

func runClone(args []string) error {
	var sourceKey string
	var flagArgs []string
	if hasHelpToken(args) {
		flagArgs = []string{"--help"}
	} else {
		var err error
		sourceKey, flagArgs, err = splitCloneArgs(args)
		if err != nil {
			return usagef("%s", err)
		}
	}

	fs := flag.NewFlagSet("clone", flag.ContinueOnError)
	var summaryOld, summaryNew, labelOld, labelNew, parent string
	summaryOldC := &repeatCounter{target: &summaryOld}
	summaryNewC := &repeatCounter{target: &summaryNew}
	labelOldC := &repeatCounter{target: &labelOld}
	labelNewC := &repeatCounter{target: &labelNew}
	parentC := &repeatCounter{target: &parent}
	var part bool
	partC := &boolRepeatCounter{target: &part}
	fs.Var(summaryOldC, "summary-old", "literal substring to find in the source's summary")
	fs.Var(summaryNewC, "summary-new", "replacement for --summary-old (requires --summary-old)")
	fs.Var(labelOldC, "label-old", "label to remove from the clone; must be one of the source's labels")
	fs.Var(labelNewC, "label-new", "label to add to the clone in place of --label-old (requires --label-old)")
	fs.Var(parentC, "parent", "key of the work item to set as the clone's parent")
	fs.Var(partC, "part", "auto-number a multi-part series: increment an existing [Part N] marker in the source's summary, or append [Part 1]/[Part 2] if none exists (mutually exclusive with --summary-old/--summary-new)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<source-key>"}}); err != nil {
		return err
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	for _, r := range []struct {
		name    string
		counter *repeatCounter
	}{
		{"summary-old", summaryOldC},
		{"summary-new", summaryNewC},
		{"label-old", labelOldC},
		{"label-new", labelNewC},
		{"parent", parentC},
	} {
		if r.counter.count > 1 {
			return usagef("jirahere: --%s may not be given more than once.", r.name)
		}
	}
	if partC.count > 1 {
		return usagef("jirahere: --part may not be given more than once.")
	}

	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

	if part && (visited["summary-old"] || visited["summary-new"]) {
		return usagef("jirahere: --part may not be combined with --summary-old or --summary-new.")
	}
	if visited["summary-old"] != visited["summary-new"] {
		return usagef("jirahere: --summary-old and --summary-new must be given together.")
	}
	if visited["label-old"] != visited["label-new"] {
		return usagef("jirahere: --label-old and --label-new must be given together.")
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
	client := jira.NewClient(creds.BaseURL, creds.AuthHeader)

	in := command.CloneInput{
		SourceKey:  sourceKey,
		HasSummary: visited["summary-old"],
		SummaryOld: summaryOld,
		SummaryNew: summaryNew,
		HasLabel:   visited["label-old"],
		LabelOld:   labelOld,
		LabelNew:   labelNew,
		HasParent:  visited["parent"],
		ParentKey:  parent,
		HasPart:    part,
	}

	res, err := command.Clone(ctx, client, in)
	if err != nil {
		return mapCloneError(err)
	}

	printCloneResult(res)

	if res.LinkErr != nil || res.ParentErr != nil || res.PartSourceErr != nil {
		return &errSilent{err: errors.New("clone completed with errors")}
	}
	return nil
}

func refreshCredentialsFailure(err error) error {
	return refreshCredentialsFailureFor(err, "")
}

func refreshCredentialsFailureFor(err error, prof string) error {
	var refreshStatus *auth.RefreshStatusError
	if errors.As(err, &refreshStatus) && isHTTPStatusCode(refreshStatus.StatusCode) {
		return failf("jirahere: could not refresh Jira credentials (%d). Run \"%s\" again.", refreshStatus.StatusCode, loginRemediation(prof))
	}
	return failf("jirahere: could not refresh Jira credentials. Run \"%s\" again.", loginRemediation(prof))
}

func mapCloneError(err error) error {
	var issueNotFound *command.IssueNotFoundError
	var summaryNotFound *command.ErrSummaryNotFound
	var labelNotFound *command.ErrLabelNotFound
	var invalidLabel *command.ErrInvalidLabel
	var unreachable *command.ErrPreflightUnreachable
	var createFailed *command.ErrCreateFailed

	switch {
	case errors.As(err, &issueNotFound):

		if issueNotFound.Subject == "clone source" {
			return failf("jirahere: %s.", issueNotFound)
		}
		return failf("jirahere: parent %q not found (404).", issueNotFound.Key)
	case errors.As(err, &summaryNotFound):
		return failf("jirahere: %s.", summaryNotFound)
	case errors.As(err, &labelNotFound):
		return failf("jirahere: %s.", labelNotFound)
	case errors.Is(err, command.ErrLabelNewEmpty):
		return usagef("jirahere: %s.", err)
	case errors.As(err, &invalidLabel):
		return usagef("jirahere: %s.", invalidLabel)
	case errors.Is(err, command.ErrClonersLinkTypeMissing):
		return failf("jirahere: %s.", err)
	case errors.As(err, &unreachable):
		return failf("jirahere: could not reach Jira to validate the clone: %s. Try again.", renderJiraError(unreachable.Err))
	case errors.As(err, &createFailed):
		return failf("jirahere: could not create clone of %q: %s. Nothing was created.", createFailed.SourceKey, renderJiraError(createFailed.Err))
	default:
		return failf("jirahere: clone failed: %s. Try again.", renderJiraError(err))
	}
}

func printCloneResult(res *command.CloneResult) {
	fmt.Printf("Cloning %s (%s: %q)...\n", sanitizeSingleLine(res.SourceKey), sanitizeSingleLine(res.SourceType), sanitizeSingleLine(res.SourceSummary))
	fmt.Printf("Created %s (%s: %q).\n", sanitizeSingleLine(res.NewKey), sanitizeSingleLine(res.SourceType), sanitizeSingleLine(res.NewSummary))

	if res.LinkErr != nil {
		fmt.Printf(
			"Could not link %s to %s as a clone: %s. Add a \"Cloners\" link between %s and %s by hand, or via the Jira web UI, to fix it.\n",
			sanitizeSingleLine(res.NewKey), sanitizeSingleLine(res.SourceKey), renderJiraError(res.LinkErr), sanitizeSingleLine(res.NewKey), sanitizeSingleLine(res.SourceKey),
		)
	} else {
		fmt.Printf("Linked %s as a clone of %s (Cloners link).\n", sanitizeSingleLine(res.NewKey), sanitizeSingleLine(res.SourceKey))
	}

	fmt.Println(labelsLine(res))

	switch {
	case !res.ParentAttempted:
		fmt.Println("No parent set.")
	case res.ParentErr != nil:
		fmt.Printf(
			"Could not set %s as %s's parent: %s. Set one with the parent/child link primitive once you have a valid target.\n",
			sanitizeSingleLine(res.ParentKey), sanitizeSingleLine(res.NewKey), renderJiraError(res.ParentErr),
		)
	default:
		fmt.Printf("Parent set: %s -> %s (%s).\n", sanitizeSingleLine(res.NewKey), sanitizeSingleLine(res.ParentKey), sanitizeSingleLine(res.ParentType))
	}

	if res.PartRequested {
		fmt.Println(partNumberingLine(res))
	}

	if res.LinkErr == nil && res.ParentErr == nil && res.PartSourceErr == nil {
		fmt.Println("Done.")
	}
}

func partNumberingLine(res *command.CloneResult) string {
	switch {
	case res.PartFound:
		return fmt.Sprintf("Part numbering: %q -> %q.", sanitizeSingleLine(res.PartFoundMarker), sanitizeSingleLine(res.PartCloneMarker))
	case res.PartSourceErr != nil:
		return fmt.Sprintf(
			"Could not mark %s as %q: %s. Clone is %q; add %q to %s's summary by hand if you want it recorded.",
			sanitizeSingleLine(res.SourceKey), sanitizeSingleLine(res.PartSourceMarker), renderJiraError(res.PartSourceErr), sanitizeSingleLine(res.PartCloneMarker), sanitizeSingleLine(res.PartSourceMarker), sanitizeSingleLine(res.SourceKey),
		)
	default:
		return fmt.Sprintf("Part numbering: source unnumbered, source is now %q, clone is %q.", sanitizeSingleLine(res.PartSourceMarker), sanitizeSingleLine(res.PartCloneMarker))
	}
}

func sanitizeLabels(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = sanitizeSingleLine(l)
	}
	return out
}

func labelsLine(res *command.CloneResult) string {
	if !res.LabelSwapped {
		if len(res.Labels) == 0 {
			return "No labels."
		}
		return fmt.Sprintf("Labels carried over unchanged: %s.", strings.Join(sanitizeLabels(res.Labels), ", "))
	}
	switch len(res.OtherLabels) {
	case 0:
		return fmt.Sprintf("Labels: %s -> %s (no other labels).", sanitizeSingleLine(res.LabelOld), sanitizeSingleLine(res.LabelNew))
	case 1:
		return fmt.Sprintf("Labels: %s -> %s (1 other label carried over unchanged: %s).", sanitizeSingleLine(res.LabelOld), sanitizeSingleLine(res.LabelNew), sanitizeSingleLine(res.OtherLabels[0]))
	default:
		return fmt.Sprintf("Labels: %s -> %s (%d other labels carried over unchanged: %s).", sanitizeSingleLine(res.LabelOld), sanitizeSingleLine(res.LabelNew), len(res.OtherLabels), strings.Join(sanitizeLabels(res.OtherLabels), ", "))
	}
}
