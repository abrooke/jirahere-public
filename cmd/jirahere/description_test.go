package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/adf"
)

func descriptionTestHandler(t *testing.T, fields string, requests *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*requests++
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/issue/PROJ-1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-1","fields":` + fields + `}`))
	}
}

func TestRunDescription_Dispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}} {
		var err error
		stderr := captureStderr(t, func() { err = runDescription(args) })
		var usageErr *errUsage
		if !errors.As(err, &usageErr) {
			t.Fatalf("args=%v: err = %T, want *errUsage", args, err)
		}
		if got, want := strings.TrimSuffix(stderr, "\n"), "usage: jirahere description <get|set> ..."; got != want {
			t.Errorf("args=%v: stderr = %q, want %q", args, got, want)
		}
	}
}

func TestRunDescriptionGet_PreflightErrorsMakeNoNetworkCall(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing id", nil, descriptionGetUsage},
		{"empty id", []string{""}, descriptionGetUsage},
		{"surplus positional", []string{"PROJ-1", "extra"}, descriptionGetUsage},
		{"unknown flag", []string{"PROJ-1", "--bad"}, "jirahere: flag provided but not defined: --bad"},
		{"json and md together", []string{"PROJ-1", "--json", "--md"}, "jirahere: description get: --json and --md may not be used together"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			commentTestLogin(t, descriptionTestHandler(t, `{}`, &requests))
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runDescriptionGet(tt.args) })
			})
			var usageErr *errUsage
			if !errors.As(err, &usageErr) {
				t.Fatalf("err = %T, want *errUsage", err)
			}
			if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
			if stdout != "" || requests != 0 {
				t.Errorf("stdout/requests = %q/%d, want empty/0", stdout, requests)
			}
		})
	}
}

func TestRunDescriptionGet_PrintsPlainText(t *testing.T) {
	var requests int
	fields := `{"description":{"type":"doc","version":1,"content":[` +
		`{"type":"paragraph","content":[{"type":"text","text":"first line"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}}`
	commentTestLogin(t, descriptionTestHandler(t, fields, &requests))
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runDescriptionGet([]string{"PROJ-1"}) })
	})
	if err != nil {
		t.Fatalf("runDescriptionGet: %v", err)
	}
	if want := "first line\n\nsecond\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" || requests != 1 {
		t.Errorf("stderr/requests = %q/%d, want empty/1", stderr, requests)
	}
}

func TestRunDescriptionGet_EmptyDescriptionPrintsNothing(t *testing.T) {
	for name, fields := range map[string]string{
		"absent":    `{}`,
		"null":      `{"description":null}`,
		"empty doc": `{"description":{"type":"doc","version":1,"content":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var requests int
			commentTestLogin(t, descriptionTestHandler(t, fields, &requests))
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runDescriptionGet([]string{"PROJ-1"}) })
			})
			if err != nil || stdout != "" || stderr != "" {
				t.Errorf("err/stdout/stderr = %v/%q/%q, want nil/empty/empty", err, stdout, stderr)
			}
		})
	}
}

func TestRunDescriptionGet_FailuresArePlainOneLine(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("untrusted \x1b[31mproxy response"))
			})
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runDescriptionGet([]string{"PROJ-1"}) })
			})
			if err == nil || stdout != "" || strings.Count(stderr, "\n") != 1 ||
				!strings.HasPrefix(stderr, "jirahere: description get failed: ") ||
				strings.ContainsAny(stderr, "\x1b\r") || strings.Contains(stderr, "untrusted") {
				t.Errorf("err/stdout/stderr = %v/%q/%q", err, stdout, stderr)
			}
		})
	}
}

func TestRunDescriptionGet_NotLoggedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runDescriptionGet([]string{"PROJ-1"}) })
	})
	if err == nil || stdout != "" || stderr != "jirahere: not logged in. Run \"jirahere login\" first.\n" {
		t.Errorf("err/stdout/stderr = %v/%q/%q", err, stdout, stderr)
	}
}

func TestRunDescriptionGet_StructuredOutput(t *testing.T) {
	text := `{"description":{"type":"doc","version":1,"content":[` +
		`{"type":"paragraph","content":[{"type":"text","text":"first \"line\""}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}}`
	for _, tt := range []struct {
		name, flag, fields, want string
	}{
		{"json", "--json", text, "{\n  \"key\": \"PROJ-1\",\n  \"description\": \"first \\\"line\\\"\\n\\nsecond\"\n}\n"},
		{"md", "--md", text, "# PROJ-1\n\n## Description\n\nfirst \"line\"\n\nsecond\n"},
		{"json empty", "--json", `{}`, "{\n  \"key\": \"PROJ-1\",\n  \"description\": \"\"\n}\n"},
		{"md empty", "--md", `{"description":null}`, "# PROJ-1\n\n## Description\n\n\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			commentTestLogin(t, descriptionTestHandler(t, tt.fields, &requests))
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runDescriptionGet([]string{"PROJ-1", tt.flag}) })
			})
			if err != nil || stderr != "" || requests != 1 {
				t.Fatalf("err/stderr/requests = %v/%q/%d", err, stderr, requests)
			}
			if stdout != tt.want {
				t.Errorf("stdout = %q, want %q", stdout, tt.want)
			}
		})
	}
}

func TestRunDescriptionGet_StructuredOutputUsesCanonicalKey(t *testing.T) {
	for _, tt := range []struct {
		name, typed, flag, respKey, want string
	}{
		{"json lowercase", "proj-1", "--json", "PROJ-1", "{\n  \"key\": \"PROJ-1\",\n  \"description\": \"\"\n}\n"},
		{"md lowercase", "proj-1", "--md", "PROJ-1", "# PROJ-1\n\n## Description\n\n\n"},
		{"json numeric", "10001", "--json", "PROJ-1", "{\n  \"key\": \"PROJ-1\",\n  \"description\": \"\"\n}\n"},
		{"md numeric", "10001", "--md", "PROJ-1", "# PROJ-1\n\n## Description\n\n\n"},
		{"json empty key falls back", "proj-1", "--json", "", "{\n  \"key\": \"proj-1\",\n  \"description\": \"\"\n}\n"},
		{"md empty key falls back", "proj-1", "--md", "", "# proj-1\n\n## Description\n\n\n"},
		{"md hostile key sanitized", "proj-1", "--md", "PROJ-1\x1b[31m\nDone.", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := json.Marshal(tt.respKey)
				_, _ = w.Write([]byte(`{"id":"1","key":` + string(b) + `,"fields":{}}`))
			})
			var err error
			stdout := captureStdout(t, func() {
				_ = captureStderr(t, func() { err = runDescriptionGet([]string{tt.typed, tt.flag}) })
			})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if tt.want == "" {
				if strings.ContainsAny(stdout, "\x1b") || strings.Contains(stdout, "\nDone.") {
					t.Errorf("stdout not sanitized: %q", stdout)
				}
				if !strings.HasPrefix(stdout, "# PROJ-1") {
					t.Errorf("stdout = %q", stdout)
				}
				return
			}
			if stdout != tt.want {
				t.Errorf("stdout = %q, want %q", stdout, tt.want)
			}
		})
	}
}

func TestRunDescriptionGet_StructuredFailureLeavesStdoutEmpty(t *testing.T) {
	for _, flagName := range []string{"--json", "--md"} {
		t.Run(flagName, func(t *testing.T) {
			commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runDescriptionGet([]string{"PROJ-1", flagName}) })
			})
			if err == nil || stdout != "" || strings.Count(stderr, "\n") != 1 {
				t.Errorf("err/stdout/stderr = %v/%q/%q", err, stdout, stderr)
			}
		})
	}
}

func TestRunDescriptionSet_PreflightErrorsMakeNoNetworkCall(t *testing.T) {
	const secret = "SECRETBODY"
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{"--body", secret}, descriptionSetUsage},
		{"empty id", []string{"", "--body", secret}, descriptionSetUsage},
		{"surplus positional", []string{"PROJ-1", "extra", "--body", secret}, descriptionSetUsage},
		{"unknown flag", []string{"PROJ-1", "--body", secret, "--bad"}, "jirahere: flag provided but not defined: --bad"},
		{"missing body", []string{"PROJ-1"}, "jirahere: description set: --body is required and must not be empty or whitespace-only."},
		{"empty body", []string{"PROJ-1", "--body", ""}, "jirahere: description set: --body is required and must not be empty or whitespace-only."},
		{"whitespace body", []string{"PROJ-1", "--body", " \t\n "}, "jirahere: description set: --body is required and must not be empty or whitespace-only."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			commentTestLogin(t, descriptionTestHandler(t, `{}`, &requests))
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runDescription(append([]string{"set"}, tt.args...)) })
			})
			var usageErr *errUsage
			if !errors.As(err, &usageErr) {
				t.Fatalf("err = %T, want *errUsage", err)
			}
			if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
			if strings.Contains(stderr, secret) || strings.Contains(err.Error(), secret) {
				t.Errorf("body echoed in error output")
			}
			if stdout != "" || requests != 0 {
				t.Errorf("stdout/requests = %q/%d, want empty/0", stdout, requests)
			}
		})
	}
}

func TestRunDescriptionSet_WritesDescription(t *testing.T) {
	var requests int
	var gotBody []byte
	commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/PROJ-1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	text := "first\nsecond\n\nthird"
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runDescription([]string{"set", "PROJ-1", "--body", text}) })
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if want := "Description set: PROJ-1\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want exactly 1 (the PUT)", requests)
	}
	wantPayload, _ := json.Marshal(map[string]any{"fields": map[string]any{"description": adf.FromPlainText(text)}})
	if string(gotBody) != string(wantPayload) {
		t.Errorf("PUT payload = %s, want %s", gotBody, wantPayload)
	}
}

func TestRunDescriptionSet_FailedWritePrintsNoSuccessLine(t *testing.T) {
	var requests int
	commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNotFound)
	})
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runDescription([]string{"set", "PROJ-9", "--body", "hello"}) })
	})
	if err == nil {
		t.Fatal("err = nil, want failure")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "description set failed") || strings.Contains(stderr, "hello") {
		t.Errorf("stderr = %q, want body-free description set failure", stderr)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1", requests)
	}
}

func TestRunDescriptionSet_SanitizesIDInSuccessLine(t *testing.T) {
	commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	stdout := captureStdout(t, func() {
		captureStderr(t, func() {
			if err := runDescription([]string{"set", "PROJ-1\x1b[31m\nDone.", "--body", "x"}); err != nil {
				t.Errorf("err = %v", err)
			}
		})
	})
	if strings.Count(stdout, "\n") != 1 || strings.ContainsRune(stdout, 0x1b) {
		t.Errorf("stdout = %q, want one sanitized line", stdout)
	}
}

func TestRunDescriptionSet_PreflightPrecedesCredentialLoad(t *testing.T) {
	const bodyMsg = "jirahere: description set: --body is required and must not be empty or whitespace-only."
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{"--body", "x"}, descriptionSetUsage},
		{"empty id", []string{"", "--body", "x"}, descriptionSetUsage},
		{"surplus positional", []string{"PROJ-1", "extra", "--body", "x"}, descriptionSetUsage},
		{"unknown flag", []string{"PROJ-1", "--body", "x", "--bad"}, "jirahere: flag provided but not defined: --bad"},
		{"missing body", []string{"PROJ-1"}, bodyMsg},
		{"empty body", []string{"PROJ-1", "--body", ""}, bodyMsg},
		{"whitespace body", []string{"PROJ-1", "--body", " \t "}, bodyMsg},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			var err error
			stderr := captureStderr(t, func() { err = runDescription(append([]string{"set"}, tt.args...)) })
			var usageErr *errUsage
			if !errors.As(err, &usageErr) {
				t.Fatalf("err = %v (%T), want *errUsage", err, err)
			}
			if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("valid pre-flight without login", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		var err error
		stderr := captureStderr(t, func() { err = runDescription([]string{"set", "PROJ-1", "--body", "hello"}) })
		if err == nil || !strings.Contains(stderr, `not logged in. Run "jirahere login" first.`) {
			t.Errorf("err = %v, stderr = %q, want a credential error", err, stderr)
		}
	})
}
