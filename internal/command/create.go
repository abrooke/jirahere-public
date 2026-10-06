package command

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

type CreateInput struct {
	ProjectKey    string
	IssueTypeName string
	Summary       string
	Labels        []string

	HasParent bool
	ParentKey string

	HasDescription bool
	Description    string
}

type CreateResult struct {
	Key string

	ParentKey       string
	ParentAttempted bool
	ParentErr       error
}

type ErrCreateResolveProject = PreflightUnreachableError

type ErrCreateProjectUnavailable struct{ ProjectKey string }

func (e *ErrCreateProjectUnavailable) Error() string {
	return fmt.Sprintf("project %q is not available for creation", e.ProjectKey)
}

type ErrCreateIssueTypeUnavailable struct {
	IssueTypeName string
	ProjectKey    string
	Available     []string
}

func (e *ErrCreateIssueTypeUnavailable) Error() string {
	return fmt.Sprintf("issue type %q is not available for project %q", e.IssueTypeName, e.ProjectKey)
}

type ErrCreateSubtaskUndetermined struct {
	IssueTypeName string
	ProjectKey    string
}

func (e *ErrCreateSubtaskUndetermined) Error() string {
	return fmt.Sprintf("issue type %q in project %q does not report whether it is a subtask type", e.IssueTypeName, e.ProjectKey)
}

type ErrCreateParentNotFound = IssueNotFoundError

type ErrCreateValidateParent = PreflightUnreachableError

type ErrCreateCallFailed = WriteCallFailedError

func Create(ctx context.Context, client jira.AccessStrategy, in CreateInput) (*CreateResult, error) {
	metadata, err := client.ResolveCreateMetadata(ctx, in.ProjectKey)
	if err != nil {
		return nil, &ErrCreateResolveProject{
			Subject:   fmt.Sprintf("project %q", in.ProjectKey),
			Operation: CreatePreflightResolveProject,
			Key:       in.ProjectKey,
			Err:       err,
		}
	}
	if metadata.ProjectID == "" {
		return nil, &ErrCreateProjectUnavailable{ProjectKey: in.ProjectKey}
	}
	issueType, ok := metadata.IssueTypes[in.IssueTypeName]
	if !ok {
		return nil, &ErrCreateIssueTypeUnavailable{
			IssueTypeName: in.IssueTypeName,
			ProjectKey:    in.ProjectKey,
			Available:     sortedIssueTypeNames(metadata.IssueTypes),
		}
	}

	if in.HasParent {
		if _, err := client.GetIssue(ctx, in.ParentKey); err != nil {
			if jira.IsNotFound(err) {
				return nil, &ErrCreateParentNotFound{Subject: "parent", Key: in.ParentKey}
			}
			return nil, &ErrCreateValidateParent{Subject: fmt.Sprintf("parent %q", in.ParentKey), Key: in.ParentKey, Err: err}
		}
		if issueType.Subtask == nil {
			return nil, &ErrCreateSubtaskUndetermined{IssueTypeName: in.IssueTypeName, ProjectKey: in.ProjectKey}
		}
	}

	var descriptionADF json.RawMessage
	if in.HasDescription {
		descriptionADF = adf.FromPlainText(in.Description)
	}

	atomicParent := in.HasParent && issueType.Subtask != nil && *issueType.Subtask
	createFields := jira.CreateIssueFields{
		ProjectID:   metadata.ProjectID,
		IssueTypeID: issueType.ID,
		Summary:     in.Summary,
		Description: descriptionADF,
		Labels:      in.Labels,
	}
	if atomicParent {
		createFields.ParentKey = in.ParentKey
	}
	created, err := client.CreateIssue(ctx, createFields)
	if err != nil {
		return nil, &ErrCreateCallFailed{Subject: "create failed", Err: err}
	}

	result := &CreateResult{Key: created.Key}

	switch {
	case atomicParent:

		result.ParentKey = in.ParentKey
		result.ParentAttempted = true
	case in.HasParent:

		result.ParentKey = in.ParentKey
		result.ParentAttempted = true
		if parentErr := client.SetParent(ctx, created.Key, in.ParentKey); parentErr != nil {
			result.ParentErr = parentErr
		}
	}

	return result, nil
}

func sortedIssueTypeNames(types map[string]jira.IssueType) []string {
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
