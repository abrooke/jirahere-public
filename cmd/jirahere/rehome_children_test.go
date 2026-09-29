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

func TestSplitRehomeChildrenArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantSource   string
		wantFlagArgs []string
		wantErr      bool
	}{
		{
			name:         "old-parent before flags",
			args:         []string{"PROJ-100", "--to", "PROJ-200"},
			wantSource:   "PROJ-100",
			wantFlagArgs: []string{"--to", "PROJ-200"},
		},
		{
			name:         "old-parent after flags",
			args:         []string{"--to", "PROJ-200", "PROJ-100"},
			wantSource:   "PROJ-100",
			wantFlagArgs: []string{"--to", "PROJ-200"},
		},
		{
			name:         "equals form",
			args:         []string{"PROJ-100", "--to=PROJ-200", "--skip-status=Done,Blocked"},
			wantSource:   "PROJ-100",
			wantFlagArgs: []string{"--to=PROJ-200", "--skip-status=Done,Blocked"},
		},
		{
			name:    "missing old-parent key",
			args:    []string{"--to", "PROJ-200"},
			wantErr: true,
		},
		{
			name:    "too many positional args",
			args:    []string{"PROJ-100", "PROJ-999"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, flagArgs, err := splitRehomeChildrenArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitRehomeChildrenArgs(%v) = (%q, %v, nil), want an error", tt.args, source, flagArgs)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitRehomeChildrenArgs(%v): unexpected error: %v", tt.args, err)
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

func TestRunRehomeChildren_NoOldParentKey(t *testing.T) {
	err := runRehomeChildren(nil)
	if err == nil {
		t.Fatal("runRehomeChildren: expected error for missing old-parent key, got nil")
	}
	if !strings.HasPrefix(err.Error(), "usage: jirahere rehome-children ") {
		t.Errorf("err = %q, want it to start with the rehome-children usage string", err.Error())
	}
}

func TestRunRehomeChildren_MissingTo(t *testing.T) {
	err := runRehomeChildren([]string{"PROJ-100"})
	want := "jirahere: rehome-children PROJ-100: --to <new-parent-key> is required."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunRehomeChildren_SummaryPairingError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"old only", []string{"PROJ-100", "--to", "PROJ-200", "--summary-old", "Q3"}},
		{"new only", []string{"PROJ-100", "--to", "PROJ-200", "--summary-new", "Q4"}},
	}
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
			originalTransport := http.DefaultTransport
			defer func() { http.DefaultTransport = originalTransport }()
			srv, _, _ := newRehomeChildrenServer(t, map[string]rehomeChildrenTestIssue{
				"PROJ-100": {summary: "Old parent"},
				"PROJ-200": {summary: "New parent"},
			}, nil)
			defer srv.Close()
			http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

			err := runRehomeChildren(tt.args)
			want := "jirahere: --summary-old and --summary-new must be given together."
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestRunRehomeChildren_LabelPairingError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"old only", []string{"PROJ-100", "--to", "PROJ-200", "--label-old", "FY26-Q3"}},
		{"new only", []string{"PROJ-100", "--to", "PROJ-200", "--label-new", "FY26-Q4"}},
	}
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
			originalTransport := http.DefaultTransport
			defer func() { http.DefaultTransport = originalTransport }()
			srv, _, _ := newRehomeChildrenServer(t, map[string]rehomeChildrenTestIssue{
				"PROJ-100": {summary: "Old parent"},
				"PROJ-200": {summary: "New parent"},
			}, nil)
			defer srv.Close()
			http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

			err := runRehomeChildren(tt.args)
			want := "jirahere: --label-old and --label-new must be given together."
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestRunRehomeChildren_RepeatedFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "to",
			args: []string{"PROJ-100", "--to", "PROJ-200", "--to", "PROJ-201"},
			want: "jirahere: --to may not be given more than once.",
		},
		{
			name: "skip-status",
			args: []string{"PROJ-100", "--to", "PROJ-200", "--skip-status", "Done", "--skip-status", "Blocked"},
			want: "jirahere: --skip-status may not be given more than once.",
		},
		{
			name: "summary-old",
			args: []string{"PROJ-100", "--to", "PROJ-200", "--summary-old", "a", "--summary-new", "b", "--summary-old", "c"},
			want: "jirahere: --summary-old may not be given more than once.",
		},
		{
			name: "label-old",
			args: []string{"PROJ-100", "--to", "PROJ-200", "--label-old", "a", "--label-new", "b", "--label-old", "c"},
			want: "jirahere: --label-old may not be given more than once.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runRehomeChildren(tt.args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunRehomeChildren_UnknownFlag_SanitizesStderr(t *testing.T) {
	const poison = "--\x1b[31mBOGUS\x1b[0m\r\nInjected forged line"

	out := captureStderr(t, func() { _ = runRehomeChildren([]string{"PROJ-100", poison}) })

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

func TestRunRehomeChildren_Help(t *testing.T) {
	for _, flagForm := range []string{"-h", "--help"} {
		t.Run(flagForm, func(t *testing.T) {
			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() {
					err = runRehomeChildren([]string{"PROJ-100", flagForm})
				})
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}
			for _, want := range []string{"-to value", "-skip-status value", "-summary-old value", "-summary-new value", "-label-old value", "-label-new value"} {
				if !strings.Contains(stdoutOut, want) {
					t.Errorf("stdout = %q, want it to contain %q (rehome-children's option listing)", stdoutOut, want)
				}
			}
			if stderrOut != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderrOut)
			}
		})
	}
}

func TestMapRehomeChildrenPreflightError_IsBodyFree(t *testing.T) {
	const secret = "Bearer reflected-authorization secret-payload token=abc123"
	statusErr := &jira.StatusError{StatusCode: 502, Body: secret}
	transportErr := errors.New(secret)

	preflightErrs := []error{

		&command.ErrRehomeOldParentNotFound{Subject: "rehome-children old parent", Key: "PROJ-100"},
		&command.ErrRehomeNewParentNotFound{Subject: "rehome-children new parent", Key: "PROJ-200"},
		command.ErrRehomeSummaryPairIncomplete,
		command.ErrRehomeLabelPairIncomplete,
		&command.ErrRehomeChildrenPreflightUnreachable{Operation: command.RehomeChildrenPreflightOldParent, Key: "PROJ-100", Err: statusErr},
		&command.ErrRehomeChildrenPreflightUnreachable{Operation: command.RehomeChildrenPreflightNewParent, Key: "PROJ-200", Err: transportErr},
	}
	for _, err := range preflightErrs {
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() { _ = mapRehomeChildrenPreflightError(err) })
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
		if strings.ContainsAny(stderr, "\x1b\r") || strings.Count(stderr, "\n") > 1 {
			t.Errorf("unsafe or multi-line text reached stderr for %T: %q", err, stderr)
		}
	}
}

func TestMapRehomeChildrenPreflightError_ParentNotFoundExactStrings(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "old parent",
			err:  &command.ErrRehomeOldParentNotFound{Subject: "rehome-children old parent", Key: "PROJ-100"},
			want: "jirahere: rehome-children old parent PROJ-100 was not found (404).",
		},
		{
			name: "new parent",
			err:  &command.ErrRehomeNewParentNotFound{Subject: "rehome-children new parent", Key: "PROJ-200"},
			want: "jirahere: rehome-children new parent PROJ-200 was not found (404).",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mapped error
			stderr := captureStderr(t, func() { mapped = mapRehomeChildrenPreflightError(tc.err) })
			if mapped == nil || mapped.Error() != tc.want {
				t.Errorf("mapped err = %v, want %q", mapped, tc.want)
			}
			if got := strings.TrimRight(stderr, "\n"); got != tc.want {
				t.Errorf("stderr = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapRehomeChildrenPreflightError_SkipStatusEchoesRawValue(t *testing.T) {
	const raw = "Done,,Blocked\x1b[31m\r\ninjected"
	stderr := captureStderr(t, func() {
		_ = mapRehomeChildrenPreflightError(&command.ErrRehomeSkipStatusInvalid{Raw: raw})
	})
	if !strings.Contains(stderr, "Done,,Blocked") {
		t.Errorf("stderr = %q, want it to echo the malformed --skip-status value for diagnosis", stderr)
	}
	if strings.ContainsAny(stderr, "\x1b\r") || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want control characters stripped and exactly one line", stderr)
	}
}

type rehomeChildrenTestIssue struct {
	summary string
}

type rehomeChildrenWriteCall struct {
	key  string
	body map[string]any
}

var rehomeChildrenParentKeyPattern = regexp.MustCompile(`\Aparent = "(.*)"\z`)

func newRehomeChildrenServer(t *testing.T, issues map[string]rehomeChildrenTestIssue, childrenOf map[string][]string) (srv *httptest.Server, calls *[]string, writeCalls *[]rehomeChildrenWriteCall) {
	t.Helper()
	var callLog []string
	var writes []rehomeChildrenWriteCall
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/issue/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		callLog = append(callLog, r.Method+":"+key)
		switch r.Method {
		case http.MethodGet:
			issue, ok := issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":"` + issue.summary + `","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writes = append(writes, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected Jira request during rehome-children: %s %s", r.Method, r.URL.Path)
		}
	})
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		m := rehomeChildrenParentKeyPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		key := m[1]
		callLog = append(callLog, r.Method+":children:"+key)
		var b strings.Builder
		b.WriteString(`{"issues":[`)
		for i, childKey := range childrenOf[key] {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":"1","key":"` + childKey + `","fields":{"summary":"","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`)
		}
		b.WriteString(`]}`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b.String()))
	})
	return httptest.NewServer(mux), &callLog, &writes
}

type rehomeChildrenServerTransport struct {
	srv  *httptest.Server
	next http.RoundTripper
}

func (rt rehomeChildrenServerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

func rehomeChildrenTestLogin(t *testing.T) {
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

func TestRunRehomeChildren_OrderedPreflight_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()

	issues := map[string]rehomeChildrenTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "old parent not found",
			args: []string{"PROJ-999", "--to", "PROJ-200"},
			want: "jirahere: rehome-children old parent PROJ-999 was not found (404).",
		},
		{
			name: "new parent not found",
			args: []string{"PROJ-100", "--to", "PROJ-999"},
			want: "jirahere: rehome-children new parent PROJ-999 was not found (404).",
		},
		{
			name: "skip-status malformed",
			args: []string{"PROJ-100", "--to", "PROJ-200", "--skip-status", "Done,,Blocked"},
			want: `jirahere: --skip-status "Done,,Blocked" contains an empty or whitespace-only entry.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rehomeChildrenTestLogin(t)
			srv, calls, writeCalls := newRehomeChildrenServer(t, issues, nil)
			defer srv.Close()
			http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

			var err error
			out := captureStdout(t, func() { err = runRehomeChildren(tt.args) })
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q (stdout: %q)", err, tt.want, out)
			}
			for _, c := range *calls {
				if !strings.HasPrefix(c, "GET:") {
					t.Errorf("unexpected non-GET call recorded: %q", c)
				}
			}
			if len(*writeCalls) != 0 {
				t.Errorf("write calls = %+v, want none (pre-flight failure must write nothing)", *writeCalls)
			}
		})
	}
}

func TestRunRehomeChildren_Execution_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()

	rehomeChildrenTestLogin(t)
	issues := map[string]rehomeChildrenTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]string{
		"PROJ-100": {"PROJ-101", "PROJ-102"},
	}
	srv, _, writeCalls := newRehomeChildrenServer(t, issues, childrenOf)
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() { err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200"}) })
	if err != nil {
		t.Fatalf("runRehomeChildren: %v (stdout: %q)", err, out)
	}
	for _, want := range []string{"PROJ-101", "PROJ-102", "Done."} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want it to contain %q", out, want)
		}
	}

	wantWrites := []rehomeChildrenWriteCall{
		{key: "PROJ-101", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-102", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
	}
	if !reflect.DeepEqual(*writeCalls, wantWrites) {
		t.Fatalf("write calls = %+v, want exactly %+v (no other item's parent may change)", *writeCalls, wantWrites)
	}
}

func TestRunRehomeChildren_Execution_NoDirectChildren_NoOpSuccess(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()

	rehomeChildrenTestLogin(t)
	issues := map[string]rehomeChildrenTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	srv, _, writeCalls := newRehomeChildrenServer(t, issues, map[string][]string{})
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() { err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200"}) })
	if err != nil {
		t.Fatalf("runRehomeChildren: %v (stdout: %q), want nil (zero direct children is a no-op success)", err, out)
	}
	if want := "No direct children found; nothing to do."; !strings.Contains(out, want) {
		t.Errorf("stdout = %q, want it to contain %q", out, want)
	}
	if len(*writeCalls) != 0 {
		t.Errorf("write calls = %+v, want none", *writeCalls)
	}
}

type rehomeChildrenRewriteNode struct {
	summary  string
	labels   []string
	children []string
}

type rehomeChildrenRewriteServerOpts struct {
	writeFailStatus map[string]int
	writeFailBody   map[string]string
}

func newRehomeChildrenRewriteServer(t *testing.T, parents map[string]moveTestIssue, tree map[string]rehomeChildrenRewriteNode, opts rehomeChildrenRewriteServerOpts) (srv *httptest.Server, writeCalls *[]rehomeChildrenWriteCall) {
	t.Helper()
	var writes []rehomeChildrenWriteCall
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		m := rehomeChildrenParentKeyPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		node := tree[m[1]]
		var b strings.Builder
		b.WriteString(`{"issues":[`)
		for i, childKey := range node.children {
			child := tree[childKey]
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
			issue, ok := parents[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
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
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writes = append(writes, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			if status, ok := opts.writeFailStatus[key]; ok {
				w.WriteHeader(status)
				if body, ok := opts.writeFailBody[key]; ok {
					_, _ = w.Write([]byte(body))
				}
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method during rehome-children rewrite pass: %s %s", r.Method, r.URL.Path)
		}
	})
	return httptest.NewServer(mux), &writes
}

func TestRunRehomeChildren_DescendantRewrite_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	tree := map[string]rehomeChildrenRewriteNode{
		"PROJ-100":  {children: []string{"PROJ-101", "PROJ-102"}},
		"PROJ-101":  {summary: "Widget Q3", labels: []string{"FY26-Q3"}, children: []string{"PROJ-101A"}},
		"PROJ-101A": {summary: "Widget Q3 grandchild", labels: []string{"FY26-Q3"}, children: nil},
		"PROJ-102":  {summary: "unrelated summary", labels: []string{"team-widgets"}, children: nil},
	}
	srv, writeCalls := newRehomeChildrenRewriteServer(t, parents, tree, rehomeChildrenRewriteServerOpts{})
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() {
		err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200", "--summary-old", "Q3", "--summary-new", "Q4", "--label-old", "FY26-Q3", "--label-new", "FY26-Q4"})
	})
	if err != nil {
		t.Fatalf("runRehomeChildren: unexpected error: %v (stdout: %q)", err, out)
	}
	for _, want := range []string{
		"Reparented PROJ-101 -> PROJ-200.",
		"Rewrite: rehomed child PROJ-101 (parent PROJ-200): summary and labels rewritten.",
		"Rewrite: descendant PROJ-101A (depth 1, parent PROJ-101): summary and labels rewritten.",
		"Reparented PROJ-102 -> PROJ-200.",
		"Rewrite: rehomed child PROJ-102 (parent PROJ-200): no match; left untouched.",
		"Done.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, missing expected line %q", out, want)
		}
	}

	for _, c := range *writeCalls {
		if c.key == "PROJ-101A" {
			if _, hasParent := c.body["parent"]; hasParent {
				t.Errorf("descendant write to %s carried a parent field: %+v", c.key, c.body)
			}
		}
	}
}

type rehomeChildrenStatusChild struct {
	key    string
	status string
}

func newRehomeChildrenSkipStatusServer(t *testing.T, issues map[string]rehomeChildrenTestIssue, childrenOf map[string][]rehomeChildrenStatusChild) (srv *httptest.Server, writeCalls *[]rehomeChildrenWriteCall) {
	t.Helper()
	var writes []rehomeChildrenWriteCall
	mux := http.NewServeMux()
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
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":"` + issue.summary + `","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writes = append(writes, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected Jira request during rehome-children skip-status test: %s %s", r.Method, r.URL.Path)
		}
	})
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		m := rehomeChildrenParentKeyPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		var b strings.Builder
		b.WriteString(`{"issues":[`)
		for i, child := range childrenOf[m[1]] {
			if i > 0 {
				b.WriteString(",")
			}
			body, err := json.Marshal(map[string]any{
				"id":  "1",
				"key": child.key,
				"fields": map[string]any{
					"summary":   "",
					"labels":    []string{},
					"issuetype": map[string]string{"id": "1", "name": "Task"},
					"project":   map[string]string{"id": "1", "key": "PROJ"},
					"status":    map[string]string{"id": "1", "name": child.status},
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
	return httptest.NewServer(mux), &writes
}

func TestRunRehomeChildren_SkipStatus_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	issues := map[string]rehomeChildrenTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {
			{key: "PROJ-101", status: "Done"},
			{key: "PROJ-102", status: "In Progress"},
		},
	}
	srv, writeCalls := newRehomeChildrenSkipStatusServer(t, issues, childrenOf)
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() {
		err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200", "--skip-status", "done"})
	})
	if err != nil {
		t.Fatalf("runRehomeChildren: unexpected error: %v (stdout: %q)", err, out)
	}
	if !strings.Contains(out, "Skipped PROJ-101 (status: Done).") {
		t.Errorf("stdout = %q, want it to name skipped child PROJ-101 and the status that matched (case-insensitive match against --skip-status done)", out)
	}
	if !strings.Contains(out, "Reparented PROJ-102 -> PROJ-200.") {
		t.Errorf("stdout = %q, want it to contain the non-skipped sibling's reparent line", out)
	}

	wantWrites := []rehomeChildrenWriteCall{
		{key: "PROJ-102", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
	}
	if !reflect.DeepEqual(*writeCalls, wantWrites) {
		t.Fatalf("write calls = %+v, want exactly %+v (skipped child PROJ-101 must never be written to)", *writeCalls, wantWrites)
	}
}

func TestRunRehomeChildren_SkipStatus_AllSkipped_NoFalseNoOpLine(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	issues := map[string]rehomeChildrenTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {
			{key: "PROJ-101", status: "Done"},
			{key: "PROJ-102", status: "Blocked"},
		},
	}
	srv, writeCalls := newRehomeChildrenSkipStatusServer(t, issues, childrenOf)
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() {
		err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200", "--skip-status", "Done,Blocked"})
	})
	if err != nil {
		t.Fatalf("runRehomeChildren: unexpected error: %v (stdout: %q)", err, out)
	}
	if strings.Contains(out, "nothing to do") {
		t.Errorf("stdout = %q, must not print the no-direct-children no-op line: children were found and excluded, not absent", out)
	}
	for _, want := range []string{"Skipped PROJ-101 (status: Done).", "Skipped PROJ-102 (status: Blocked).", "Done."} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want it to contain %q", out, want)
		}
	}
	if len(*writeCalls) != 0 {
		t.Errorf("write calls = %+v, want none (every direct child was excluded)", *writeCalls)
	}
}

func TestMapRehomeChildrenExecutionError_IsBodyFree(t *testing.T) {
	const secret = "Bearer reflected-authorization secret-payload token=abc123"
	statusErr := &jira.StatusError{StatusCode: 502, Body: secret}
	transportErr := errors.New(secret)

	executionErrs := []error{
		&command.ErrRehomeChildrenListFailed{OldParentKey: "PROJ-100", Err: statusErr},
		&command.ErrRehomeChildrenListFailed{OldParentKey: "PROJ-100", Err: transportErr},
		&command.ErrRehomeOldParentDeleted{Key: "PROJ-100"},
	}
	for _, err := range executionErrs {
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() { _ = mapRehomeChildrenExecutionError(err) })
		})
		if strings.Contains(stderr, secret) {
			t.Errorf("execution error output exposed secret for %T: %q", err, stderr)
		}
		if strings.Contains(stdout, secret) {
			t.Errorf("execution error stdout exposed secret for %T: %q", err, stdout)
		}
		if stdout != "" {
			t.Errorf("execution error wrote unexpected stdout for %T: %q", err, stdout)
		}
		if strings.ContainsAny(stderr, "\x1b\r") || strings.Count(stderr, "\n") > 1 {
			t.Errorf("unsafe or multi-line text reached stderr for %T: %q", err, stderr)
		}
	}
}

func TestRehomeChildrenFailureOutput_IsBodyFree(t *testing.T) {
	const secret = "Bearer reflected-authorization secret-payload token=abc123"
	statusErr := &jira.StatusError{StatusCode: 502, Body: secret}
	transportErr := errors.New(secret)

	for _, operation := range []rehomeChildrenFailureOperation{
		rehomeChildrenFailureListChildren,
		rehomeChildrenFailureReparent,
		rehomeChildrenFailureSummary,
		rehomeChildrenFailureLabels,
		rehomeChildrenFailureChildrenOf,
		rehomeChildrenFailureNodeCeiling,
	} {
		for _, err := range []error{statusErr, transportErr} {
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() {
					_ = failf("jirahere: %s. Repair the affected item using the Jira web UI.", renderRehomeChildrenFailure(rehomeChildrenFailureContext{
						Operation: operation,
						IssueKey:  "PROJ-123",
						ParentKey: "PROJ-100",
						Depth:     2,
						Err:       err,
					}))
				})
			})
			if strings.Contains(stderr, secret) {
				t.Errorf("rehome-children output exposed secret for operation %d: %q", operation, stderr)
			}
			if strings.Contains(stdout, secret) {
				t.Errorf("rehome-children stdout exposed secret for operation %d: %q", operation, stdout)
			}
			if stdout != "" {
				t.Errorf("rehome-children wrote unexpected stdout for operation %d: %q", operation, stdout)
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

func TestRewriteOutcomeLines_ReportsActualOutcome(t *testing.T) {
	succeeded := command.RewriteOutcome{
		Key: "PROJ-101", ParentKey: "PROJ-200", Depth: 0,
		SummaryAttempted: true, LabelsAttempted: true,
	}
	lines := rewriteOutcomeLines(succeeded)
	if len(lines) != 1 || !strings.Contains(lines[0], "summary and labels rewritten") {
		t.Errorf("rewriteOutcomeLines(succeeded) = %v, want a single line reporting both writes as rewritten (succeeded), not merely attempted", lines)
	}
	if strings.Contains(lines[0], "attempted") {
		t.Errorf("rewriteOutcomeLines(succeeded) = %v, must not say \"attempted\" for a write that actually succeeded", lines)
	}

	const poison = "Bearer reflected-authorization secret-payload token=abc123\x1b[31m"
	statusErr := &jira.StatusError{StatusCode: 502, Body: poison}
	failed := command.RewriteOutcome{
		Key: "PROJ-101", ParentKey: "PROJ-200", Depth: 0,
		SummaryAttempted: true, SummaryErr: statusErr,
		LabelsAttempted: true, LabelsErr: statusErr,
	}
	lines = rewriteOutcomeLines(failed)
	if len(lines) != 2 {
		t.Fatalf("rewriteOutcomeLines(failed) = %v, want one line per failed write (summary and labels)", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "reflected-authorization") || strings.Contains(l, "secret-payload") || strings.Contains(l, "token=abc123") {
			t.Errorf("rewriteOutcomeLines(failed) leaked the poisoned response body: %q", l)
		}
		if strings.Contains(l, "\x1b") {
			t.Errorf("rewriteOutcomeLines(failed) contains an unsanitized escape sequence: %q", l)
		}
		if !strings.Contains(l, "(502)") {
			t.Errorf("rewriteOutcomeLines(failed) = %q, want it to retain the numeric HTTP status", l)
		}
		if strings.Contains(l, "rewritten") {
			t.Errorf("rewriteOutcomeLines(failed) = %q, must not report a failed write as rewritten (succeeded)", l)
		}
	}
}

func TestRunRehomeChildren_SetParentFailureIsolation_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	issues := map[string]rehomeChildrenTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]string{"PROJ-100": {"PROJ-101", "PROJ-102", "PROJ-103"}}

	var writeCalls []rehomeChildrenWriteCall
	mux := http.NewServeMux()
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
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":"` + issue.summary + `","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writeCalls = append(writeCalls, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			if key == "PROJ-102" {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method during SetParent-isolation test: %s %s", r.Method, r.URL.Path)
		}
	})
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		m := rehomeChildrenParentKeyPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		var b strings.Builder
		b.WriteString(`{"issues":[`)
		for i, childKey := range childrenOf[m[1]] {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":"1","key":"` + childKey + `","fields":{"summary":"","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`)
		}
		b.WriteString(`]}`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b.String()))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() { err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200"}) })
	if err == nil {
		t.Fatal("runRehomeChildren: expected a non-nil error (PROJ-102's SetParent failure)")
	}
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Errorf("err = %T, want *errSilent (already-printed failure)", err)
	}
	for _, want := range []string{"Reparented PROJ-101 -> PROJ-200.", "Reparented PROJ-103 -> PROJ-200."} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, missing expected line %q (siblings must still process despite PROJ-102's failure)", out, want)
		}
	}
	if !strings.Contains(out, "(502)") {
		t.Errorf("stdout = %q, want it to retain the numeric HTTP status for PROJ-102's failure", out)
	}
	if strings.Contains(out, "Reparented PROJ-102") {
		t.Errorf("stdout = %q, must not report PROJ-102 as reparented", out)
	}
	if strings.Contains(out, "Done.\n") {
		t.Errorf("stdout = %q, want no \"Done.\" line when a failure occurred", out)
	}

	wantKeys := []string{"PROJ-101", "PROJ-102", "PROJ-103"}
	if len(writeCalls) != len(wantKeys) {
		t.Fatalf("write calls = %+v, want exactly one per key in %v (every direct child must still be attempted)", writeCalls, wantKeys)
	}
	for i, want := range wantKeys {
		if writeCalls[i].key != want {
			t.Errorf("write calls = %+v, want keys %v in order", writeCalls, wantKeys)
		}
	}
}

type rehomeChildrenTaxonomyNode struct {
	status   string
	summary  string
	labels   []string
	children []string
}

func newRehomeChildrenTaxonomyServer(t *testing.T, parents map[string]moveTestIssue, tree map[string]rehomeChildrenTaxonomyNode, setParentFailStatus map[string]int) (srv *httptest.Server, writeCalls *[]rehomeChildrenWriteCall) {
	t.Helper()
	var writes []rehomeChildrenWriteCall
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		m := rehomeChildrenParentKeyPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		node := tree[m[1]]
		var b strings.Builder
		b.WriteString(`{"issues":[`)
		for i, childKey := range node.children {
			child := tree[childKey]
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
					"status":    map[string]string{"id": "1", "name": child.status},
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
			issue, ok := parents[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
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
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writes = append(writes, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			if _, hasParent := decoded.Fields["parent"]; hasParent {
				if status, ok := setParentFailStatus[key]; ok {
					w.WriteHeader(status)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method during rehome-children taxonomy test: %s %s", r.Method, r.URL.Path)
		}
	})
	return httptest.NewServer(mux), &writes
}

func TestRunRehomeChildren_MixedTaxonomy_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	tree := map[string]rehomeChildrenTaxonomyNode{
		"PROJ-100": {children: []string{"PROJ-101", "PROJ-102", "PROJ-103", "PROJ-104"}},
		"PROJ-101": {status: "Done", summary: "Q3 done child"},
		"PROJ-102": {status: "In Progress", summary: "Q3 active child"},
		"PROJ-103": {status: "In Progress", summary: "Q3 matching child"},
		"PROJ-104": {status: "In Progress", summary: "unrelated summary"},
	}
	srv, writeCalls := newRehomeChildrenTaxonomyServer(t, parents, tree, map[string]int{"PROJ-102": http.StatusBadGateway})
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() {
		err = runRehomeChildren([]string{
			"PROJ-100", "--to", "PROJ-200",
			"--skip-status", "Done",
			"--summary-old", "Q3", "--summary-new", "Q4",
		})
	})
	if err == nil {
		t.Fatal("runRehomeChildren: expected a non-nil error (PROJ-102's SetParent failure)")
	}
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Errorf("err = %T, want *errSilent (already-printed failure)", err)
	}

	for _, want := range []string{
		"Skipped PROJ-101 (status: Done).",
		"Reparented PROJ-103 -> PROJ-200.",
		"Rewrite: rehomed child PROJ-103 (parent PROJ-200): summary rewritten.",
		"Reparented PROJ-104 -> PROJ-200.",
		"Rewrite: rehomed child PROJ-104 (parent PROJ-200): no match; left untouched.",
		"(502)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, missing expected line/text %q", out, want)
		}
	}
	if strings.Contains(out, "Reparented PROJ-102") {
		t.Errorf("stdout = %q, must not report failed child PROJ-102 as reparented", out)
	}
	if strings.Contains(out, "Done.\n") {
		t.Errorf("stdout = %q, want no \"Done.\" line when a failure occurred", out)
	}

	for _, c := range *writeCalls {
		if c.key == "PROJ-101" {
			t.Errorf("write call %+v unexpectedly targeted skipped child PROJ-101", c)
		}
	}
}

func newRehomeChildrenDeletedParentServer(t *testing.T, oldParentKey, newParentKey string, childrenOfStatus int, childrenOfBody string) *httptest.Server {
	t.Helper()
	oldParentReads := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			if childrenOfStatus != 0 {
				w.WriteHeader(childrenOfStatus)
				_, _ = w.Write([]byte(childrenOfBody))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issues":[]}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			switch key {
			case oldParentKey:
				oldParentReads++
				if oldParentReads > 1 {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":"Old parent","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
			case newParentKey:
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":"New parent","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			t.Fatalf("unexpected request during deleted-old-parent test: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestRunRehomeChildren_NoDirectChildren_OldParentDeleted_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	srv := newRehomeChildrenDeletedParentServer(t, "PROJ-100", "PROJ-200", 0, "")
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200"}) })
	})
	if err == nil {
		t.Fatal("runRehomeChildren: expected a non-nil error (old parent deleted before execution)")
	}
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Errorf("err = %T, want *errSilent (already-printed failure)", err)
	}
	if strings.Contains(out, "nothing to do") {
		t.Errorf("stdout = %q, must not report the false-negative no-op line for a deleted old parent", out)
	}
	if !strings.Contains(stderr, "PROJ-100") || !strings.Contains(stderr, "deleted") {
		t.Errorf("stderr = %q, want it to name PROJ-100 and say it was deleted", stderr)
	}
}

func TestRunRehomeChildren_ChildrenOfFails_OldParentDeleted_EndToEnd(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	const poison = "Bearer reflected-authorization secret-payload token=abc123"
	srv := newRehomeChildrenDeletedParentServer(t, "PROJ-100", "PROJ-200", http.StatusBadGateway, poison)
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200"}) })
	})
	if err == nil {
		t.Fatal("runRehomeChildren: expected a non-nil error (old parent deleted before execution)")
	}
	if strings.Contains(out, poison) || strings.Contains(stderr, poison) {
		t.Errorf("deleted-old-parent output leaked the ChildrenOf failure's poisoned body: stdout=%q stderr=%q", out, stderr)
	}
	if !strings.Contains(stderr, "PROJ-100") || !strings.Contains(stderr, "deleted") {
		t.Errorf("stderr = %q, want it to name PROJ-100 and say it was deleted", stderr)
	}
}

func TestRunRehomeChildren_RewriteServer_FailedReparentSkipsRewrite(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	rehomeChildrenTestLogin(t)

	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	tree := map[string]rehomeChildrenRewriteNode{
		"PROJ-100": {children: []string{"PROJ-101", "PROJ-102"}},
		"PROJ-101": {summary: "Widget Q3", labels: []string{"FY26-Q3"}},
		"PROJ-102": {summary: "Widget Q3 too", labels: []string{"FY26-Q3"}},
	}
	const poison = "Bearer reflected-authorization secret-payload token=abc123\x1b[31m"
	srv, writeCalls := newRehomeChildrenRewriteServer(t, parents, tree, rehomeChildrenRewriteServerOpts{
		writeFailStatus: map[string]int{"PROJ-101": http.StatusBadGateway},
		writeFailBody:   map[string]string{"PROJ-101": poison},
	})
	defer srv.Close()
	http.DefaultTransport = rehomeChildrenServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() {
		err = runRehomeChildren([]string{"PROJ-100", "--to", "PROJ-200", "--summary-old", "Q3", "--summary-new", "Q4"})
	})
	if err == nil {
		t.Fatal("runRehomeChildren: expected a non-nil error (PROJ-101's SetParent write failed)")
	}
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Errorf("err = %T, want *errSilent", err)
	}
	if strings.Contains(out, "reflected-authorization") || strings.Contains(out, "secret-payload") || strings.Contains(out, "token=abc123") {
		t.Errorf("stdout leaked the poisoned response body: %q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("stdout contains an unsanitized escape sequence: %q", out)
	}
	if !strings.Contains(out, "(502)") {
		t.Errorf("stdout = %q, want it to retain the numeric HTTP status for PROJ-101's failed write", out)
	}
	if strings.Contains(out, "Reparented PROJ-101") {
		t.Errorf("stdout = %q, must not report PROJ-101 as reparented", out)
	}
	if !strings.Contains(out, "Reparented PROJ-102 -> PROJ-200.") {
		t.Errorf("stdout = %q, want PROJ-102's independent reparent still reported", out)
	}
	if !strings.Contains(out, "Rewrite: rehomed child PROJ-102 (parent PROJ-200): summary rewritten.") {
		t.Errorf("stdout = %q, want PROJ-102's independent, successful rewrite reported as succeeded (rewritten), not merely attempted", out)
	}
	if strings.Contains(out, "Done.\n") {
		t.Errorf("stdout = %q, want no \"Done.\" line when a write failed", out)
	}

	for _, c := range *writeCalls {
		if c.key == "PROJ-101" {
			if _, has := c.body["summary"]; has {
				t.Errorf("write call %+v unexpectedly attempted PROJ-101's summary rewrite despite its failed reparent", c)
			}
		}
	}
}

func TestPrintRehomeChildrenResult_ReparentedOnly(t *testing.T) {
	res := &command.RehomeChildrenResult{
		OldParentKey: "PROJ-100",
		NewParentKey: "PROJ-200",
		ChildKeys:    []string{"PROJ-101"},
		HasSummary:   false,
		HasLabel:     false,
	}

	out := captureStdout(t, func() { printRehomeChildrenResult(res) })

	if want := "Reparented PROJ-101 -> PROJ-200."; !strings.Contains(out, want) {
		t.Errorf("stdout = %q, want it to contain the reparented child's line %q", out, want)
	}
	if strings.Contains(out, "Rewrite:") {
		t.Errorf("stdout = %q, want no rewrite lines for a reparented-only child (no rewrite was requested)", out)
	}
}

func TestPrintRehomeChildrenResult_ReparentedOnly_OutputCharacterization(t *testing.T) {
	res := &command.RehomeChildrenResult{
		OldParentKey: "PROJ-100",
		NewParentKey: "PROJ-200",
		ChildKeys:    []string{"PROJ-101"},
		HasSummary:   false,
		HasLabel:     false,
	}

	out := captureStdout(t, func() { printRehomeChildrenResult(res) })

	const want = "Rehoming PROJ-100's children to PROJ-200...\n" +
		"Reparented PROJ-101 -> PROJ-200.\n" +
		"Done.\n"
	if out != want {
		t.Errorf("printRehomeChildrenResult stdout =\n%q\nwant\n%q", out, want)
	}
}
