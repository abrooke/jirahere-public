package jira

import (
	"context"
	"encoding/json"
)

type IssueReader interface {
	GetIssue(ctx context.Context, key string) (*Issue, error)
	GetIssueRelation(ctx context.Context, key string) (*Issue, error)
}

type IssueWriter interface {
	SetParent(ctx context.Context, issueKey, parentKey string) error
	SetSummary(ctx context.Context, issueKey, summary string) error
	SetLabels(ctx context.Context, issueKey string, labels []string) error
}

type IssueCreator interface {
	ResolveCreateMetadata(ctx context.Context, projectKey string) (*CreateMetadata, error)
	CreateIssue(ctx context.Context, in CreateIssueFields) (*CreateIssueResult, error)
}

type IssueLinker interface {
	IssueLinkTypes(ctx context.Context) ([]IssueLinkType, error)
	CreateIssueLink(ctx context.Context, linkTypeName, inwardKey, outwardKey string) error
}

type DescendantWalker interface {
	ChildrenOf(ctx context.Context, key string) ([]Issue, error)
	WalkDescendants(ctx context.Context, rootKey string) (*DescendantWalkResult, error)
	WalkDescendantsFromChildren(ctx context.Context, rootKey string, children []Issue) (*DescendantWalkResult, error)
}

type CommentService interface {
	AddComment(ctx context.Context, key string, body json.RawMessage) (*CommentResult, error)
	ListComments(ctx context.Context, key string) ([]Comment, error)
}

type TransitionService interface {
	Transitions(ctx context.Context, issueKey string) ([]Transition, error)
	DoTransition(ctx context.Context, issueKey, transitionID string) error
}

type AccessStrategy interface {
	IssueReader
	IssueWriter
	IssueCreator
	IssueLinker
	DescendantWalker
	CommentService
	TransitionService
}

var _ AccessStrategy = (*Client)(nil)
