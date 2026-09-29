package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

type contextGetProfileRecorder struct {
	mu    sync.Mutex
	auths []string
}

func (r *contextGetProfileRecorder) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auths...)
}

func newContextGetProfileServer(t *testing.T) *contextGetProfileRecorder {
	t.Helper()
	rec := &contextGetProfileRecorder{}
	contextGetTestLogin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		if r.URL.Path != "/rest/api/3/issue/PROJ-1" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-1","fields":{"summary":"s","issuetype":{"id":"1","name":"Task"},"status":{"id":"1","name":"Open"}}}`))
	}))
	t.Cleanup(srv.Close)

	original := http.DefaultTransport
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: original}
	t.Cleanup(func() { http.DefaultTransport = original })
	return rec
}

func runContextGetCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runContextGet(args) })
	})
	return stdout, stderr, err
}

func TestRunContextGet_Profile_UsesProfileCredentialsOnly(t *testing.T) {
	rec := newContextGetProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

	stdout, stderr, err := runContextGetCapture(t, "PROJ-1", "--profile", "work")
	if err != nil {
		t.Fatalf("runContextGet: %v (stderr %q)", err, stderr)
	}
	if stdout == "" || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want non-empty stdout and empty stderr", stdout, stderr)
	}
	auths := rec.requests()
	if len(auths) != 1 {
		t.Fatalf("%d request(s), want exactly the one GetIssue read", len(auths))
	}
	if want := basicAuth("work@example.com", "work-token"); auths[0] != want {
		t.Errorf("Authorization = %q, want the work profile's credentials %q", auths[0], want)
	}
}

func TestRunContextGet_Profile_FlagOrderIndependent(t *testing.T) {
	for _, args := range [][]string{
		{"--profile", "work", "PROJ-1"},
		{"--profile=work", "PROJ-1"},
		{"PROJ-1", "--profile", "work"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			rec := newContextGetProfileServer(t)
			saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

			stdout, stderr, err := runContextGetCapture(t, args...)
			if err != nil || stdout == "" || stderr != "" {
				t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
			}
			if auths := rec.requests(); len(auths) != 1 || auths[0] != basicAuth("work@example.com", "work-token") {
				t.Errorf("Authorization headers = %v, want only the work profile's", auths)
			}
		})
	}
}

func TestRunContextGet_Profile_IsolatedFromLoggedInDefault(t *testing.T) {
	rec := newContextGetProfileServer(t)

	stdout, stderr, err := runContextGetCapture(t, "PROJ-1", "--profile", "empty")
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v (%T), want *errSilent", err, err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	want := "jirahere: not logged in (profile \"empty\"). Run \"jirahere login --profile empty\" first.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("%d request(s) reached Jira; the default profile's credentials were used", n)
	}
}

func TestRunContextGet_NoProfile_Unchanged(t *testing.T) {
	t.Run("uses default credentials", func(t *testing.T) {
		rec := newContextGetProfileServer(t)
		saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

		stdout, stderr, err := runContextGetCapture(t, "PROJ-1")
		if err != nil || stdout == "" || stderr != "" {
			t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
		}
		if auths := rec.requests(); len(auths) != 1 || auths[0] != basicAuth("test@example.com", "test-token") {
			t.Errorf("Authorization headers = %v, want only the default profile's", auths)
		}
	})
	t.Run("not logged in", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		stdout, stderr, err := runContextGetCapture(t, "PROJ-1")
		if err == nil || stdout != "" {
			t.Fatalf("err %v stdout %q, want a failure with empty stdout", err, stdout)
		}
		if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

func TestRunContextGet_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", []string{"--profile", ""}},
		{"empty equals form", []string{"--profile="}},
		{"missing value", []string{"--profile"}},
		{"path traversal", []string{"--profile", "../evil"}},
		{"separator", []string{"--profile", "a/b"}},
		{"leading dot", []string{"--profile", ".hidden"}},
		{"whitespace", []string{"--profile", "a b"}},
		{"too long", []string{"--profile", strings.Repeat("a", 65)}},
		{"control chars", []string{"--profile", "a\x1b[31mb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			rec := newContextGetProfileServer(t)
			before, _ := os.ReadDir(xdg)

			args := append([]string{"PROJ-1"}, tc.args...)
			stdout, stderr, err := runContextGetCapture(t, args...)

			var usage *errUsage
			if !errors.As(err, &usage) {
				t.Fatalf("err = %v (%T), want *errUsage (exit 2)", err, err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "--profile") {
				t.Errorf("stderr = %q, want it to name --profile", stderr)
			}
			if strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "\x1b") || strings.Contains(stderr, "evil") {
				t.Errorf("stderr = %q, want exactly one sanitized line without the name echoed", stderr)
			}
			if n := len(rec.requests()); n != 0 {
				t.Errorf("%d request(s) reached Jira before the usage error", n)
			}
			after, _ := os.ReadDir(xdg)
			if len(after) != len(before) {
				t.Errorf("config dir entries %d -> %d; the usage error touched the filesystem", len(before), len(after))
			}
		})
	}
}

func TestRunContextGet_Profile_CredentialLoadFailureNamesProfileLogin(t *testing.T) {
	rec := newContextGetProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")
	path, err := auth.ConfigPath("work")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runContextGetCapture(t, "PROJ-1", "--profile", "work")
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v (%T), want *errSilent", err, err)
	}
	want := "jirahere: could not load Jira credentials. Run \"jirahere login --profile work\" again.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("%d request(s) reached Jira", n)
	}
}

func TestContextGet_Profile_ProcessExitCodeAndHelp(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "context", "get", "PROJ-123", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "context", "get", "PROJ-123", "--help")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("stderr=%q exit=%d, want exit 0 and empty stderr", stderr, exitCode)
	}
	if !strings.Contains(stdout, "usage: jirahere context get") || !strings.Contains(stdout, "--profile profile") || !strings.Contains(stdout, profileFlagUsage[:20]) {
		t.Errorf("stdout = %q, want the canonical --profile listing", stdout)
	}
}
