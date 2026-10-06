package command

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"unicode"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

const clonersLinkType = "Cloners"

type CloneInput struct {
	SourceKey string

	HasSummary bool
	SummaryOld string
	SummaryNew string

	HasLabel bool
	LabelOld string
	LabelNew string

	HasParent bool
	ParentKey string

	HasPart bool
}

type CloneResult struct {
	SourceKey     string
	SourceType    string
	SourceSummary string

	NewKey     string
	NewSummary string

	Labels       []string
	LabelSwapped bool
	LabelOld     string
	LabelNew     string
	OtherLabels  []string

	LinkErr error

	ParentKey       string
	ParentAttempted bool
	ParentType      string
	ParentErr       error

	PartRequested    bool
	PartFound        bool
	PartFoundMarker  string
	PartCloneMarker  string
	PartSourceMarker string
	PartSourceErr    error
}

var partMarkerPattern = regexp.MustCompile(`\[Part ([0-9]+)\]`)

type partNumbering struct {
	found        bool
	foundMarker  string
	cloneMarker  string
	cloneSummary string

	sourceMarker  string
	sourceSummary string
}

func derivePartNumbering(sourceSummary string) partNumbering {
	if loc := partMarkerPattern.FindStringSubmatchIndex(sourceSummary); loc != nil {
		foundMarker := sourceSummary[loc[0]:loc[1]]
		n := new(big.Int)
		if _, ok := n.SetString(sourceSummary[loc[2]:loc[3]], 10); ok {
			n.Add(n, big.NewInt(1))
			cloneMarker := "[Part " + n.String() + "]"
			return partNumbering{
				found:        true,
				foundMarker:  foundMarker,
				cloneMarker:  cloneMarker,
				cloneSummary: sourceSummary[:loc[0]] + cloneMarker + sourceSummary[loc[1]:],
			}
		}

	}
	const sourceMarker = "[Part 1]"
	const cloneMarker = "[Part 2]"
	return partNumbering{
		cloneMarker:   cloneMarker,
		cloneSummary:  sourceSummary + " " + cloneMarker,
		sourceMarker:  sourceMarker,
		sourceSummary: sourceSummary + " " + sourceMarker,
	}
}

type ErrSourceNotFound = IssueNotFoundError

type ErrSummaryNotFound struct {
	SourceKey     string
	SourceSummary string
	Substr        string
}

func (e *ErrSummaryNotFound) Error() string {
	return fmt.Sprintf("%s's summary (%q) does not contain %q — nothing was created", e.SourceKey, e.SourceSummary, e.Substr)
}

type ErrLabelNotFound struct {
	SourceKey string
	Label     string
	Labels    []string
}

func (e *ErrLabelNotFound) Error() string {
	return fmt.Sprintf("%s does not have the label %q (its labels: %s)", e.SourceKey, e.Label, strings.Join(e.Labels, ", "))
}

var ErrLabelNewEmpty = errors.New("--label-new must not be empty")

type ErrInvalidLabel struct{ Label string }

func (e *ErrInvalidLabel) Error() string {
	return fmt.Sprintf("%q is not a valid Jira label (labels can't contain whitespace)", e.Label)
}

type ErrParentNotFound = IssueNotFoundError

var ErrClonersLinkTypeMissing = errors.New(`this site has no "Cloners" issue link type (it may have been renamed or removed) — clone requires it to record the original-to-clone link. Ask a Jira admin to restore it`)

type ErrPreflightUnreachable = PreflightUnreachableError

type ErrCreateFailed = WriteCallFailedError

func hasWhitespace(label string) bool {
	return strings.ContainsFunc(label, unicode.IsSpace)
}

func Clone(ctx context.Context, client jira.AccessStrategy, in CloneInput) (*CloneResult, error) {
	src, err := client.GetIssue(ctx, in.SourceKey)
	if err != nil {
		if jira.IsNotFound(err) {
			return nil, &ErrSourceNotFound{Subject: "clone source", Key: in.SourceKey}
		}
		return nil, &ErrPreflightUnreachable{Subject: "the clone", Err: err}
	}

	if in.HasSummary && !strings.Contains(src.Fields.Summary, in.SummaryOld) {
		return nil, &ErrSummaryNotFound{SourceKey: in.SourceKey, SourceSummary: src.Fields.Summary, Substr: in.SummaryOld}
	}

	if in.HasLabel {
		if !containsLabel(src.Fields.Labels, in.LabelOld) {
			return nil, &ErrLabelNotFound{SourceKey: in.SourceKey, Label: in.LabelOld, Labels: src.Fields.Labels}
		}
		if in.LabelNew == "" {
			return nil, ErrLabelNewEmpty
		}
		if hasWhitespace(in.LabelNew) {
			return nil, &ErrInvalidLabel{Label: in.LabelNew}
		}
	}

	var parentType string
	if in.HasParent {
		parent, err := client.GetIssue(ctx, in.ParentKey)
		if err != nil {
			if jira.IsNotFound(err) {
				return nil, &ErrParentNotFound{Subject: "parent", Key: in.ParentKey}
			}
			return nil, &ErrPreflightUnreachable{Subject: "the clone", Err: err}
		}
		parentType = parent.Fields.IssueType.Name
	}

	linkTypes, err := client.IssueLinkTypes(ctx)
	if err != nil {
		return nil, &ErrPreflightUnreachable{Subject: "the clone", Err: err}
	}
	if !hasClonersLinkType(linkTypes) {
		return nil, ErrClonersLinkTypeMissing
	}

	newSummary := src.Fields.Summary
	if in.HasSummary {
		newSummary = strings.ReplaceAll(newSummary, in.SummaryOld, in.SummaryNew)
	}
	var pn partNumbering
	if in.HasPart {
		pn = derivePartNumbering(src.Fields.Summary)
		newSummary = pn.cloneSummary
	}
	newLabels, otherLabels := swapLabel(src.Fields.Labels, in.HasLabel, in.LabelOld, in.LabelNew)

	created, err := client.CreateIssue(ctx, jira.CreateIssueFields{
		ProjectID:   src.Fields.Project.ID,
		IssueTypeID: src.Fields.IssueType.ID,
		Summary:     newSummary,
		Description: src.Fields.Description,
		Labels:      newLabels,
	})
	if err != nil {
		return nil, &ErrCreateFailed{Subject: fmt.Sprintf("could not create clone of %q", in.SourceKey), SourceKey: in.SourceKey, Err: err}
	}

	result := &CloneResult{
		SourceKey:     src.Key,
		SourceType:    src.Fields.IssueType.Name,
		SourceSummary: src.Fields.Summary,
		NewKey:        created.Key,
		NewSummary:    newSummary,
		Labels:        newLabels,
		LabelSwapped:  in.HasLabel,
		LabelOld:      in.LabelOld,
		LabelNew:      in.LabelNew,
		OtherLabels:   otherLabels,
		ParentKey:     in.ParentKey,

		PartRequested:    in.HasPart,
		PartFound:        pn.found,
		PartFoundMarker:  pn.foundMarker,
		PartCloneMarker:  pn.cloneMarker,
		PartSourceMarker: pn.sourceMarker,
	}

	if linkErr := client.CreateIssueLink(ctx, clonersLinkType, created.Key, src.Key); linkErr != nil {
		result.LinkErr = linkErr
	}

	if in.HasParent {
		result.ParentAttempted = true
		result.ParentType = parentType
		if parentErr := client.SetParent(ctx, created.Key, in.ParentKey); parentErr != nil {
			result.ParentErr = parentErr
		}
	}

	if in.HasPart && !pn.found {
		if partErr := client.SetSummary(ctx, src.Key, pn.sourceSummary); partErr != nil {
			result.PartSourceErr = partErr
		}
	}

	return result, nil
}

func hasClonersLinkType(types []jira.IssueLinkType) bool {
	for _, t := range types {
		if t.Name == clonersLinkType {
			return true
		}
	}
	return false
}
