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

type rehomeChildrenProfileRecorder struct {
	mu    sync.Mutex
	auths []string
}

func (r *rehomeChildrenProfileRecorder) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auths...)
}

func newRehomeChildrenProfileServer(t *testing.T) *rehomeChildrenProfileRecorder {
	t.Helper()
	rec := &rehomeChildrenProfileRecorder{}
	rehomeChildrenTestLogin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		switch {
		case r.URL.Path == "/rest/api/3/issue/PROJ-100" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-100","fields":{"summary":"Old parent","labels":[],"issuetype":{"id":"5","name":"Task"},"project":{"id":"10000","key":"PROJ"},"status":{"id":"1","name":"Open"}}}`))
		case r.URL.Path == "/rest/api/3/issue/PROJ-200" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"2","key":"PROJ-200","fields":{"summary":"New parent","labels":[],"issuetype":{"id":"5","name":"Task"},"project":{"id":"10000","key":"PROJ"},"status":{"id":"1","name":"Open"}}}`))
		case r.URL.Path == "/rest/api/3/search/jql":
			_, _ = w.Write([]byte(`{"issues":[{"id":"3","key":"PROJ-101","fields":{"summary":"Child","labels":[],"issuetype":{"id":"5","name":"Task"},"project":{"id":"10000","key":"PROJ"},"status":{"id":"1","name":"Open"}}}]}`))
		case r.URL.Path == "/rest/api/3/issue/PROJ-101" && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	original := http.DefaultTransport
	http.DefaultTransport = createServerTransport{srv: srv, next: original}
	t.Cleanup(func() { http.DefaultTransport = original })
	return rec
}

func runRehomeChildrenCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runRehomeChildren(args) })
	})
	return stdout, stderr, err
}

func TestRunRehomeChildren_Profile_UsesProfileCredentialsOnly(t *testing.T) {
	rec := newRehomeChildrenProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

	stdout, stderr, err := runRehomeChildrenCapture(t, "PROJ-100", "--to", "PROJ-200", "--profile", "work")
	if err != nil {
		t.Fatalf("runRehomeChildren: %v (stderr %q)", err, stderr)
	}
	if stdout == "" || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want non-empty stdout and empty stderr", stdout, stderr)
	}
	auths := rec.requests()
	if len(auths) == 0 {
		t.Fatal("no requests recorded")
	}
	want := basicAuth("work@example.com", "work-token")
	for _, a := range auths {
		if a != want {
			t.Errorf("Authorization headers = %v, want every request to use the work profile's credentials %q", auths, want)
		}
	}
}

func TestRunRehomeChildren_Profile_FlagOrderIndependent(t *testing.T) {
	for _, args := range [][]string{
		{"--profile", "work", "PROJ-100", "--to", "PROJ-200"},
		{"--profile=work", "PROJ-100", "--to", "PROJ-200"},
		{"PROJ-100", "--to", "PROJ-200", "--profile", "work"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			rec := newRehomeChildrenProfileServer(t)
			saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

			stdout, stderr, err := runRehomeChildrenCapture(t, args...)
			if err != nil || stdout == "" || stderr != "" {
				t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
			}
			want := basicAuth("work@example.com", "work-token")
			for _, a := range rec.requests() {
				if a != want {
					t.Errorf("Authorization headers = %v, want only the work profile's", rec.requests())
				}
			}
		})
	}
}

func TestRunRehomeChildren_Profile_IsolatedFromLoggedInDefault(t *testing.T) {
	rec := newRehomeChildrenProfileServer(t)

	stdout, stderr, err := runRehomeChildrenCapture(t, "PROJ-100", "--to", "PROJ-200", "--profile", "empty")
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

func TestRunRehomeChildren_NoProfile_Unchanged(t *testing.T) {
	t.Run("uses default credentials", func(t *testing.T) {
		rec := newRehomeChildrenProfileServer(t)
		saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

		stdout, stderr, err := runRehomeChildrenCapture(t, "PROJ-100", "--to", "PROJ-200")
		if err != nil || stdout == "" || stderr != "" {
			t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
		}
		want := basicAuth("test@example.com", "test-token")
		for _, a := range rec.requests() {
			if a != want {
				t.Errorf("Authorization headers = %v, want only the default profile's", rec.requests())
			}
		}
	})
	t.Run("not logged in", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		stdout, stderr, err := runRehomeChildrenCapture(t, "PROJ-100", "--to", "PROJ-200")
		if err == nil || stdout != "" {
			t.Fatalf("err %v stdout %q, want a failure with empty stdout", err, stdout)
		}
		if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

func TestRunRehomeChildren_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
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
			rec := newRehomeChildrenProfileServer(t)
			before, _ := os.ReadDir(xdg)

			args := append([]string{"PROJ-100", "--to", "PROJ-200"}, tc.args...)
			stdout, stderr, err := runRehomeChildrenCapture(t, args...)

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

func TestRunRehomeChildren_Profile_CredentialLoadFailureNamesProfileLogin(t *testing.T) {
	rec := newRehomeChildrenProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")
	path, err := auth.ConfigPath("work")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runRehomeChildrenCapture(t, "PROJ-100", "--to", "PROJ-200", "--profile", "work")
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

func TestRehomeChildren_Profile_ProcessExitCodeAndHelp(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "rehome-children", "PROJ-100", "--to", "PROJ-200", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "rehome-children", "PROJ-100", "--help")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("stderr=%q exit=%d, want exit 0 and empty stderr", stderr, exitCode)
	}
	if !strings.Contains(stdout, "usage: jirahere rehome-children") || !strings.Contains(stdout, "--profile profile") || !strings.Contains(stdout, profileFlagUsage[:20]) {
		t.Errorf("stdout = %q, want the canonical --profile listing", stdout)
	}
}
