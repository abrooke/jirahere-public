package command

import "fmt"

type IssueNotFoundError struct {
	Subject string
	Key     string
}

func (e *IssueNotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found (404)", e.Subject, e.Key)
}

type PreflightOperation uint8

const (
	MovePreflightSource PreflightOperation = iota + 1
	MovePreflightParent

	RehomeChildrenPreflightOldParent
	RehomeChildrenPreflightNewParent

	CreatePreflightResolveProject
)

type PreflightUnreachableError struct {
	Subject   string
	Operation PreflightOperation
	Key       string
	Err       error
}

func (e *PreflightUnreachableError) Error() string {
	return fmt.Sprintf("could not reach Jira to validate %s: %s. Try again", e.Subject, e.Err)
}
func (e *PreflightUnreachableError) Unwrap() error { return e.Err }

type WriteCallFailedError struct {
	Subject   string
	SourceKey string
	Err       error
}

func (e *WriteCallFailedError) Error() string {
	return fmt.Sprintf("%s: %s. Nothing was created", e.Subject, e.Err)
}
func (e *WriteCallFailedError) Unwrap() error { return e.Err }
