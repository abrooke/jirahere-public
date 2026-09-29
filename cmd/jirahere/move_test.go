package main

import (
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

func TestRunMove_NoSourceKey(t *testing.T) {
	err := runMove(nil)
	if err == nil {
		t.Fatal("runMove: expected error for missing source key, got nil")
	}
	if !strings.HasPrefix(err.Error(), "usage: jirahere move ") {
		t.Errorf("err = %q, want it to start with the move usage string", err.Error())
	}
}

func TestMapMovePreflightError_SourceVsParentNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "source",
			err:  &command.ErrMoveSourceNotFound{Subject: "move source", Key: "PROJ-123"},
			want: "jirahere: move source PROJ-123 was not found (404).",
		},
		{
			name: "parent",
			err:  &command.ErrMoveParentNotFound{Subject: "parent", Key: "PROJ-456"},
			want: "jirahere: move parent PROJ-456 was not found (404).",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mapMovePreflightError(tc.err)
			if err == nil || err.Error() != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMapMovePreflightError_SanitizesJiraControlledText(t *testing.T) {
	poison := "visible\x1b[2J\nnext\u202E"
	for _, err := range []error{
		&command.ErrMoveSummaryNotFound{SourceKey: poison, SourceSummary: poison, Substr: poison},
		&command.ErrMoveLabelNotFound{SourceKey: poison, Label: poison, Labels: []string{poison}},
		&command.ErrMovePreflightUnreachable{Err: &jira.StatusError{StatusCode: 502, Body: poison}},
	} {
		stderr := captureStderr(t, func() { _ = mapMovePreflightError(err) })
		if strings.ContainsAny(stderr, "\x1b\r\u202e") || strings.Count(stderr, "\n") != 1 {
			t.Errorf("unsafe Jira-controlled text reached stderr: %q", stderr)
		}
	}
}

func TestMoveFailureOutput_IsBodyFree(t *testing.T) {
	const secret = "Bearer reflected-authorization secret-payload token=abc123"
	statusErr := &jira.StatusError{StatusCode: 502, Body: secret}
	transportErr := errors.New(secret)

	preflight := []error{
		&command.ErrMoveSourceNotFound{Subject: "move source", Key: "PROJ-123"},
		&command.ErrMoveSummaryNotFound{SourceKey: "PROJ-123", SourceSummary: secret, Substr: secret},
		&command.ErrMoveLabelNotFound{SourceKey: "PROJ-123", Label: secret, Labels: []string{secret}},
		command.ErrMoveLabelNewEmpty,
		&command.ErrMoveInvalidLabel{Label: secret},
		&command.ErrMoveParentNotFound{Subject: "parent", Key: "PROJ-456"},
		&command.ErrMovePreflightUnreachable{Operation: command.MovePreflightSource, Key: "PROJ-123", Err: statusErr},
		&command.ErrMovePreflightUnreachable{Operation: command.MovePreflightParent, Key: "PROJ-456", Err: transportErr},
	}
	for _, err := range preflight {
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() { _ = mapMovePreflightError(err) })
		})
		if strings.Contains(stderr, secret) {
			t.Errorf("preflight output exposed secret for %T: %q", err, stderr)
		}
		if strings.Contains(stdout, secret) {
			t.Errorf("preflight stdout exposed secret for %T: %q", err, stdout)
		}
		if stdout != "" {
			t.Errorf("preflight wrote unexpected stdout for %T: %q", err, stdout)
		}
	}

	for _, operation := range []moveFailureOperation{
		moveFailureTargetPreflight,
		moveFailureTargetReparent,
		moveFailureTargetSummary,
		moveFailureTargetLabels,
		moveFailureDescendantSummary,
		moveFailureDescendantLabels,
		moveFailureChildrenOf,
		moveFailureNodeCeiling,
	} {
		for _, err := range []error{statusErr, transportErr} {
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() {
					_ = failf("jirahere: %s. Repair the affected item using the Jira web UI.", renderMoveFailure(moveFailureContext{
						Operation: operation,
						IssueKey:  "PROJ-123",
						ParentKey: "PROJ-100",
						Depth:     2,
						Err:       err,
					}))
				})
			})
			if strings.Contains(stderr, secret) {
				t.Errorf("move output exposed secret for operation %d: %q", operation, stderr)
			}
			if strings.Contains(stdout, secret) {
				t.Errorf("move stdout exposed secret for operation %d: %q", operation, stdout)
			}
			if stdout != "" {
				t.Errorf("move wrote unexpected stdout for operation %d: %q", operation, stdout)
			}
			if err == statusErr && !strings.Contains(stderr, "(502)") {
				t.Errorf("status failure for operation %d omitted HTTP status: %q", operation, stderr)
			}
			if strings.Contains(stderr, "PROJ-100") && !strings.Contains(stderr, "depth 2") {
				t.Errorf("lineage was not rendered consistently: %q", stderr)
			}
		}
	}
}

type moveTestIssue struct {
	summary string
	labels  []string
}

type moveWriteCall struct {
	method string
	key    string
	fields map[string]any
}

func newMoveFlagBindingServer(t *testing.T, issues map[string]moveTestIssue) (*httptest.Server, *[]moveWriteCall) {
	t.Helper()
	var calls []moveWriteCall
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	})
	mux.HandleFunc("/rest/api/3/issue/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		switch r.Method {
		case http.MethodGet:
			issue, ok := issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			body, err := json.Marshal(map[string]any{
				"id":  "1",
				"key": key,
				"fields": map[string]any{
					"summary":   issue.summary,
					"labels":    issue.labels,
					"issuetype": map[string]string{"id": "1", "name": "Task"},
					"project":   map[string]string{"id": "1", "key": "PROJ"},
				},
			})
			if err != nil {
				t.Fatalf("encode fixture issue: %v", err)
			}
			_, _ = w.Write(body)
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			calls = append(calls, moveWriteCall{method: r.Method, key: key, fields: decoded.Fields})
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method during ApplyMove: %s %s", r.Method, r.URL.Path)
		}
	})
	return httptest.NewServer(mux), &calls
}

type moveServerTransport struct {
	srv  *httptest.Server
	next http.RoundTripper
}

func (rt moveServerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(rt.srv.URL)
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = target.Scheme
	cloned.URL.Host = target.Host
	cloned.Host = target.Host
	return rt.next.RoundTrip(cloned)
}

func moveWriteCallsWant(hasParent, hasSummary, hasLabel bool) []moveWriteCall {
	var want []moveWriteCall
	if hasParent {
		want = append(want, moveWriteCall{
			method: http.MethodPut, key: "PROJ-123",
			fields: map[string]any{"parent": map[string]any{"key": "PROJ-001"}},
		})
	}
	if hasSummary {
		want = append(want, moveWriteCall{
			method: http.MethodPut, key: "PROJ-123",
			fields: map[string]any{"summary": "Widget Q4'26"},
		})
	}
	if hasLabel {
		want = append(want, moveWriteCall{
			method: http.MethodPut, key: "PROJ-123",
			fields: map[string]any{"labels": []any{"FY26-Q4", "team-widgets"}},
		})
	}
	return want
}

func TestRunMove_ValidCombinations(t *testing.T) {
	tests := []struct {
		name                            string
		args                            []string
		hasParent, hasSummary, hasLabel bool
	}{
		{
			name:      "parent alone",
			args:      []string{"PROJ-123", "--parent", "PROJ-001"},
			hasParent: true,
		},
		{
			name:       "summary pair alone",
			args:       []string{"PROJ-123", "--summary-old", "Q3'26", "--summary-new", "Q4'26"},
			hasSummary: true,
		},
		{
			name:     "label pair alone",
			args:     []string{"PROJ-123", "--label-old", "FY26-Q3", "--label-new", "FY26-Q4"},
			hasLabel: true,
		},
		{
			name:       "parent + summary pair",
			args:       []string{"PROJ-123", "--parent", "PROJ-001", "--summary-old", "Q3'26", "--summary-new", "Q4'26"},
			hasParent:  true,
			hasSummary: true,
		},
		{
			name:      "parent + label pair",
			args:      []string{"PROJ-123", "--parent", "PROJ-001", "--label-old", "FY26-Q3", "--label-new", "FY26-Q4"},
			hasParent: true,
			hasLabel:  true,
		},
		{
			name:       "summary pair + label pair",
			args:       []string{"PROJ-123", "--summary-old", "Q3'26", "--summary-new", "Q4'26", "--label-old", "FY26-Q3", "--label-new", "FY26-Q4"},
			hasSummary: true,
			hasLabel:   true,
		},
		{
			name: "parent + summary pair + label pair",
			args: []string{
				"PROJ-123",
				"--parent", "PROJ-001",
				"--summary-old", "Q3'26", "--summary-new", "Q4'26",
				"--label-old", "FY26-Q3", "--label-new", "FY26-Q4",
			},
			hasParent:  true,
			hasSummary: true,
			hasLabel:   true,
		},
	}

	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if err := auth.Save("", &auth.Config{
				Provider: auth.ProviderAPIToken,
				Site:     "acme.atlassian.net",
				APIToken: &auth.APITokenConfig{Email: "test@example.com", Token: "test-token"},
			}); err != nil {
				t.Fatalf("auth.Save: %v", err)
			}

			issues := map[string]moveTestIssue{
				"PROJ-123": {summary: "Widget Q3'26", labels: []string{"FY26-Q3", "team-widgets"}},
				"PROJ-001": {summary: "Parent", labels: nil},
			}
			srv, calls := newMoveFlagBindingServer(t, issues)
			defer srv.Close()
			http.DefaultTransport = moveServerTransport{srv: srv, next: originalTransport}

			var err error
			out := captureStdout(t, func() { err = runMove(tt.args) })
			if err != nil {
				t.Fatalf("runMove: unexpected error: %v (stdout: %q)", err, out)
			}
			if !strings.Contains(out, "Done.") {
				t.Errorf("stdout = %q, want it to report a completed move", out)
			}

			want := moveWriteCallsWant(tt.hasParent, tt.hasSummary, tt.hasLabel)
			if !reflect.DeepEqual(*calls, want) {
				t.Errorf("write calls = %+v, want %+v (a parsed flag value landed in the wrong MoveInput field)", *calls, want)
			}
		})
	}
}

var moveDescendantJQLPattern = regexp.MustCompile(`\Aparent = "(.*)"\z`)

type moveDescendantServerOpts struct {
	childrenOfFailStatus map[string]int
	childrenOfFailBody   map[string]string
	writeFailStatus      map[string]int
	writeFailBody        map[string]string
}

func newMoveDescendantServer(t *testing.T, issues map[string]moveTestIssue, children map[string][]string, opts moveDescendantServerOpts) (*httptest.Server, *[]moveWriteCall, *int) {
	t.Helper()
	var calls []moveWriteCall
	childrenOfCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		childrenOfCalls++
		jql := r.URL.Query().Get("jql")
		m := moveDescendantJQLPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		key := m[1]
		if status, ok := opts.childrenOfFailStatus[key]; ok {
			w.WriteHeader(status)
			if body, ok := opts.childrenOfFailBody[key]; ok {
				_, _ = w.Write([]byte(body))
			}
			return
		}
		var b strings.Builder
		b.WriteString(`{"issues":[`)
		for i, childKey := range children[key] {
			child := issues[childKey]
			if i > 0 {
				b.WriteString(",")
			}
			body, err := json.Marshal(map[string]any{
				"id":  "1",
				"key": childKey,
				"fields": map[string]any{
					"summary":   child.summary,
					"labels":    child.labels,
					"issuetype": map[string]string{"id": "1", "name": "Task"},
					"project":   map[string]string{"id": "1", "key": "PROJ"},
				},
			})
			if err != nil {
				t.Fatalf("encode fixture issue: %v", err)
			}
			b.Write(body)
		}
		b.WriteString(`]}`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b.String()))
	})
	mux.HandleFunc("/rest/api/3/issue/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		switch r.Method {
		case http.MethodGet:
			issue, ok := issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			body, err := json.Marshal(map[string]any{
				"id":  "1",
				"key": key,
				"fields": map[string]any{
					"summary":   issue.summary,
					"labels":    issue.labels,
					"issuetype": map[string]string{"id": "1", "name": "Task"},
					"project":   map[string]string{"id": "1", "key": "PROJ"},
				},
			})
			if err != nil {
				t.Fatalf("encode fixture issue: %v", err)
			}
			_, _ = w.Write(body)
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			calls = append(calls, moveWriteCall{method: r.Method, key: key, fields: decoded.Fields})
			if status, ok := opts.writeFailStatus[key]; ok {
				w.WriteHeader(status)
				if body, ok := opts.writeFailBody[key]; ok {
					_, _ = w.Write([]byte(body))
				}
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method during descendant pass: %s %s", r.Method, r.URL.Path)
		}
	})
	return httptest.NewServer(mux), &calls, &childrenOfCalls
}

func moveDescendantTestLogin(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: "test@example.com", Token: "test-token"},
	}); err != nil {
		t.Fatalf("auth.Save: %v", err)
	}
}

func TestRunMove_DescendantPass_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	moveDescendantTestLogin(t)

	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget Q3", labels: []string{"FY26-Q3"}},
		"PROJ-124": {summary: "Widget Q3 child", labels: []string{"FY26-Q3"}},
		"PROJ-125": {summary: "unrelated summary", labels: []string{"team-widgets"}},
		"PROJ-126": {summary: "Widget Q3 grandchild", labels: []string{"FY26-Q3"}},
	}
	children := map[string][]string{
		"PROJ-123": {"PROJ-124", "PROJ-125"},
		"PROJ-124": {"PROJ-126"},
	}
	srv, calls, childrenOfCalls := newMoveDescendantServer(t, issues, children, moveDescendantServerOpts{})
	defer srv.Close()
	http.DefaultTransport = moveServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() {
		err = runMove([]string{"PROJ-123", "--summary-old", "Q3", "--summary-new", "Q4", "--label-old", "FY26-Q3", "--label-new", "FY26-Q4"})
	})
	if err != nil {
		t.Fatalf("runMove: unexpected error: %v (stdout: %q)", err, out)
	}
	for _, want := range []string{
		"Descendant PROJ-124 (depth 1, parent PROJ-123): summary and labels rewritten.",
		"Descendant PROJ-125 (depth 1, parent PROJ-123): no match; left untouched.",
		"Descendant PROJ-126 (depth 2, parent PROJ-124): summary and labels rewritten.",
		"Done.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, missing expected line %q", out, want)
		}
	}
	if *childrenOfCalls == 0 {
		t.Error("expected at least one ChildrenOf request, got 0")
	}
	for _, c := range *calls {
		if c.key == "PROJ-123" {
			continue
		}
		if _, hasParent := c.fields["parent"]; hasParent {
			t.Errorf("descendant write to %s carried a parent field: %+v", c.key, c.fields)
		}
	}
}

func TestRunMove_ParentOnly_NoDescendantWalk(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	moveDescendantTestLogin(t)

	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget", labels: nil},
		"PROJ-001": {summary: "Parent", labels: nil},
	}
	srv, _, childrenOfCalls := newMoveDescendantServer(t, issues, nil, moveDescendantServerOpts{})
	defer srv.Close()
	http.DefaultTransport = moveServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() { err = runMove([]string{"PROJ-123", "--parent", "PROJ-001"}) })
	if err != nil {
		t.Fatalf("runMove: unexpected error: %v (stdout: %q)", err, out)
	}
	if *childrenOfCalls != 0 {
		t.Errorf("ChildrenOf requests = %d, want 0 for a --parent-only move", *childrenOfCalls)
	}
	if strings.Contains(out, "Descendant ") || strings.Contains(out, "Descendants:") {
		t.Errorf("stdout unexpectedly mentions descendants for a --parent-only move: %q", out)
	}
}

func TestRunMove_DescendantFailures_AreBodyFreeAndExitNonzero(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	moveDescendantTestLogin(t)

	const poison = "Bearer reflected-authorization secret-payload token=abc123\x1b[31m"
	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget Q3", labels: nil},
		"PROJ-124": {summary: "Widget Q3 child", labels: nil},
		"PROJ-125": {summary: "Widget Q3 child2", labels: nil},
	}
	children := map[string][]string{"PROJ-123": {"PROJ-124", "PROJ-125"}}
	srv, _, _ := newMoveDescendantServer(t, issues, children, moveDescendantServerOpts{
		writeFailStatus:      map[string]int{"PROJ-124": http.StatusBadGateway},
		writeFailBody:        map[string]string{"PROJ-124": poison},
		childrenOfFailStatus: map[string]int{"PROJ-125": http.StatusBadGateway},
		childrenOfFailBody:   map[string]string{"PROJ-125": poison},
	})
	defer srv.Close()
	http.DefaultTransport = moveServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() {
		err = runMove([]string{"PROJ-123", "--summary-old", "Q3", "--summary-new", "Q4"})
	})
	if err == nil {
		t.Fatal("runMove: expected a non-nil error (move completed with errors)")
	}
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Errorf("err = %T, want *errSilent (already-printed failure)", err)
	}
	if strings.Contains(out, "reflected-authorization") || strings.Contains(out, "secret-payload") || strings.Contains(out, "token=abc123") {
		t.Errorf("stdout leaked the poisoned response body: %q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("stdout contains an unsanitized escape sequence: %q", out)
	}
	if got := strings.Count(out, "(502)"); got != 2 {
		t.Errorf("stdout contains %d occurrences of \"(502)\", want 2 (one for the descendant write failure, one for the ChildrenOf failure): %q", got, out)
	}
	if strings.Contains(out, "Done.") {
		t.Errorf("stdout = %q, want no Done. line when a descendant failure occurred", out)
	}
}

func TestRunMove_NothingToDo(t *testing.T) {
	err := runMove([]string{"PROJ-123"})
	want := "jirahere: move PROJ-123: nothing to do — give at least one of --parent, --summary-old/--summary-new, or --label-old/--label-new."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunMove_SummaryPairingError(t *testing.T) {
	err := runMove([]string{"PROJ-123", "--summary-old", "Q3'26"})
	want := "jirahere: --summary-old and --summary-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunMove_SummaryPairingError_OnlyNew(t *testing.T) {
	err := runMove([]string{"PROJ-123", "--summary-new", "Q4'26"})
	want := "jirahere: --summary-old and --summary-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunMove_LabelPairingError(t *testing.T) {
	err := runMove([]string{"PROJ-123", "--label-old", "FY26-Q3"})
	want := "jirahere: --label-old and --label-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunMove_LabelPairingError_OnlyNew(t *testing.T) {
	err := runMove([]string{"PROJ-123", "--label-new", "FY26-Q4"})
	want := "jirahere: --label-old and --label-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunMove_RepeatedFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "parent",
			args: []string{"PROJ-123", "--parent", "PROJ-001", "--parent", "PROJ-002"},
			want: "jirahere: --parent may not be given more than once.",
		},
		{
			name: "summary-old",
			args: []string{"PROJ-123", "--summary-old", "a", "--summary-new", "b", "--summary-old", "c"},
			want: "jirahere: --summary-old may not be given more than once.",
		},
		{
			name: "summary-new",
			args: []string{"PROJ-123", "--summary-old", "a", "--summary-new", "b", "--summary-new", "c"},
			want: "jirahere: --summary-new may not be given more than once.",
		},
		{
			name: "label-old",
			args: []string{"PROJ-123", "--label-old", "a", "--label-new", "b", "--label-old", "c"},
			want: "jirahere: --label-old may not be given more than once.",
		},
		{
			name: "label-new",
			args: []string{"PROJ-123", "--label-old", "a", "--label-new", "b", "--label-new", "c"},
			want: "jirahere: --label-new may not be given more than once.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runMove(tt.args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunMove_RepeatedFlag_EqualsAndMixedForms(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "parent, both = form",
			args: []string{"PROJ-123", "--parent=PROJ-001", "--parent=PROJ-002"},
			want: "jirahere: --parent may not be given more than once.",
		},
		{
			name: "parent, separate then =",
			args: []string{"PROJ-123", "--parent", "PROJ-001", "--parent=PROJ-002"},
			want: "jirahere: --parent may not be given more than once.",
		},
		{
			name: "summary-old, both = form",
			args: []string{"PROJ-123", "--summary-old=a", "--summary-old=c", "--summary-new", "b"},
			want: "jirahere: --summary-old may not be given more than once.",
		},
		{
			name: "summary-old, = then separate",
			args: []string{"PROJ-123", "--summary-old=a", "--summary-old", "c", "--summary-new", "b"},
			want: "jirahere: --summary-old may not be given more than once.",
		},
		{
			name: "summary-new, both = form",
			args: []string{"PROJ-123", "--summary-old", "a", "--summary-new=b", "--summary-new=c"},
			want: "jirahere: --summary-new may not be given more than once.",
		},
		{
			name: "summary-new, separate then =",
			args: []string{"PROJ-123", "--summary-old", "a", "--summary-new", "b", "--summary-new=c"},
			want: "jirahere: --summary-new may not be given more than once.",
		},
		{
			name: "label-old, both = form",
			args: []string{"PROJ-123", "--label-old=a", "--label-old=c", "--label-new", "b"},
			want: "jirahere: --label-old may not be given more than once.",
		},
		{
			name: "label-old, = then separate",
			args: []string{"PROJ-123", "--label-old=a", "--label-old", "c", "--label-new", "b"},
			want: "jirahere: --label-old may not be given more than once.",
		},
		{
			name: "label-new, both = form",
			args: []string{"PROJ-123", "--label-old", "a", "--label-new=b", "--label-new=c"},
			want: "jirahere: --label-new may not be given more than once.",
		},
		{
			name: "label-new, separate then =",
			args: []string{"PROJ-123", "--label-old", "a", "--label-new", "b", "--label-new=c"},
			want: "jirahere: --label-new may not be given more than once.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runMove(tt.args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunMove_UnknownFlag_SanitizesStderr(t *testing.T) {
	const poison = "--\x1b[31mBOGUS\x1b[0m\r\nInjected forged line"

	out := captureStderr(t, func() { _ = runMove([]string{"PROJ-123", poison}) })

	if strings.Contains(out, "\x1b") {
		t.Errorf("stderr contains an unsanitized escape sequence: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("stderr contains an unsanitized carriage return: %q", out)
	}
	if want := 1; strings.Count(out, "\n") != want {
		t.Errorf("stderr has %d lines, want %d (the flag package's own dump must be suppressed and the message must print exactly once): %q", strings.Count(out, "\n"), want, out)
	}
}

func TestRunMove_Help(t *testing.T) {
	for _, flagForm := range []string{"-h", "--help"} {
		t.Run(flagForm, func(t *testing.T) {
			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() {
					err = runMove([]string{"PROJ-123", flagForm})
				})
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}
			for _, want := range []string{"-parent value", "-summary-old value", "-summary-new value", "-label-old value", "-label-new value"} {
				if !strings.Contains(stdoutOut, want) {
					t.Errorf("stdout = %q, want it to contain %q (move's option listing)", stdoutOut, want)
				}
			}
			if stderrOut != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderrOut)
			}
		})
	}
}

func TestSplitMoveArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantSource   string
		wantFlagArgs []string
		wantErr      bool
	}{
		{
			name:         "source before flags",
			args:         []string{"PROJ-123", "--parent", "PROJ-001"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--parent", "PROJ-001"},
		},
		{
			name:         "source after flags",
			args:         []string{"--parent", "PROJ-001", "PROJ-123"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--parent", "PROJ-001"},
		},
		{
			name:         "equals form",
			args:         []string{"PROJ-123", "--summary-old=Q3'26", "--summary-new=Q4'26"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--summary-old=Q3'26", "--summary-new=Q4'26"},
		},
		{
			name:    "missing source key",
			args:    []string{"--parent", "PROJ-001"},
			wantErr: true,
		},
		{
			name:    "too many positional args",
			args:    []string{"PROJ-123", "PROJ-456"},
			wantErr: true,
		},
		{
			name:         "-- terminator with only the source key after it",
			args:         []string{"--", "PROJ-123"},
			wantSource:   "PROJ-123",
			wantFlagArgs: nil,
		},
		{
			name:    "-- terminator does not silently drop a recognized flag after it",
			args:    []string{"PROJ-123", "--", "--parent", "PROJ-001"},
			wantErr: true,
		},
		{
			name:         "-- terminator lets a flag-looking token become the positional source key",
			args:         []string{"--", "--not-a-real-flag"},
			wantSource:   "--not-a-real-flag",
			wantFlagArgs: nil,
		},
		{

			name:         "empty-string source key is accepted here; validity is M3's job",
			args:         []string{""},
			wantSource:   "",
			wantFlagArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, flagArgs, err := splitMoveArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitMoveArgs(%v) = (%q, %v, nil), want an error", tt.args, source, flagArgs)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitMoveArgs(%v): unexpected error: %v", tt.args, err)
			}
			if source != tt.wantSource {
				t.Errorf("source = %q, want %q", source, tt.wantSource)
			}
			if len(flagArgs) != len(tt.wantFlagArgs) {
				t.Fatalf("flagArgs = %v, want %v", flagArgs, tt.wantFlagArgs)
			}
			for i := range flagArgs {
				if flagArgs[i] != tt.wantFlagArgs[i] {
					t.Errorf("flagArgs = %v, want %v", flagArgs, tt.wantFlagArgs)
				}
			}
		})
	}
}

func TestPrintMoveResult_NamesExactlyWhatRan(t *testing.T) {
	tests := []struct {
		name string
		res  *command.MoveResult
		want string
	}{
		{
			name: "parent only",
			res: &command.MoveResult{
				SourceKey:       "PROJ-123",
				ParentAttempted: true,
				ParentKey:       "PROJ-001",
			},
			want: "Moving PROJ-123...\n" +
				"Parent set: PROJ-123 -> PROJ-001.\n" +
				"Summary unchanged (not requested).\n" +
				"Labels unchanged (not requested).\n" +
				"Done.\n",
		},
		{
			name: "rewrite pairs only, no parent",
			res: &command.MoveResult{
				SourceKey:        "PROJ-123",
				SummaryAttempted: true,
				SourceSummary:    "Widget Q3",
				NewSummary:       "Widget Q4",
				LabelsAttempted:  true,
				LabelOld:         "FY26-Q3",
				LabelNew:         "FY26-Q4",
				OtherLabels:      []string{"team-widgets"},
			},
			want: "Moving PROJ-123...\n" +
				"Parent unchanged (not requested).\n" +
				`Summary: "Widget Q3" -> "Widget Q4".` + "\n" +
				"Labels: FY26-Q3 -> FY26-Q4 (1 other label carried over unchanged: team-widgets).\n" +
				"Done.\n",
		},
		{
			name: "parent and rewrite together",
			res: &command.MoveResult{
				SourceKey:        "PROJ-123",
				ParentAttempted:  true,
				ParentKey:        "PROJ-001",
				SummaryAttempted: true,
				SourceSummary:    "Widget Q3",
				NewSummary:       "Widget Q4",
				LabelsAttempted:  true,
				LabelOld:         "FY26-Q3",
				LabelNew:         "FY26-Q4",
				OtherLabels:      []string{"team-widgets"},
			},
			want: "Moving PROJ-123...\n" +
				"Parent set: PROJ-123 -> PROJ-001.\n" +
				`Summary: "Widget Q3" -> "Widget Q4".` + "\n" +
				"Labels: FY26-Q3 -> FY26-Q4 (1 other label carried over unchanged: team-widgets).\n" +
				"Done.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() { printMoveResult(tt.res, &command.DescendantMoveResult{}) })
			if out != tt.want {
				t.Errorf("output = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestPrintMoveResult_WriteFailureIsBodyFree(t *testing.T) {
	const poison = "Bearer reflected-authorization secret-payload token=abc123\x1b[31m"
	statusErr := &jira.StatusError{StatusCode: 502, Body: poison}

	tests := []struct {
		name string
		res  *command.MoveResult
	}{
		{
			name: "parent write failure",
			res: &command.MoveResult{
				SourceKey: "PROJ-123", ParentAttempted: true, ParentKey: "PROJ-001", ParentErr: statusErr,
			},
		},
		{
			name: "summary write failure",
			res: &command.MoveResult{
				SourceKey: "PROJ-123", SummaryAttempted: true, SourceSummary: "Widget Q3", NewSummary: "Widget Q4", SummaryErr: statusErr,
			},
		},
		{
			name: "labels write failure",
			res: &command.MoveResult{
				SourceKey: "PROJ-123", LabelsAttempted: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4", LabelsErr: statusErr,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() { printMoveResult(tt.res, &command.DescendantMoveResult{}) })
			if strings.Contains(out, "reflected-authorization") || strings.Contains(out, "secret-payload") || strings.Contains(out, "token=abc123") {
				t.Errorf("output leaked the poisoned response body: %q", out)
			}
			if strings.Contains(out, "\x1b") {
				t.Errorf("output contains an unsanitized escape sequence: %q", out)
			}
			if !strings.Contains(out, "(502)") {
				t.Errorf("output = %q, want it to retain the numeric HTTP status", out)
			}
			if strings.Contains(out, "Done.") {
				t.Errorf("output = %q, want no \"Done.\" line when a write failed", out)
			}
		})
	}
}
