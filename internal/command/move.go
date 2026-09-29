package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

type MoveInput struct {
	SourceKey string

	HasSummary bool
	SummaryOld string
	SummaryNew string

	HasLabel bool
	LabelOld string
	LabelNew string

	HasParent bool
	ParentKey string
}

type MovePreflight struct {
	Source *jira.Issue
	Parent *jira.Issue
}

type ErrMoveSourceNotFound = IssueNotFoundError

type ErrMoveSummaryNotFound struct {
	SourceKey     string
	SourceSummary string
	Substr        string
}

func (e *ErrMoveSummaryNotFound) Error() string {
	return fmt.Sprintf("%s's summary (%q) does not contain %q", e.SourceKey, e.SourceSummary, e.Substr)
}

type ErrMoveLabelNotFound struct {
	SourceKey string
	Label     string
	Labels    []string
}

func (e *ErrMoveLabelNotFound) Error() string {
	return fmt.Sprintf("%s does not have the label %q (its labels: %s)", e.SourceKey, e.Label, strings.Join(e.Labels, ", "))
}

var ErrMoveLabelNewEmpty = errors.New("--label-new must not be empty")

type ErrMoveInvalidLabel struct{ Label string }

func (e *ErrMoveInvalidLabel) Error() string {
	return fmt.Sprintf("%q is not a valid Jira label (labels can't contain whitespace)", e.Label)
}

type ErrMoveParentNotFound = IssueNotFoundError

type MovePreflightOperation = PreflightOperation

type ErrMovePreflightUnreachable = PreflightUnreachableError

func PreflightMove(ctx context.Context, client jira.AccessStrategy, in MoveInput) (*MovePreflight, error) {
	source, err := client.GetIssue(ctx, in.SourceKey)
	if err != nil {
		if jira.IsNotFound(err) {
			return nil, &ErrMoveSourceNotFound{Subject: "move source", Key: in.SourceKey}
		}
		return nil, &ErrMovePreflightUnreachable{Subject: "the move", Operation: MovePreflightSource, Key: in.SourceKey, Err: err}
	}

	if in.HasSummary && !strings.Contains(source.Fields.Summary, in.SummaryOld) {
		return nil, &ErrMoveSummaryNotFound{SourceKey: in.SourceKey, SourceSummary: source.Fields.Summary, Substr: in.SummaryOld}
	}

	if in.HasLabel {
		if !containsLabel(source.Fields.Labels, in.LabelOld) {
			return nil, &ErrMoveLabelNotFound{SourceKey: in.SourceKey, Label: in.LabelOld, Labels: source.Fields.Labels}
		}
		if in.LabelNew == "" {
			return nil, ErrMoveLabelNewEmpty
		}
		if strings.ContainsFunc(in.LabelNew, unicode.IsSpace) {
			return nil, &ErrMoveInvalidLabel{Label: in.LabelNew}
		}
	}

	preflight := &MovePreflight{Source: source}
	if in.HasParent {
		parent, err := client.GetIssue(ctx, in.ParentKey)
		if err != nil {
			if jira.IsNotFound(err) {
				return nil, &ErrMoveParentNotFound{Subject: "parent", Key: in.ParentKey}
			}
			return nil, &ErrMovePreflightUnreachable{Subject: "the move", Operation: MovePreflightParent, Key: in.ParentKey, Err: err}
		}
		preflight.Parent = parent
	}
	return preflight, nil
}

type MoveResult struct {
	SourceKey string

	ParentAttempted bool
	ParentKey       string
	ParentErr       error

	SummaryAttempted bool
	SourceSummary    string
	NewSummary       string
	SummaryErr       error

	LabelsAttempted bool
	Labels          []string
	LabelOld        string
	LabelNew        string
	OtherLabels     []string
	LabelsErr       error
}

func ApplyMove(ctx context.Context, client jira.AccessStrategy, in MoveInput, pf *MovePreflight) *MoveResult {
	result := &MoveResult{SourceKey: in.SourceKey}

	if in.HasParent {
		result.ParentAttempted = true
		result.ParentKey = in.ParentKey
		if err := client.SetParent(ctx, in.SourceKey, in.ParentKey); err != nil {
			result.ParentErr = err
		}
	}

	if in.HasSummary {
		result.SummaryAttempted = true
		result.SourceSummary = pf.Source.Fields.Summary
		result.NewSummary = strings.ReplaceAll(pf.Source.Fields.Summary, in.SummaryOld, in.SummaryNew)
		if err := client.SetSummary(ctx, in.SourceKey, result.NewSummary); err != nil {
			result.SummaryErr = err
		}
	}

	if in.HasLabel {
		result.LabelsAttempted = true
		newLabels, otherLabels := swapLabel(pf.Source.Fields.Labels, true, in.LabelOld, in.LabelNew)
		result.Labels = newLabels
		result.LabelOld = in.LabelOld
		result.LabelNew = in.LabelNew
		result.OtherLabels = otherLabels
		if err := client.SetLabels(ctx, in.SourceKey, newLabels); err != nil {
			result.LabelsErr = err
		}
	}

	return result
}

type DescendantWriteResult struct {
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

type DescendantMoveResult struct {
	Attempted  bool
	HasSummary bool
	HasLabel   bool

	Descendants        []DescendantWriteResult
	ChildrenOfFailures []jira.DescendantFailure
	WalkErr            error
}

func ApplyDescendants(ctx context.Context, client jira.AccessStrategy, in MoveInput) *DescendantMoveResult {
	result := &DescendantMoveResult{HasSummary: in.HasSummary, HasLabel: in.HasLabel}
	if !in.HasSummary && !in.HasLabel {
		return result
	}
	result.Attempted = true

	walk, err := client.WalkDescendants(ctx, in.SourceKey)
	result.ChildrenOfFailures = walk.Failures
	result.WalkErr = err

	for _, node := range walk.Nodes {
		dr := DescendantWriteResult{Key: node.Issue.Key, ParentKey: node.ParentKey, Depth: node.Depth}

		if in.HasSummary && strings.Contains(node.Issue.Fields.Summary, in.SummaryOld) {
			dr.SummaryAttempted = true
			dr.OldSummary = node.Issue.Fields.Summary
			dr.NewSummary = strings.ReplaceAll(node.Issue.Fields.Summary, in.SummaryOld, in.SummaryNew)
			if err := client.SetSummary(ctx, node.Issue.Key, dr.NewSummary); err != nil {
				dr.SummaryErr = err
			}
		}

		if in.HasLabel && containsLabel(node.Issue.Fields.Labels, in.LabelOld) {
			dr.LabelsAttempted = true
			newLabels, otherLabels := swapLabel(node.Issue.Fields.Labels, true, in.LabelOld, in.LabelNew)
			dr.Labels = newLabels
			dr.LabelOld = in.LabelOld
			dr.LabelNew = in.LabelNew
			dr.OtherLabels = otherLabels
			if err := client.SetLabels(ctx, node.Issue.Key, newLabels); err != nil {
				dr.LabelsErr = err
			}
		}

		result.Descendants = append(result.Descendants, dr)
	}

	return result
}
