package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

const failBody = "SECRET-BODY-TEXT"

type failingRecursiveClient struct {
	fakeRecursiveClient
	getErr         error
	walkErr        error
	failures       []jira.DescendantFailure
	transitionsErr map[string]error
	doErr          map[string]error
}

func (f *failingRecursiveClient) GetIssue(ctx context.Context, key string) (*jira.Issue, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.fakeRecursiveClient.GetIssue(ctx, key)
}

func (f *failingRecursiveClient) WalkDescendants(ctx context.Context, key string) (*jira.DescendantWalkResult, error) {
	if f.walkErr != nil {
		return nil, f.walkErr
	}
	res, _ := f.fakeRecursiveClient.WalkDescendants(ctx, key)
	res.Failures = f.failures
	return res, nil
}

func (f *failingRecursiveClient) Transitions(ctx context.Context, key string) ([]jira.Transition, error) {
	if err := f.transitionsErr[key]; err != nil {
		return nil, err
	}
	return f.fakeRecursiveClient.Transitions(ctx, key)
}

func (f *failingRecursiveClient) DoTransition(ctx context.Context, key, id string) error {
	if err := f.doErr[key]; err != nil {
		return err
	}
	return f.fakeRecursiveClient.DoTransition(ctx, key, id)
}

func twoLevelTree() *failingRecursiveClient {
	f := &failingRecursiveClient{}
	f.statuses = map[string]string{"P-1": "To Do", "P-2": "To Do", "P-3": "Done", "P-4": "To Do", "P-5": "Done"}
	f.nodes = []jira.DescendantNode{
		f.node("P-2", "P-1", 1), f.node("P-4", "P-1", 1),
		f.node("P-3", "P-2", 2), f.node("P-5", "P-4", 2),
	}
	f.transitions = map[string][]jira.Transition{
		"P-1": {{ID: "11", ToStatusName: "Done"}},
		"P-2": {{ID: "22", ToStatusName: "Done"}},
		"P-4": {{ID: "44", ToStatusName: "Done"}},
	}
	return f
}

func runFailing(t *testing.T, f *failingRecursiveClient) (stdout, stderr string, err error) {
	t.Helper()
	var out bytes.Buffer
	stderr = captureStderr(t, func() {
		err = statusRecursive(context.Background(), f, &out, "P-1", false)
	})
	return out.String(), stderr, err
}

func TestStatusRecursive_NoWriteFailures(t *testing.T) {
	status500 := &jira.StatusError{StatusCode: 500, Body: failBody}
	tests := []struct {
		name    string
		mutate  func(*failingRecursiveClient)
		wantMsg string
	}{
		{"target missing", func(f *failingRecursiveClient) { f.getErr = &jira.StatusError{StatusCode: 404, Body: failBody} },
			`jirahere: status recursive could not read "P-1": Jira returned an unexpected response (404).`},
		{"existence check network error", func(f *failingRecursiveClient) { f.getErr = netTestErr(failBody) },
			`jirahere: status recursive could not read "P-1": could not reach Jira.`},
		{"walk error", func(f *failingRecursiveClient) { f.walkErr = status500 },
			`jirahere: status recursive could not walk the descendants of "P-1": Jira returned an unexpected response (500).`},
		{"branch failure", func(f *failingRecursiveClient) {
			f.failures = []jira.DescendantFailure{{Key: "P-4", ParentKey: "P-1", Depth: 1, Err: status500}}
		}, `jirahere: status recursive could not list the children of "P-4": Jira returned an unexpected response (500).`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := twoLevelTree()
			tt.mutate(f)
			stdout, stderr, err := runFailing(t, f)
			if err == nil {
				t.Fatal("expected nonzero-exit error")
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if got := strings.TrimSpace(stderr); got != tt.wantMsg {
				t.Errorf("stderr = %q, want %q", got, tt.wantMsg)
			}
			if len(f.done) != 0 {
				t.Errorf("writes = %v, want none", f.done)
			}
			if strings.Contains(stderr, failBody) {
				t.Errorf("stderr leaks response body: %q", stderr)
			}
		})
	}
}

func TestStatusRecursive_DryRunNoWriteFailures(t *testing.T) {
	status500 := &jira.StatusError{StatusCode: 500, Body: failBody}
	tests := []struct {
		name   string
		mutate func(*failingRecursiveClient)
	}{
		{"target missing", func(f *failingRecursiveClient) { f.getErr = &jira.StatusError{StatusCode: 404, Body: failBody} }},
		{"existence check network error", func(f *failingRecursiveClient) { f.getErr = netTestErr(failBody) }},
		{"walk error", func(f *failingRecursiveClient) { f.walkErr = status500 }},
		{"branch failure", func(f *failingRecursiveClient) {
			f.failures = []jira.DescendantFailure{{Key: "P-4", ParentKey: "P-1", Depth: 1, Err: status500}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			real := twoLevelTree()
			tt.mutate(real)
			_, realStderr, realErr := runFailing(t, real)

			f := twoLevelTree()
			tt.mutate(f)
			guard := &dryRunGuardClient{failingRecursiveClient: f, t: t}
			var out bytes.Buffer
			var err error
			stderr := captureStderr(t, func() {
				err = statusRecursive(context.Background(), guard, &out, "P-1", true)
			})
			if err == nil {
				t.Fatal("expected nonzero-exit error")
			}
			if fmt.Sprintf("%T", err) != fmt.Sprintf("%T", realErr) {
				t.Errorf("error type %T, want %T (same exit code as non-dry run)", err, realErr)
			}
			if out.String() != "" {
				t.Errorf("stdout = %q, want empty (no header)", out.String())
			}
			if got := strings.TrimSpace(stderr); got != strings.TrimSpace(realStderr) || strings.Contains(got, "\n") {
				t.Errorf("stderr = %q, want single line %q", stderr, realStderr)
			}
			if strings.Contains(stderr, failBody) {
				t.Errorf("stderr leaks response body: %q", stderr)
			}
			if len(f.done) != 0 {
				t.Errorf("writes = %v, want none", f.done)
			}
		})
	}
}

type dryRunGuardClient struct {
	*failingRecursiveClient
	t *testing.T
}

func (g *dryRunGuardClient) Transitions(_ context.Context, key string) ([]jira.Transition, error) {
	g.t.Errorf("Transitions(%s) called in dry run", key)
	return nil, nil
}

func (g *dryRunGuardClient) DoTransition(_ context.Context, key, id string) error {
	g.t.Errorf("DoTransition(%s, %s) called in dry run", key, id)
	return nil
}

func TestStatusRecursive_MidRunTransitionFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*failingRecursiveClient)
		wantErr string
	}{
		{"zero match", func(f *failingRecursiveClient) {
			f.transitions["P-4"] = []jira.Transition{{ID: "1", ToStatusName: "Other"}}
		}, `jirahere: status recursive: "Done" is not an available status for "P-4"; available: Other.`},
		{"ambiguous", func(f *failingRecursiveClient) {
			f.transitions["P-4"] = []jira.Transition{{ID: "1", ToStatusName: "Done"}, {ID: "2", ToStatusName: "Done"}}
		}, `jirahere: status recursive: "Done" is ambiguous for "P-4" (2 matching transitions); no transition was made.`},
		{"transitions list error", func(f *failingRecursiveClient) {
			f.transitionsErr = map[string]error{"P-4": &jira.StatusError{StatusCode: 503, Body: failBody}}
		}, `jirahere: status recursive could not list available transitions for "P-4": Jira returned an unexpected response (503).`},
		{"transition error", func(f *failingRecursiveClient) {
			f.doErr = map[string]error{"P-4": &jira.StatusError{StatusCode: 400, Body: failBody}}
		}, `jirahere: status recursive could not transition "P-4" to "Done": Jira returned an unexpected response (400).`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := twoLevelTree()
			tt.mutate(f)
			stdout, stderr, err := runFailing(t, f)
			if err == nil {
				t.Fatal("expected nonzero-exit error")
			}
			if want := "Rolling up P-1 ...\nStatus set: P-2 -> Done\n"; stdout != want {
				t.Errorf("stdout = %q, want %q", stdout, want)
			}
			if got := strings.TrimSpace(stderr); got != tt.wantErr {
				t.Errorf("stderr = %q, want %q", got, tt.wantErr)
			}
			if strings.Contains(stderr, failBody) {
				t.Errorf("stderr leaks response body: %q", stderr)
			}
			if got := strings.Join(f.done, ","); got != "P-2:22" {
				t.Errorf("writes = %q, want only P-2:22 (stop at failed node; P-1 untouched)", got)
			}
		})
	}
}
