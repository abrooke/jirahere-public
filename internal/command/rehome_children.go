package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

type RehomeChildrenInput struct {
	OldParentKey string
	NewParentKey string

	SummaryOldGiven bool
	SummaryOld      string
	SummaryNewGiven bool
	SummaryNew      string

	LabelOldGiven bool
	LabelOld      string
	LabelNewGiven bool
	LabelNew      string

	SkipStatusGiven bool
	SkipStatusRaw   string
}

type RehomeChildrenPreflight struct {
	OldParent *jira.Issue
	NewParent *jira.Issue

	HasSummary bool
	SummaryOld string
	SummaryNew string

	HasLabel bool
	LabelOld string
	LabelNew string

	SkipStatus []string
}

type (
	ErrRehomeOldParentNotFound = IssueNotFoundError
	ErrRehomeNewParentNotFound = IssueNotFoundError
)

var ErrRehomeSummaryPairIncomplete = errors.New("--summary-old and --summary-new must be given together")

var ErrRehomeLabelPairIncomplete = errors.New("--label-old and --label-new must be given together")

type ErrRehomeSkipStatusInvalid struct{ Raw string }

func (e *ErrRehomeSkipStatusInvalid) Error() string {
	return fmt.Sprintf("--skip-status %q contains an empty or whitespace-only entry", e.Raw)
}

type RehomeChildrenPreflightOperation = PreflightOperation

type ErrRehomeChildrenPreflightUnreachable = PreflightUnreachableError

func PreflightRehomeChildren(ctx context.Context, client jira.AccessStrategy, in RehomeChildrenInput) (*RehomeChildrenPreflight, error) {
	oldParent, err := client.GetIssue(ctx, in.OldParentKey)
	if err != nil {
		if jira.IsNotFound(err) {
			return nil, &ErrRehomeOldParentNotFound{Subject: "rehome-children old parent", Key: in.OldParentKey}
		}
		return nil, &ErrRehomeChildrenPreflightUnreachable{Subject: "rehome-children", Operation: RehomeChildrenPreflightOldParent, Key: in.OldParentKey, Err: err}
	}

	newParent, err := client.GetIssue(ctx, in.NewParentKey)
	if err != nil {
		if jira.IsNotFound(err) {
			return nil, &ErrRehomeNewParentNotFound{Subject: "rehome-children new parent", Key: in.NewParentKey}
		}
		return nil, &ErrRehomeChildrenPreflightUnreachable{Subject: "rehome-children", Operation: RehomeChildrenPreflightNewParent, Key: in.NewParentKey, Err: err}
	}

	if in.SummaryOldGiven != in.SummaryNewGiven {
		return nil, ErrRehomeSummaryPairIncomplete
	}

	if in.LabelOldGiven != in.LabelNewGiven {
		return nil, ErrRehomeLabelPairIncomplete
	}

	var skipStatus []string
	if in.SkipStatusGiven {
		skipStatus, err = splitSkipStatus(in.SkipStatusRaw)
		if err != nil {
			return nil, err
		}
	}

	return &RehomeChildrenPreflight{
		OldParent:  oldParent,
		NewParent:  newParent,
		HasSummary: in.SummaryOldGiven,
		SummaryOld: in.SummaryOld,
		SummaryNew: in.SummaryNew,
		HasLabel:   in.LabelOldGiven,
		LabelOld:   in.LabelOld,
		LabelNew:   in.LabelNew,
		SkipStatus: skipStatus,
	}, nil
}

type RehomeChildrenResult struct {
	OldParentKey string
	NewParentKey string
	ChildKeys    []string
	Skipped      []SkippedChild
	Failed       []FailedChild

	HasSummary bool
	HasLabel   bool
	Rewrites   []ChildRewritePass
}

type SkippedChild struct {
	Key        string
	StatusName string
}

type RewriteOutcome struct {
	Key       string
	ParentKey string
	Depth     int

	SummaryAttempted bool
	OldSummary       string
	NewSummary       string
	SummaryErr       error

	LabelsAttempted bool
	Labels          []string
	LabelOld        string
	LabelNew        string
	OtherLabels     []string
	LabelsErr       error
}

type ChildRewritePass struct {
	ChildKey string

	Items              []RewriteOutcome
	ChildrenOfFailures []jira.DescendantFailure
	WalkErr            error
}

func rewriteItem(ctx context.Context, client jira.AccessStrategy, issue jira.Issue, parentKey string, depth int, pf *RehomeChildrenPreflight) RewriteOutcome {
	out := RewriteOutcome{Key: issue.Key, ParentKey: parentKey, Depth: depth}

	if pf.HasSummary && strings.Contains(issue.Fields.Summary, pf.SummaryOld) {
		out.SummaryAttempted = true
		out.OldSummary = issue.Fields.Summary
		out.NewSummary = strings.ReplaceAll(issue.Fields.Summary, pf.SummaryOld, pf.SummaryNew)
		if err := client.SetSummary(ctx, issue.Key, out.NewSummary); err != nil {
			out.SummaryErr = err
		}
	}

	if pf.HasLabel && containsLabel(issue.Fields.Labels, pf.LabelOld) {
		out.LabelsAttempted = true
		newLabels, otherLabels := swapLabel(issue.Fields.Labels, true, pf.LabelOld, pf.LabelNew)
		out.Labels = newLabels
		out.LabelOld = pf.LabelOld
		out.LabelNew = pf.LabelNew
		out.OtherLabels = otherLabels
		if err := client.SetLabels(ctx, issue.Key, newLabels); err != nil {
			out.LabelsErr = err
		}
	}

	return out
}

type ErrRehomeChildrenListFailed struct {
	OldParentKey string
	Err          error
}

func (e *ErrRehomeChildrenListFailed) Error() string {
	return fmt.Sprintf("could not list %s's direct children: %s", e.OldParentKey, e.Err)
}
func (e *ErrRehomeChildrenListFailed) Unwrap() error { return e.Err }

type ErrRehomeOldParentDeleted struct{ Key string }

func (e *ErrRehomeOldParentDeleted) Error() string {
	return fmt.Sprintf("rehome-children old parent %q was deleted before execution (404)", e.Key)
}

type FailedChild struct {
	Key string
	Err error
}

func matchesSkipStatus(statusName string, skipStatus []string) bool {
	name := strings.TrimSpace(statusName)
	if name == "" {
		return false
	}
	for _, s := range skipStatus {
		if strings.EqualFold(name, s) {
			return true
		}
	}
	return false
}

func ApplyRehomeChildren(ctx context.Context, client jira.AccessStrategy, pf *RehomeChildrenPreflight) (*RehomeChildrenResult, error) {
	children, err := client.ChildrenOf(ctx, pf.OldParent.Key)
	if err != nil {
		if rehomeOldParentDeleted(ctx, client, pf.OldParent.Key) {
			return nil, &ErrRehomeOldParentDeleted{Key: pf.OldParent.Key}
		}
		return nil, &ErrRehomeChildrenListFailed{OldParentKey: pf.OldParent.Key, Err: err}
	}

	if len(children) == 0 && rehomeOldParentDeleted(ctx, client, pf.OldParent.Key) {
		return nil, &ErrRehomeOldParentDeleted{Key: pf.OldParent.Key}
	}

	result := &RehomeChildrenResult{
		OldParentKey: pf.OldParent.Key,
		NewParentKey: pf.NewParent.Key,
		ChildKeys:    make([]string, 0, len(children)),
		Skipped:      make([]SkippedChild, 0),
		Failed:       make([]FailedChild, 0),
		HasSummary:   pf.HasSummary,
		HasLabel:     pf.HasLabel,
	}
	rewrite := pf.HasSummary || pf.HasLabel

	for _, child := range children {
		if matchesSkipStatus(child.Fields.Status.Name, pf.SkipStatus) {
			result.Skipped = append(result.Skipped, SkippedChild{
				Key:        child.Key,
				StatusName: strings.TrimSpace(child.Fields.Status.Name),
			})
			continue
		}

		if err := client.SetParent(ctx, child.Key, pf.NewParent.Key); err != nil {
			result.Failed = append(result.Failed, FailedChild{Key: child.Key, Err: err})
			continue
		}
		result.ChildKeys = append(result.ChildKeys, child.Key)

		if !rewrite {
			continue
		}

		pass := ChildRewritePass{ChildKey: child.Key}
		pass.Items = append(pass.Items, rewriteItem(ctx, client, child, pf.NewParent.Key, 0, pf))

		walk, walkErr := client.WalkDescendants(ctx, child.Key)
		pass.ChildrenOfFailures = walk.Failures
		pass.WalkErr = walkErr
		for _, node := range walk.Nodes {
			pass.Items = append(pass.Items, rewriteItem(ctx, client, node.Issue, node.ParentKey, node.Depth, pf))
		}

		result.Rewrites = append(result.Rewrites, pass)
	}
	return result, nil
}

func rehomeOldParentDeleted(ctx context.Context, client jira.AccessStrategy, oldParentKey string) bool {
	_, err := client.GetIssue(ctx, oldParentKey)
	return err != nil && jira.IsNotFound(err)
}

func splitSkipStatus(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	statuses := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return nil, &ErrRehomeSkipStatusInvalid{Raw: raw}
		}
		statuses = append(statuses, trimmed)
	}
	return statuses, nil
}
