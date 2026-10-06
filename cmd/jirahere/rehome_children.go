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

var rehomeChildrenValueFlags = map[string]bool{
	"to":          true,
	"skip-status": true,
	"summary-old": true,
	"summary-new": true,
	"label-old":   true,
	"label-new":   true,
	"profile":     true,
}

func splitRehomeChildrenArgs(args []string) (oldParentKey string, flagArgs []string, err error) {
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
		if rehomeChildrenValueFlags[name] && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if len(positional) != 1 {
		return "", nil, errors.New("usage: jirahere rehome-children <old-parent-key> --to <new-parent-key> [--skip-status <s1,s2,...>] [--summary-old <s> --summary-new <s>] [--label-old <l> --label-new <l>] [--profile <name>]")
	}
	return positional[0], flagArgs, nil
}

func runRehomeChildren(args []string) error {
	var oldParentKey string
	var flagArgs []string
	if hasHelpToken(args) {
		flagArgs = []string{"--help"}
	} else {
		var err error
		oldParentKey, flagArgs, err = splitRehomeChildrenArgs(args)
		if err != nil {
			return usagef("%s", err)
		}
	}

	fs := flag.NewFlagSet("rehome-children", flag.ContinueOnError)
	var to, skipStatus, summaryOld, summaryNew, labelOld, labelNew string
	toC := &repeatCounter{target: &to}
	skipStatusC := &repeatCounter{target: &skipStatus}
	summaryOldC := &repeatCounter{target: &summaryOld}
	summaryNewC := &repeatCounter{target: &summaryNew}
	labelOldC := &repeatCounter{target: &labelOld}
	labelNewC := &repeatCounter{target: &labelNew}
	fs.Var(toC, "to", "key of the work item to set as the children's new parent (required)")
	fs.Var(skipStatusC, "skip-status", "comma-separated list of statuses to exclude from the rehome")
	fs.Var(summaryOldC, "summary-old", "literal substring to find in each rehomed child's summary")
	fs.Var(summaryNewC, "summary-new", "replacement for --summary-old (requires --summary-old)")
	fs.Var(labelOldC, "label-old", "label to remove from each matching rehomed child")
	fs.Var(labelNewC, "label-new", "label to add in place of --label-old (requires --label-old)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<old-parent-key>"}}); err != nil {
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
		{"to", toC},
		{"skip-status", skipStatusC},
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

	if !visited["to"] {
		return usagef("jirahere: rehome-children %s: --to <new-parent-key> is required.", moveIssueKey(oldParentKey))
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

	in := command.RehomeChildrenInput{
		OldParentKey:    oldParentKey,
		NewParentKey:    to,
		SummaryOldGiven: visited["summary-old"],
		SummaryOld:      summaryOld,
		SummaryNewGiven: visited["summary-new"],
		SummaryNew:      summaryNew,
		LabelOldGiven:   visited["label-old"],
		LabelOld:        labelOld,
		LabelNewGiven:   visited["label-new"],
		LabelNew:        labelNew,
		SkipStatusGiven: visited["skip-status"],
		SkipStatusRaw:   skipStatus,
	}

	pf, err := command.PreflightRehomeChildren(ctx, client, in)
	if err != nil {
		return mapRehomeChildrenPreflightError(err)
	}

	res, err := command.ApplyRehomeChildren(ctx, client, pf)
	if err != nil {
		return mapRehomeChildrenExecutionError(err)
	}
	printRehomeChildrenResult(res)

	if !rehomeChildrenSucceeded(res) {
		return &errSilent{err: errors.New("rehome-children completed with errors")}
	}
	return nil
}

func rehomeChildrenSucceeded(res *command.RehomeChildrenResult) bool {
	if len(res.Failed) > 0 {
		return false
	}
	for _, pass := range res.Rewrites {
		if pass.WalkErr != nil || len(pass.ChildrenOfFailures) > 0 {
			return false
		}
		for _, item := range pass.Items {
			if item.SummaryErr != nil || item.LabelsErr != nil {
				return false
			}
		}
	}
	return true
}

func reparentedOnly(res *command.RehomeChildrenResult) bool {
	return !res.HasSummary && !res.HasLabel
}

func printRehomeChildrenResult(res *command.RehomeChildrenResult) {
	fmt.Printf("Rehoming %s's children to %s...\n", sanitizeSingleLine(res.OldParentKey), sanitizeSingleLine(res.NewParentKey))
	if len(res.ChildKeys) == 0 && len(res.Skipped) == 0 && len(res.Failed) == 0 {
		fmt.Println("No direct children found; nothing to do.")
		fmt.Println("Done.")
		return
	}

	for _, s := range res.Skipped {
		fmt.Printf("Skipped %s (status: %s).\n", sanitizeSingleLine(s.Key), sanitizeSingleLine(s.StatusName))
	}

	for _, f := range res.Failed {
		fmt.Println(renderRehomeChildrenFailureLine(rehomeChildrenFailureContext{
			Operation: rehomeChildrenFailureReparent,
			IssueKey:  f.Key,
			ParentKey: res.NewParentKey,
			Err:       f.Err,
		}, "It was not rehomed; retry once resolved."))
	}

	rewriteByChild := make(map[string]command.ChildRewritePass, len(res.Rewrites))
	for _, pass := range res.Rewrites {
		rewriteByChild[pass.ChildKey] = pass
	}
	for _, key := range res.ChildKeys {
		fmt.Printf("Reparented %s -> %s.\n", sanitizeSingleLine(key), sanitizeSingleLine(res.NewParentKey))
		if reparentedOnly(res) {
			continue
		}
		if pass, ok := rewriteByChild[key]; ok {
			printChildRewritePass(pass)
		}
	}

	if rehomeChildrenSucceeded(res) {
		fmt.Println("Done.")
	}
}

func printChildRewritePass(pass command.ChildRewritePass) {
	for _, item := range pass.Items {
		for _, line := range rewriteOutcomeLines(item) {
			fmt.Println(line)
		}
	}

	for _, f := range pass.ChildrenOfFailures {
		fmt.Println(renderRehomeChildrenFailureLine(rehomeChildrenFailureContext{
			Operation: rehomeChildrenFailureChildrenOf,
			IssueKey:  f.Key,
			ParentKey: f.ParentKey,
			Depth:     f.Depth,
			Err:       f.Err,
		}, "That node's subtree may be incomplete; retry once fixed."))
	}

	if pass.WalkErr != nil {
		fmt.Println(renderRehomeChildrenFailureLine(rehomeChildrenFailureContext{
			Operation: rehomeChildrenFailureNodeCeiling,
			IssueKey:  pass.ChildKey,
			Err:       pass.WalkErr,
		}, "The descendant results above may be incomplete; rerun rehome-children once resolved."))
	}
}

func rewriteOutcomeLines(o command.RewriteOutcome) []string {
	label := fmt.Sprintf("descendant %s (depth %d, parent %s)", sanitizeSingleLine(o.Key), o.Depth, sanitizeSingleLine(o.ParentKey))
	if o.Depth == 0 {
		label = fmt.Sprintf("rehomed child %s (parent %s)", sanitizeSingleLine(o.Key), sanitizeSingleLine(o.ParentKey))
	}

	var lines []string
	if o.SummaryErr != nil {
		lines = append(lines, renderRehomeChildrenFailureLine(rehomeChildrenFailureContext{
			Operation: rehomeChildrenFailureSummary,
			IssueKey:  o.Key,
			ParentKey: o.ParentKey,
			Depth:     o.Depth,
			Err:       o.SummaryErr,
		}, "Rewrite it by hand, or retry rehome-children."))
	}
	if o.LabelsErr != nil {
		lines = append(lines, renderRehomeChildrenFailureLine(rehomeChildrenFailureContext{
			Operation: rehomeChildrenFailureLabels,
			IssueKey:  o.Key,
			ParentKey: o.ParentKey,
			Depth:     o.Depth,
			Err:       o.LabelsErr,
		}, "Swap it by hand, or retry rehome-children."))
	}
	if len(lines) > 0 {
		return lines
	}

	switch {
	case o.SummaryAttempted && o.LabelsAttempted:
		return []string{fmt.Sprintf("Rewrite: %s: summary and labels rewritten.", label)}
	case o.SummaryAttempted:
		return []string{fmt.Sprintf("Rewrite: %s: summary rewritten.", label)}
	case o.LabelsAttempted:
		return []string{fmt.Sprintf("Rewrite: %s: labels rewritten.", label)}
	default:
		return []string{fmt.Sprintf("Rewrite: %s: no match; left untouched.", label)}
	}
}

type rehomeChildrenFailureOperation uint8

const (
	rehomeChildrenFailureListChildren rehomeChildrenFailureOperation = iota + 1
	rehomeChildrenFailureReparent
	rehomeChildrenFailureSummary
	rehomeChildrenFailureLabels
	rehomeChildrenFailureChildrenOf
	rehomeChildrenFailureNodeCeiling
)

type rehomeChildrenFailureContext struct {
	Operation rehomeChildrenFailureOperation
	IssueKey  string
	ParentKey string
	Depth     int
	Err       error
}

func renderRehomeChildrenFailure(f rehomeChildrenFailureContext) string {
	operation := map[rehomeChildrenFailureOperation]string{
		rehomeChildrenFailureListChildren: "listing direct children",
		rehomeChildrenFailureReparent:     "reparenting a direct child",
		rehomeChildrenFailureSummary:      "rewriting a summary",
		rehomeChildrenFailureLabels:       "rewriting labels",
		rehomeChildrenFailureChildrenOf:   "listing a rehomed child's descendants",
		rehomeChildrenFailureNodeCeiling:  "walking a rehomed child's descendants",
	}[f.Operation]
	if operation == "" {
		operation = "processing rehome-children"
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
	return fmt.Sprintf("could not complete rehome-children while %s%s: %s", operation, where, renderJiraError(f.Err))
}

func renderRehomeChildrenFailureLine(f rehomeChildrenFailureContext, guidance string) string {
	msg := renderRehomeChildrenFailure(f)
	if msg != "" {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	return fmt.Sprintf("%s. %s", msg, guidance)
}

func mapRehomeChildrenExecutionError(err error) error {
	var oldParentDeleted *command.ErrRehomeOldParentDeleted
	var listFailed *command.ErrRehomeChildrenListFailed

	switch {
	case errors.As(err, &oldParentDeleted):
		return failf("jirahere: rehome-children old parent %s was deleted before execution (404, detected on re-check); any children it had were orphaned in Jira, not rehomed. Point --to at wherever they should live now, or restore the old parent first.", moveIssueKey(oldParentDeleted.Key))
	case errors.As(err, &listFailed):
		return failf("jirahere: %s. Try again.", renderRehomeChildrenFailure(rehomeChildrenFailureContext{
			Operation: rehomeChildrenFailureListChildren,
			IssueKey:  listFailed.OldParentKey,
			Err:       listFailed.Err,
		}))
	default:
		return failf("jirahere: rehome-children execution failed: %s. Try again.", renderJiraError(err))
	}
}

func mapRehomeChildrenPreflightError(err error) error {
	var parentNotFound *command.IssueNotFoundError
	var skipStatusInvalid *command.ErrRehomeSkipStatusInvalid
	var unreachable *command.ErrRehomeChildrenPreflightUnreachable

	switch {
	case errors.As(err, &parentNotFound):

		return failf("jirahere: %s %s was not found (404).", parentNotFound.Subject, moveIssueKey(parentNotFound.Key))
	case errors.Is(err, command.ErrRehomeSummaryPairIncomplete):
		return usagef("jirahere: --summary-old and --summary-new must be given together.")
	case errors.Is(err, command.ErrRehomeLabelPairIncomplete):
		return usagef("jirahere: --label-old and --label-new must be given together.")
	case errors.As(err, &skipStatusInvalid):
		return usagef("jirahere: --skip-status %q contains an empty or whitespace-only entry.", skipStatusInvalid.Raw)
	case errors.As(err, &unreachable):
		label := "old parent"
		if unreachable.Operation == command.RehomeChildrenPreflightNewParent {
			label = "new parent"
		}
		return failf("jirahere: could not reach Jira to validate rehome-children's %s %s: %s. Try again.", label, moveIssueKey(unreachable.Key), renderJiraError(unreachable.Err))
	default:
		return failf("jirahere: rehome-children preflight failed: %s. Try again.", renderJiraError(err))
	}
}
