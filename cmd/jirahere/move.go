package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

var moveValueFlags = map[string]bool{
	"parent":      true,
	"summary-old": true,
	"summary-new": true,
	"label-old":   true,
	"label-new":   true,
	"profile":     true,
}

func splitMoveArgs(args []string) (sourceKey string, flagArgs []string, err error) {
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
		if moveValueFlags[name] && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if len(positional) != 1 {
		return "", nil, errors.New("usage: jirahere move <source-key> [--parent <new-parent-key>] [--summary-old <s> --summary-new <s>] [--label-old <l> --label-new <l>] [--profile <name>]")
	}
	return positional[0], flagArgs, nil
}

func runMove(args []string) error {
	var sourceKey string
	var flagArgs []string
	if hasHelpToken(args) {
		flagArgs = []string{"--help"}
	} else {
		var err error
		sourceKey, flagArgs, err = splitMoveArgs(args)
		if err != nil {
			return usagef("%s", err)
		}
	}

	fs := flag.NewFlagSet("move", flag.ContinueOnError)
	var parent, summaryOld, summaryNew, labelOld, labelNew string
	parentC := &repeatCounter{target: &parent}
	summaryOldC := &repeatCounter{target: &summaryOld}
	summaryNewC := &repeatCounter{target: &summaryNew}
	labelOldC := &repeatCounter{target: &labelOld}
	labelNewC := &repeatCounter{target: &labelNew}
	fs.Var(parentC, "parent", "key of the work item to set as the target's new parent")
	fs.Var(summaryOldC, "summary-old", "literal substring to find in the target's (and each descendant's) summary")
	fs.Var(summaryNewC, "summary-new", "replacement for --summary-old (requires --summary-old)")
	fs.Var(labelOldC, "label-old", "label to remove from the target (and each descendant); must be one of the target's labels")
	fs.Var(labelNewC, "label-new", "label to add in place of --label-old (requires --label-old)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs); err != nil {
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
		{"parent", parentC},
		{"summary-old", summaryOldC},
		{"summary-new", summaryNewC},
		{"label-old", labelOldC},
		{"label-new", labelNewC},
	} {
		if r.counter.count > 1 {
			return usagef("jirahere: --%s may not be given more than once.", r.name)
		}
	}

	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

	if visited["summary-old"] != visited["summary-new"] {
		return usagef("jirahere: --summary-old and --summary-new must be given together.")
	}
	if visited["label-old"] != visited["label-new"] {
		return usagef("jirahere: --label-old and --label-new must be given together.")
	}
	if !visited["parent"] && !visited["summary-old"] && !visited["label-old"] {
		return usagef("jirahere: move %s: nothing to do — give at least one of --parent, --summary-old/--summary-new, or --label-old/--label-new.", moveIssueKey(sourceKey))
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

	in := command.MoveInput{
		SourceKey:  sourceKey,
		HasSummary: visited["summary-old"],
		SummaryOld: summaryOld,
		SummaryNew: summaryNew,
		HasLabel:   visited["label-old"],
		LabelOld:   labelOld,
		LabelNew:   labelNew,
		HasParent:  visited["parent"],
		ParentKey:  parent,
	}

	pf, err := command.PreflightMove(ctx, client, in)
	if err != nil {
		return mapMovePreflightError(err)
	}

	res := command.ApplyMove(ctx, client, in, pf)
	descRes := command.ApplyDescendants(ctx, client, in)
	printMoveResult(res, descRes)

	if !moveSucceeded(res, descRes) {
		return &errSilent{err: errors.New("move completed with errors")}
	}
	return nil
}

func moveSucceeded(res *command.MoveResult, descRes *command.DescendantMoveResult) bool {
	if res.ParentErr != nil || res.SummaryErr != nil || res.LabelsErr != nil {
		return false
	}
	if descRes.WalkErr != nil || len(descRes.ChildrenOfFailures) > 0 {
		return false
	}
	for _, d := range descRes.Descendants {
		if d.SummaryErr != nil || d.LabelsErr != nil {
			return false
		}
	}
	return true
}

func printMoveResult(res *command.MoveResult, descRes *command.DescendantMoveResult) {
	fmt.Printf("Moving %s...\n", sanitizeSingleLine(res.SourceKey))

	switch {
	case !res.ParentAttempted:
		fmt.Println("Parent unchanged (not requested).")
	case res.ParentErr != nil:
		fmt.Println(renderMoveFailureLine(moveFailureContext{
			Operation: moveFailureTargetReparent,
			IssueKey:  res.SourceKey,
			Err:       res.ParentErr,
		}, "Set it by hand once you have a valid target, or retry the move."))
	default:
		fmt.Printf("Parent set: %s -> %s.\n", sanitizeSingleLine(res.SourceKey), sanitizeSingleLine(res.ParentKey))
	}

	switch {
	case !res.SummaryAttempted:
		fmt.Println("Summary unchanged (not requested).")
	case res.SummaryErr != nil:
		fmt.Println(renderMoveFailureLine(moveFailureContext{
			Operation: moveFailureTargetSummary,
			IssueKey:  res.SourceKey,
			Err:       res.SummaryErr,
		}, "Rewrite it by hand, or retry the move."))
	default:
		fmt.Printf("Summary: %q -> %q.\n", sanitizeSingleLine(res.SourceSummary), sanitizeSingleLine(res.NewSummary))
	}

	fmt.Println(moveLabelsLine(res))

	printDescendantResult(descRes)

	if moveSucceeded(res, descRes) {
		fmt.Println("Done.")
	}
}

func printDescendantResult(descRes *command.DescendantMoveResult) {
	if !descRes.Attempted {
		return
	}
	if len(descRes.Descendants) == 0 && len(descRes.ChildrenOfFailures) == 0 && descRes.WalkErr == nil {
		fmt.Println("Descendants: none found.")
		return
	}

	for _, d := range descRes.Descendants {
		for _, line := range descendantOutcomeLines(d) {
			fmt.Println(line)
		}
	}

	for _, f := range descRes.ChildrenOfFailures {
		fmt.Println(renderMoveFailureLine(moveFailureContext{
			Operation: moveFailureChildrenOf,
			IssueKey:  f.Key,
			ParentKey: f.ParentKey,
			Depth:     f.Depth,
			Err:       f.Err,
		}, "That node's subtree may be incomplete; retry once fixed."))
	}

	if descRes.WalkErr != nil {
		fmt.Println(renderMoveFailureLine(moveFailureContext{
			Operation: moveFailureNodeCeiling,
			Err:       descRes.WalkErr,
		}, "The descendant results above may be incomplete; rerun the move once resolved."))
	}
}

func descendantOutcomeLines(d command.DescendantWriteResult) []string {
	var lines []string
	if d.SummaryErr != nil {
		lines = append(lines, renderMoveFailureLine(moveFailureContext{
			Operation: moveFailureDescendantSummary,
			IssueKey:  d.Key,
			ParentKey: d.ParentKey,
			Depth:     d.Depth,
			Err:       d.SummaryErr,
		}, "Rewrite it by hand, or retry the move."))
	}
	if d.LabelsErr != nil {
		lines = append(lines, renderMoveFailureLine(moveFailureContext{
			Operation: moveFailureDescendantLabels,
			IssueKey:  d.Key,
			ParentKey: d.ParentKey,
			Depth:     d.Depth,
			Err:       d.LabelsErr,
		}, "Swap it by hand, or retry the move."))
	}
	if len(lines) > 0 {
		return lines
	}

	key := sanitizeSingleLine(d.Key)
	parent := sanitizeSingleLine(d.ParentKey)
	switch {
	case d.SummaryAttempted && d.LabelsAttempted:
		return []string{fmt.Sprintf("Descendant %s (depth %d, parent %s): summary and labels rewritten.", key, d.Depth, parent)}
	case d.SummaryAttempted:
		return []string{fmt.Sprintf("Descendant %s (depth %d, parent %s): summary rewritten.", key, d.Depth, parent)}
	case d.LabelsAttempted:
		return []string{fmt.Sprintf("Descendant %s (depth %d, parent %s): labels rewritten.", key, d.Depth, parent)}
	default:
		return []string{fmt.Sprintf("Descendant %s (depth %d, parent %s): no match; left untouched.", key, d.Depth, parent)}
	}
}

func renderMoveFailureLine(f moveFailureContext, guidance string) string {
	msg := renderMoveFailure(f)
	if msg != "" {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	return fmt.Sprintf("%s. %s", msg, guidance)
}

func moveLabelsLine(res *command.MoveResult) string {
	switch {
	case !res.LabelsAttempted:
		return "Labels unchanged (not requested)."
	case res.LabelsErr != nil:
		return renderMoveFailureLine(moveFailureContext{
			Operation: moveFailureTargetLabels,
			IssueKey:  res.SourceKey,
			Err:       res.LabelsErr,
		}, "Swap it by hand, or retry the move.")
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

type moveFailureOperation uint8

const (
	moveFailureTargetPreflight moveFailureOperation = iota + 1
	moveFailureTargetReparent
	moveFailureTargetSummary
	moveFailureTargetLabels
	moveFailureDescendantSummary
	moveFailureDescendantLabels
	moveFailureChildrenOf
	moveFailureNodeCeiling
)

type moveFailureContext struct {
	Operation moveFailureOperation
	IssueKey  string
	ParentKey string
	Depth     int
	Err       error
}

func renderMoveFailure(f moveFailureContext) string {
	operation := map[moveFailureOperation]string{
		moveFailureTargetPreflight:   "validating the target",
		moveFailureTargetReparent:    "setting the target parent",
		moveFailureTargetSummary:     "rewriting the target summary",
		moveFailureTargetLabels:      "rewriting the target labels",
		moveFailureDescendantSummary: "rewriting a descendant summary",
		moveFailureDescendantLabels:  "rewriting descendant labels",
		moveFailureChildrenOf:        "listing descendant children",
		moveFailureNodeCeiling:       "walking descendants",
	}[f.Operation]
	if operation == "" {
		operation = "processing the move"
	}

	where := ""
	if key := moveIssueKey(f.IssueKey); key != "" {
		where = " for " + key
	}
	if f.Depth > 0 {
		where += fmt.Sprintf(" at descendant depth %d", f.Depth)
		if parent := moveIssueKey(f.ParentKey); parent != "" {
			where += " below " + parent
		}
	}
	return fmt.Sprintf("could not complete move while %s%s: %s", operation, where, renderJiraError(f.Err))
}

func moveIssueKey(key string) string {
	hyphen := strings.LastIndexByte(key, '-')
	if hyphen <= 0 || hyphen == len(key)-1 {
		return ""
	}
	for i, r := range key[:hyphen] {
		if (i == 0 && (r < 'A' || r > 'Z')) || (i > 0 && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_') {
			return ""
		}
	}
	for _, r := range key[hyphen+1:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return key
}

func mapMovePreflightError(err error) error {
	var issueNotFound *command.IssueNotFoundError
	var summaryNotFound *command.ErrMoveSummaryNotFound
	var labelNotFound *command.ErrMoveLabelNotFound
	var invalidLabel *command.ErrMoveInvalidLabel
	var unreachable *command.ErrMovePreflightUnreachable

	switch {
	case errors.As(err, &issueNotFound):

		if issueNotFound.Subject == "move source" {
			return failf("jirahere: move source %s was not found (404).", moveIssueKey(issueNotFound.Key))
		}
		return failf("jirahere: move parent %s was not found (404).", moveIssueKey(issueNotFound.Key))
	case errors.As(err, &summaryNotFound):
		return failf("jirahere: move preflight could not apply the requested summary rewrite to source %s.", moveIssueKey(summaryNotFound.SourceKey))
	case errors.As(err, &labelNotFound):
		return failf("jirahere: move preflight could not apply the requested label rewrite to source %s.", moveIssueKey(labelNotFound.SourceKey))
	case errors.Is(err, command.ErrMoveLabelNewEmpty):
		return usagef("jirahere: move preflight rejected an empty replacement label.")
	case errors.As(err, &invalidLabel):
		return usagef("jirahere: move preflight rejected a replacement label containing whitespace.")
	case errors.As(err, &unreachable):
		return failf("jirahere: %s. Try again.", renderMoveFailure(moveFailureContext{
			Operation: movePreflightFailureOperation(unreachable.Operation),
			IssueKey:  unreachable.Key,
			Err:       unreachable.Err,
		}))
	default:
		return failf("jirahere: %s. Try again.", renderMoveFailure(moveFailureContext{Operation: moveFailureTargetPreflight, Err: err}))
	}
}

func movePreflightFailureOperation(operation command.MovePreflightOperation) moveFailureOperation {

	return moveFailureTargetPreflight
}
