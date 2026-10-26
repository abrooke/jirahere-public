package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

type commentProfileRecorder struct {
	mu    sync.Mutex
	auths []string
}

func (r *commentProfileRecorder) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auths...)
}

func newCommentProfileServer(t *testing.T) *commentProfileRecorder {
	t.Helper()
	rec := &commentProfileRecorder{}
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		if r.URL.Path != "/rest/api/3/issue/PROJ-1/comment" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"10001"}`))
	})
	return rec
}

func runCommentAddCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCommentAdd(args) })
	})
	return stdout, stderr, err
}

func TestRunCommentAdd_Profile_UsesProfileCredentialsOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newCommentProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

	stdout, stderr, err := runCommentAddCapture(t, "PROJ-1", "--body", "hello", "--profile", "work")
	if err != nil {
		t.Fatalf("runCommentAdd: %v (stderr %q)", err, stderr)
	}
	if stdout != "10001\n" || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want the comment id only", stdout, stderr)
	}
	auths := rec.requests()
	if len(auths) != 1 {
		t.Fatalf("%d request(s), want exactly the one comment POST", len(auths))
	}
	if want := basicAuth("work@example.com", "work-token"); auths[0] != want {
		t.Errorf("Authorization = %q, want the work profile's credentials %q", auths[0], want)
	}
}

func TestRunCommentAdd_Profile_FlagOrderIndependent(t *testing.T) {
	for _, args := range [][]string{
		{"--profile", "work", "PROJ-1", "--body", "hello"},
		{"--profile=work", "--body", "hello", "PROJ-1"},
		{"--body", "hello", "--profile", "work", "PROJ-1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			rec := newCommentProfileServer(t)
			saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

			stdout, stderr, err := runCommentAddCapture(t, args...)
			if err != nil || stdout != "10001\n" || stderr != "" {
				t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
			}
			if auths := rec.requests(); len(auths) != 1 || auths[0] != basicAuth("work@example.com", "work-token") {
				t.Errorf("Authorization headers = %v, want only the work profile's", auths)
			}
		})
	}
}

func TestRunCommentAdd_Profile_IsolatedFromLoggedInDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newCommentProfileServer(t)

	stdout, stderr, err := runCommentAddCapture(t, "PROJ-1", "--body", "hello", "--profile", "empty")
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

func TestRunCommentAdd_NoProfile_Unchanged(t *testing.T) {
	t.Run("uses default credentials", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		rec := newCommentProfileServer(t)
		saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

		stdout, stderr, err := runCommentAddCapture(t, "PROJ-1", "--body", "hello")
		if err != nil || stdout != "10001\n" || stderr != "" {
			t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
		}
		if auths := rec.requests(); len(auths) != 1 || auths[0] != basicAuth("test@example.com", "test-token") {
			t.Errorf("Authorization headers = %v, want only the default profile's", auths)
		}
	})
	t.Run("not logged in", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		stdout, stderr, err := runCommentAddCapture(t, "PROJ-1", "--body", "hello")
		if err == nil || stdout != "" {
			t.Fatalf("err %v stdout %q, want a failure with empty stdout", err, stdout)
		}
		if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

func TestRunCommentAdd_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
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
			rec := newCommentProfileServer(t)
			before, _ := os.ReadDir(xdg)

			args := append([]string{"PROJ-1", "--body", "secret-comment-body"}, tc.args...)
			stdout, stderr, err := runCommentAddCapture(t, args...)

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
			if strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "\x1b") || strings.Contains(stderr, "evil") || strings.Contains(stderr, "secret-comment-body") {
				t.Errorf("stderr = %q, want exactly one sanitized line without the name or body echoed", stderr)
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

func TestRunCommentAdd_Profile_CredentialLoadFailureNamesProfileLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newCommentProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")
	path, err := auth.ConfigPath("work")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runCommentAddCapture(t, "PROJ-1", "--body", "hello", "--profile", "work")
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

func TestCommentAdd_Profile_ProcessExitCodeAndHelp(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "comment", "add", "PROJ-1", "--body", "hello", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") || strings.Contains(stderr, "hello") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}

	for _, args := range [][]string{{"comment", "add", "--help"}, {"comment", "--help"}} {
		stdout, stderr, exitCode = runJirahereProcess(t, args...)
		if exitCode != 0 || stderr != "" {
			t.Fatalf("%v: stderr=%q exit=%d, want exit 0 and empty stderr", args, stderr, exitCode)
		}
		if !strings.Contains(stdout, "usage: jirahere comment add") || !strings.Contains(stdout, "--profile profile") || !strings.Contains(stdout, profileFlagUsage[:20]) {
			t.Errorf("%v: stdout = %q, want the canonical --profile listing", args, stdout)
		}
	}
}
