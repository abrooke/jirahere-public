package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

func statusRecursiveCountingLogin(t *testing.T) *atomic.Int32 {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var n atomic.Int32
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	return &n
}

func TestRunStatusRecursive_PreflightErrors(t *testing.T) {
	const badKey = "jirahere: status recursive: <id> must be a well-formed Jira issue key"
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing id", nil, statusRecursiveUsage},
		{"missing id with flag", []string{"--dry-run"}, statusRecursiveUsage},
		{"empty id", []string{""}, badKey},
		{"malformed id", []string{"proj-1"}, badKey},
		{"id without number", []string{"PROJ-"}, badKey},
		{"extra id", []string{"PROJ-1", "PROJ-2"}, statusRecursiveUsage},
		{"unknown flag", []string{"PROJ-1", "--bad"}, "jirahere: flag provided but not defined: --bad"},
		{"malformed dry-run value", []string{"PROJ-1", "--dry-run=maybe"}, ""},
		{"profile missing value", []string{"PROJ-1", "--profile"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := statusRecursiveCountingLogin(t)
			var err error
			stderr := captureStderr(t, func() { err = runStatusRecursive(tt.args) })
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %T %v, want *errSilent", err, err)
			}
			if tt.want != "" {
				if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
					t.Errorf("stderr = %q, want %q", got, tt.want)
				}
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want exactly one line", stderr)
			}
			if n := requests.Load(); n != 0 {
				t.Errorf("%d request(s) reached Jira", n)
			}
		})
	}
}

func TestRunStatusRecursive_InvalidProfile(t *testing.T) {
	requests := statusRecursiveCountingLogin(t)
	var err error
	stderr := captureStderr(t, func() { err = runStatusRecursive([]string{"PROJ-1", "--profile", "bad/name"}) })
	if err == nil || strings.Count(stderr, "\n") != 1 {
		t.Errorf("err = %v, stderr = %q, want one-line failure", err, stderr)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) reached Jira", n)
	}
}

func TestRunStatusRecursive_NotLoggedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var err error
	stderr := captureStderr(t, func() { err = runStatusRecursive([]string{"PROJ-1"}) })
	if err == nil {
		t.Fatal("err = nil, want failure")
	}
	if want := notLoggedInMessage("") + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestRunStatusRecursive_UnreadableCredentials(t *testing.T) {
	requests := statusRecursiveCountingLogin(t)
	path, err := auth.ConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr := captureStderr(t, func() { err = runStatusRecursive([]string{"PROJ-1"}) })
	if err == nil {
		t.Fatal("err = nil, want failure")
	}
	if want := "jirahere: could not load Jira credentials. Run \"jirahere login\" again.\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) reached Jira", n)
	}
}

type fakeRecursiveClient struct {
	statuses    map[string]string
	nodes       []jira.DescendantNode
	transitions map[string][]jira.Transition
	done        []string
}

func (f *fakeRecursiveClient) issue(key string) jira.Issue {
	var i jira.Issue
	i.Key = key
	i.Fields.Status.Name = f.statuses[key]
	return i
}

func (f *fakeRecursiveClient) GetIssue(_ context.Context, key string) (*jira.Issue, error) {
	i := f.issue(key)
	return &i, nil
}

func (f *fakeRecursiveClient) GetIssueRelation(ctx context.Context, key string) (*jira.Issue, error) {
	return f.GetIssue(ctx, key)
}

func (f *fakeRecursiveClient) WalkDescendants(context.Context, string) (*jira.DescendantWalkResult, error) {
	return &jira.DescendantWalkResult{Nodes: f.nodes, Failures: []jira.DescendantFailure{}}, nil
}

func (f *fakeRecursiveClient) Transitions(_ context.Context, key string) ([]jira.Transition, error) {
	return f.transitions[key], nil
}

func (f *fakeRecursiveClient) DoTransition(_ context.Context, key, id string) error {
	f.done = append(f.done, key+":"+id)
	return nil
}

func (f *fakeRecursiveClient) node(key, parent string, depth int) jira.DescendantNode {
	return jira.DescendantNode{Issue: f.issue(key), Depth: depth, ParentKey: parent}
}

func TestStatusRecursive_MultiLevelDeepestFirst(t *testing.T) {
	f := &fakeRecursiveClient{statuses: map[string]string{
		"P-1": "To Do", "P-2": "To Do", "P-3": "Done", "P-4": "Done",
		"P-5": "To Do", "P-6": "Weird", "P-7": "Done", "P-8": "Done",
	}}

	f.nodes = []jira.DescendantNode{
		f.node("P-2", "P-1", 1), f.node("P-5", "P-1", 1), f.node("P-7", "P-1", 1),
		f.node("P-3", "P-2", 2), f.node("P-4", "P-2", 2), f.node("P-6", "P-5", 2), f.node("P-8", "P-7", 2),
	}
	f.statuses["P-7"] = "Done"
	f.transitions = map[string][]jira.Transition{
		"P-1": {{ID: "11", ToStatusName: "In Progress"}},
		"P-2": {{ID: "22", ToStatusName: "Done"}},
	}
	var out bytes.Buffer
	if err := statusRecursive(context.Background(), f, &out, "P-1", false); err != nil {
		t.Fatal(err)
	}

	want := "Rolling up P-1 ...\n" +
		"Status set: P-2 -> Done\n" +
		"No rule matched P-5.\n" +
		"Unchanged P-7 (Done).\n" +
		"Status set: P-1 -> In Progress\n" +
		"Done.\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if got := strings.Join(f.done, ","); got != "P-2:22,P-1:11" {
		t.Errorf("transitions = %q, want deepest first P-2:22,P-1:11", got)
	}
}

type readOnlyRecursiveClient struct {
	fakeRecursiveClient
	t *testing.T
}

func (f *readOnlyRecursiveClient) Transitions(_ context.Context, key string) ([]jira.Transition, error) {
	f.t.Errorf("Transitions(%s) called in dry run", key)
	return nil, nil
}

func (f *readOnlyRecursiveClient) DoTransition(_ context.Context, key, id string) error {
	f.t.Errorf("DoTransition(%s, %s) called in dry run", key, id)
	return nil
}

func TestStatusRecursive_DryRunNoWrites(t *testing.T) {
	f := &readOnlyRecursiveClient{t: t}

	f.statuses = map[string]string{
		"P-1": "To Do", "P-2": "To Do", "P-3": "Done", "P-4": "Done",
		"P-5": "To Do", "P-6": "Weird", "P-7": "Done", "P-8": "Done",
	}
	f.nodes = []jira.DescendantNode{
		f.node("P-2", "P-1", 1), f.node("P-5", "P-1", 1), f.node("P-7", "P-1", 1),
		f.node("P-3", "P-2", 2), f.node("P-4", "P-2", 2), f.node("P-6", "P-5", 2), f.node("P-8", "P-7", 2),
	}
	var out bytes.Buffer
	if err := statusRecursive(context.Background(), f, &out, "P-1", true); err != nil {
		t.Fatal(err)
	}
	want := "Rolling up P-1 (dry run) ...\n" +
		"Would set: P-2 To Do -> Done\n" +
		"No rule matched P-5.\n" +
		"Unchanged P-7 (Done).\n" +
		"Would set: P-1 To Do -> In Progress\n" +
		"Done.\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if len(f.done) != 0 {
		t.Errorf("writes = %v, want none", f.done)
	}
}

func TestStatusRecursive_NoChildren(t *testing.T) {
	f := &fakeRecursiveClient{statuses: map[string]string{"P-1": "To Do"}}
	var out bytes.Buffer
	if err := statusRecursive(context.Background(), f, &out, "P-1", false); err != nil {
		t.Fatal(err)
	}
	if want := "Rolling up P-1 ...\nDone.\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if len(f.done) != 0 {
		t.Errorf("transitions = %v, want none", f.done)
	}
}

func TestRunStatusRecursive_ValidInvocationRuns(t *testing.T) {
	for _, args := range [][]string{
		{"PROJ-1"},
		{"PROJ-1", "--dry-run"},
		{"--dry-run", "PROJ-1"},
	} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/rest/api/3/search/jql") {
				_, _ = w.Write([]byte(`{"issues":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-1","fields":{"status":{"name":"To Do"}}}`))
		})
		var err error
		stdout := captureStdout(t, func() { err = runStatusRecursive(args) })
		if err != nil {
			t.Fatalf("args %v: err = %v", args, err)
		}
		want := "Rolling up PROJ-1 ...\nDone.\n"
		if len(args) == 2 {
			want = "Rolling up PROJ-1 (dry run) ...\nDone.\n"
		}
		if stdout != want {
			t.Errorf("args %v: stdout = %q, want %q", args, stdout, want)
		}
	}
}

func TestMainStatusRecursive_ExitBehavior(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
		wantErr  string
	}{
		{"help prints group synopsis", []string{"status", "recursive", "--help"}, 0, statusUsage + "\n" + statusRecursiveHelpNote + "\n", ""},
		{"missing id", []string{"status", "recursive"}, 2, "", statusRecursiveUsage + "\n"},
		{"extra id", []string{"status", "recursive", "PROJ-1", "PROJ-2"}, 2, "", statusRecursiveUsage + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != tt.wantExit || stdout != tt.wantOut || stderr != tt.wantErr {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}
