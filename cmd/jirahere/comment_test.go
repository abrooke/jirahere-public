package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/auth"
)

type commentRoundTripper func(*http.Request) (*http.Response, error)

func (f commentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func commentTestLogin(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	createTestLogin(t, handler)
}

func TestRunCommentAdd_PreflightErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{"--body", "hello"}, commentAddUsage},
		{"empty id", []string{"", "--body", "hello"}, commentAddUsage},
		{"surplus positional", []string{"PROJ-1", "extra", "--body", "hello"}, commentAddUsage},
		{"missing body", []string{"PROJ-1"}, "jirahere: --body is required and must not be empty."},
		{"empty body", []string{"PROJ-1", "--body", ""}, "jirahere: --body is required and must not be empty."},
		{"bad flag before a surplus positional", []string{"PROJ-1", "--bad", "value"}, "jirahere: flag provided but not defined: --bad"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runCommentAdd(tt.args) })
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %T, want *errSilent", err)
			}
			if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want exactly one line", stderr)
			}
		})
	}
}

func TestRunCommentAdd_PostsADFAndPrintsID(t *testing.T) {
	var posts int
	const commentText = "first line\nsecond line"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		if got, want := r.URL.Path, "/rest/api/3/issue/PROJ-1/comment"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		var payload struct {
			Body json.RawMessage `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got, want := string(payload.Body), string(adf.FromPlainText(commentText)); got != want {
			t.Errorf("ADF body = %s, want %s", got, want)
		}
		_, _ = w.Write([]byte(`{"id":"10001"}`))
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentAdd([]string{"PROJ-1", "--body", commentText}) })
	})
	if err != nil {
		t.Fatalf("runCommentAdd: %v", err)
	}
	if posts != 1 || stdout != "10001\n" || stderr != "" {
		t.Errorf("posts/stdout/stderr = %d/%q/%q", posts, stdout, stderr)
	}
}

func TestRunCommentAdd_SanitizesJiraAssignedID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"10001\n\u001b[31mforged\u202e"}`))
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentAdd([]string{"PROJ-1", "--body", "hello"}) })
	})
	if err != nil {
		t.Fatalf("runCommentAdd: %v", err)
	}
	if got, want := stdout, "10001[31mforged\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr != "" || strings.ContainsAny(stdout, "\x1b\r\u202e") || strings.Count(stdout, "\n") != 1 {
		t.Errorf("stdout/stderr = %q/%q, want one sanitized stdout line only", stdout, stderr)
	}
}

func TestRunCommentAdd_FailuresAndPoisonedBodyNeverEcho(t *testing.T) {
	poisoned := "comment\x1b[31m\r\nforged\u202e"
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("untrusted proxy response"))
			})
			commentTestLogin(t, handler)
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runCommentAdd([]string{"PROJ-404", "--body", poisoned}) })
			})
			if err == nil || stdout != "" || strings.Count(stderr, "\n") != 1 {
				t.Errorf("err/stdout/stderr = %v/%q/%q", err, stdout, stderr)
			}
			if strings.ContainsAny(stdout+stderr, "\x1b\r\u202e") || strings.Contains(stdout+stderr, "forged") || strings.Contains(stdout+stderr, "proxy response") {
				t.Errorf("output exposed poisoned body or response text: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestRunCommentAdd_TransportFailureHasSanitizedStderrOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: "user@example.com", Token: "token"},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	originalTransport := http.DefaultTransport
	http.DefaultTransport = commentRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("untrusted transport failure\n\x1b[31m")
	})
	defer func() { http.DefaultTransport = originalTransport }()

	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentAdd([]string{"PROJ-1", "--body", "hello"}) })
	})
	if err == nil {
		t.Fatal("runCommentAdd returned nil error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if got, want := stderr, "jirahere: comment add failed: could not reach Jira.\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if strings.Count(stderr, "\n") != 1 || strings.ContainsAny(stderr, "\x1b\r") {
		t.Errorf("stderr was not one sanitized line: %q", stderr)
	}
}

func TestRunComment_HelpAndGroupValidation(t *testing.T) {
	var err error
	stdout := captureStdout(t, func() { err = runComment([]string{"add", "--help"}) })
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(stdout, "usage: jirahere comment add") {
		t.Errorf("help err/stdout = %v/%q", err, stdout)
	}
	stdout = captureStdout(t, func() { err = runComment([]string{"list", "--help"}) })
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(stdout, "usage: jirahere comment list") {
		t.Errorf("list help err/stdout = %v/%q", err, stdout)
	}
	stderr := captureStderr(t, func() { err = runComment([]string{"bogus"}) })
	if err == nil || strings.TrimSuffix(stderr, "\n") != "usage: jirahere comment <add|list> ..." {
		t.Errorf("group validation err/stderr = %v/%q", err, stderr)
	}
}

func TestRunCommentList_PreflightErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{}, commentListUsage},
		{"empty id", []string{""}, commentListUsage},
		{"surplus positional", []string{"PROJ-1", "extra"}, commentListUsage},
		{"bad flag", []string{"--bad", "PROJ-1"}, "jirahere: flag provided but not defined: --bad"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runCommentList(tt.args) })
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %T, want *errSilent", err)
			}
			if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want exactly one line", stderr)
			}
		})
	}
}

func TestRunCommentList_SinglePage_OrderPreserved(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/rest/api/3/issue/PROJ-1/comment"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":2,"comments":[
			{"id":"1","author":{"displayName":"Alice"},"created":"2026-01-01T00:00:00.000+0000","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}},
			{"id":"2","author":{"displayName":"Bob"},"created":"2026-01-02T00:00:00.000+0000","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}}
		]}`))
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1"}) })
	})
	if err != nil {
		t.Fatalf("runCommentList: %v", err)
	}
	want := "Alice\t2026-01-01T00:00:00.000+0000\nfirst\n\nBob\t2026-01-02T00:00:00.000+0000\nsecond\n"
	if stdout != want || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want %q/empty", stdout, stderr, want)
	}
}

func TestRunCommentList_MultiPage_Pagination(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("startAt") {
		case "0":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":1,"total":2,"comments":[
				{"id":"1","author":{"displayName":"Alice"},"created":"t1","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}}
			]}`))
		case "1":
			_, _ = w.Write([]byte(`{"startAt":1,"maxResults":1,"total":2,"comments":[
				{"id":"2","author":{"displayName":"Bob"},"created":"t2","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}}
			]}`))
		default:
			t.Fatalf("unexpected startAt %q", r.URL.Query().Get("startAt"))
		}
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1"}) })
	})
	if err != nil {
		t.Fatalf("runCommentList: %v", err)
	}
	want := "Alice\tt1\nfirst\n\nBob\tt2\nsecond\n"
	if stdout != want || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want %q/empty", stdout, stderr, want)
	}
}

func TestRunCommentList_NoComments(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":0,"comments":[]}`))
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1"}) })
	})
	if err != nil {
		t.Fatalf("runCommentList: %v", err)
	}
	if stdout != "no comments on PROJ-1\n" || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q", stdout, stderr)
	}
}

func TestRunCommentList_FailuresAndPoisonedBodyNeverEcho(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("untrusted proxy response"))
			})
			commentTestLogin(t, handler)
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-404"}) })
			})
			if err == nil || stdout != "" || strings.Count(stderr, "\n") != 1 {
				t.Errorf("err/stdout/stderr = %v/%q/%q", err, stdout, stderr)
			}
			if strings.ContainsAny(stdout+stderr, "\x1b\r\u202e") || strings.Contains(stdout+stderr, "proxy response") {
				t.Errorf("output exposed proxy response text: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestRunCommentList_TransportFailureHasSanitizedStderrOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: "user@example.com", Token: "token"},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	originalTransport := http.DefaultTransport
	http.DefaultTransport = commentRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("untrusted transport failure\n\x1b[31m")
	})
	defer func() { http.DefaultTransport = originalTransport }()

	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1"}) })
	})
	if err == nil {
		t.Fatal("runCommentList returned nil error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if got, want := stderr, "jirahere: comment list failed: could not reach Jira.\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if strings.Count(stderr, "\n") != 1 || strings.ContainsAny(stderr, "\x1b\r") {
		t.Errorf("stderr was not one sanitized line: %q", stderr)
	}
}

func TestRunCommentList_PoisonedAuthorAndBodyNeverEchoRaw(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := json.Marshal(map[string]any{
			"startAt":    0,
			"maxResults": 100,
			"total":      1,
			"comments": []map[string]any{{
				"id": "1",
				"author": map[string]any{
					"displayName": "Alice\x1b[31m\r\nforged\u202e",
				},
				"created": "2026-01-01\x1b[31m",
				"body": map[string]any{
					"type": "doc", "version": 1,
					"content": []map[string]any{{
						"type": "paragraph",
						"content": []map[string]any{{
							"type": "text", "text": "hi\x1b[31m\u202eforged",
						}},
					}},
				},
			}},
		})
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		_, _ = w.Write(payload)
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1"}) })
	})
	if err != nil {
		t.Fatalf("runCommentList: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if strings.ContainsAny(stdout, "\x1b\r\u202e") {
		t.Errorf("stdout leaked raw control/ANSI/bidi bytes: %q", stdout)
	}
	if !strings.Contains(stdout, "forged") {
		t.Errorf("stdout = %q, want the sanitized (non-escaped) text preserved", stdout)
	}
}

func TestRunCommentList_JSONNoComments(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":0,"comments":[]}`))
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1", "--json"}) })
	})
	if err != nil {
		t.Fatalf("runCommentList --json: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	var raw map[string]json.RawMessage
	if uerr := json.Unmarshal([]byte(stdout), &raw); uerr != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, stdout)
	}
	if _, ok := raw["comments"]; !ok || len(raw) != 1 {
		t.Fatalf("top-level keys = %v, want exactly {comments}", raw)
	}
	if strings.TrimSpace(string(raw["comments"])) != "[]" {
		t.Errorf("comments = %s, want []", raw["comments"])
	}

	var doc commentListDoc
	if uerr := json.Unmarshal([]byte(stdout), &doc); uerr != nil {
		t.Fatalf("unmarshal into commentListDoc: %v", uerr)
	}
	if doc.Comments == nil || len(doc.Comments) != 0 {
		t.Errorf("doc.Comments = %#v, want non-nil empty slice", doc.Comments)
	}
}

func TestRunCommentList_JSONMultiCommentMatchesTextFieldsAndOrder(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":2,"comments":[
			{"id":"1","author":{"displayName":"Alice"},"created":"2026-01-01T00:00:00.000+0000","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}},
			{"id":"2","author":{"displayName":"Bob"},"created":"2026-01-02T00:00:00.000+0000","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}}
		]}`))
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1", "--json"}) })
	})
	if err != nil {
		t.Fatalf("runCommentList --json: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	var doc commentListDoc
	if uerr := json.Unmarshal([]byte(stdout), &doc); uerr != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, stdout)
	}
	if len(doc.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(doc.Comments))
	}
	want := []commentListItem{
		{ID: "1", Author: "Alice", Created: "2026-01-01T00:00:00.000+0000", Body: "first"},
		{ID: "2", Author: "Bob", Created: "2026-01-02T00:00:00.000+0000", Body: "second"},
	}
	if doc.Comments[0] != want[0] || doc.Comments[1] != want[1] {
		t.Errorf("comments = %+v, want %+v", doc.Comments, want)
	}

	dec := json.NewDecoder(strings.NewReader(stdout))
	var discard any
	if derr := dec.Decode(&discard); derr != nil {
		t.Fatalf("first decode: %v", derr)
	}
	if dec.More() {
		t.Errorf("stdout carries more than one JSON value: %q", stdout)
	}
}

func TestRunCommentList_JSONPoisonedAuthorAndBodyNeverEchoRaw(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := json.Marshal(map[string]any{
			"startAt":    0,
			"maxResults": 100,
			"total":      1,
			"comments": []map[string]any{{
				"id": "1",
				"author": map[string]any{
					"displayName": "Alice\x1b[31m\r\nforged\u202e",
				},
				"created": "2026-01-01\x1b[31m",
				"body": map[string]any{
					"type": "doc", "version": 1,
					"content": []map[string]any{{
						"type": "paragraph",
						"content": []map[string]any{{
							"type": "text", "text": "hi\x1b[31m\u202eforged",
						}},
					}},
				},
			}},
		})
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		_, _ = w.Write(payload)
	})
	commentTestLogin(t, handler)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-1", "--json"}) })
	})
	if err != nil {
		t.Fatalf("runCommentList --json: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if strings.ContainsAny(stdout, "\x1b\r\u202e") {
		t.Errorf("stdout leaked raw control/ANSI/bidi bytes: %q", stdout)
	}
	if !strings.Contains(stdout, "forged") {
		t.Errorf("stdout = %q, want the sanitized (non-escaped) text preserved", stdout)
	}

	var doc commentListDoc
	if uerr := json.Unmarshal([]byte(stdout), &doc); uerr != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, stdout)
	}
	if len(doc.Comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(doc.Comments))
	}
}

func TestRunCommentList_JSONFailuresUnaffected(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("untrusted proxy response"))
			})
			commentTestLogin(t, handler)
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runCommentList([]string{"PROJ-404", "--json"}) })
			})
			if err == nil || stdout != "" || strings.Count(stderr, "\n") != 1 {
				t.Errorf("err/stdout/stderr = %v/%q/%q", err, stdout, stderr)
			}
			if strings.ContainsAny(stdout+stderr, "\x1b\r\u202e") || strings.Contains(stdout+stderr, "proxy response") {
				t.Errorf("output exposed proxy response text: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestMainComment_UsageAndHelpExitBehavior(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
		wantErr  string
	}{
		{"root help", []string{"--help"}, 0, "comment add", ""},
		{"root help lists comment list", []string{"--help"}, 0, "comment list", ""},
		{"add help", []string{"comment", "add", "--help"}, 0, "usage: jirahere comment add", ""},
		{"list help", []string{"comment", "list", "--help"}, 0, "usage: jirahere comment list", ""},
		{"missing id", []string{"comment", "add", "--body", "hello"}, 2, "", commentAddUsage + "\n"},
		{"missing body", []string{"comment", "add", "PROJ-1"}, 2, "", "jirahere: --body is required and must not be empty.\n"},
		{"list missing id", []string{"comment", "list"}, 2, "", commentListUsage + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != tt.wantExit || (tt.wantOut != "" && !strings.Contains(stdout, tt.wantOut)) || (tt.wantOut == "" && stdout != "") || stderr != tt.wantErr {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}
