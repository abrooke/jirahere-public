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

type statusSetProfileRecorder struct {
	mu    sync.Mutex
	auths []string
}

func (r *statusSetProfileRecorder) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auths...)
}

func newStatusSetProfileServer(t *testing.T) *statusSetProfileRecorder {
	t.Helper()
	rec := &statusSetProfileRecorder{}
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/transitions"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"transitions":[{"id":"21","to":{"id":"4","name":"Done"}}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transitions"):
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return rec
}

func runStatusSetCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runStatusSet(args) })
	})
	return stdout, stderr, err
}

func TestRunStatusSet_Profile_UsesProfileCredentialsOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newStatusSetProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

	stdout, stderr, err := runStatusSetCapture(t, "PROJ-1", "--status", "Done", "--profile", "work")
	if err != nil {
		t.Fatalf("runStatusSet: %v (stderr %q)", err, stderr)
	}
	if want := "Status set: PROJ-1 -> Done\n"; stdout != want || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want %q/empty", stdout, stderr, want)
	}
	auths := rec.requests()
	if len(auths) != 2 {
		t.Fatalf("%d request(s), want exactly the transitions GET and POST", len(auths))
	}
	want := basicAuth("work@example.com", "work-token")
	for _, a := range auths {
		if a != want {
			t.Errorf("Authorization = %q, want the work profile's credentials %q", a, want)
		}
	}
}

func TestRunStatusSet_Profile_FlagOrderIndependent(t *testing.T) {
	for _, args := range [][]string{
		{"--profile", "work", "PROJ-1", "--status", "Done"},
		{"--profile=work", "PROJ-1", "--status", "Done"},
		{"PROJ-1", "--status", "Done", "--profile", "work"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			rec := newStatusSetProfileServer(t)
			saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

			stdout, stderr, err := runStatusSetCapture(t, args...)
			if err != nil || stdout == "" || stderr != "" {
				t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
			}
			for _, a := range rec.requests() {
				if a != basicAuth("work@example.com", "work-token") {
					t.Errorf("Authorization headers = %v, want only the work profile's", rec.requests())
					break
				}
			}
		})
	}
}

func TestRunStatusSet_Profile_IsolatedFromLoggedInDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newStatusSetProfileServer(t)

	stdout, stderr, err := runStatusSetCapture(t, "PROJ-1", "--status", "Done", "--profile", "empty")
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

func TestRunStatusSet_NoProfile_Unchanged(t *testing.T) {
	t.Run("uses default credentials", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		rec := newStatusSetProfileServer(t)
		saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

		stdout, stderr, err := runStatusSetCapture(t, "PROJ-1", "--status", "Done")
		if err != nil || stdout == "" || stderr != "" {
			t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
		}
		for _, a := range rec.requests() {
			if a != basicAuth("test@example.com", "test-token") {
				t.Errorf("Authorization headers = %v, want only the default profile's", rec.requests())
				break
			}
		}
	})
	t.Run("not logged in", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		stdout, stderr, err := runStatusSetCapture(t, "PROJ-1", "--status", "Done")
		if err == nil || stdout != "" {
			t.Fatalf("err %v stdout %q, want a failure with empty stdout", err, stdout)
		}
		if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

func TestRunStatusSet_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
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
			rec := newStatusSetProfileServer(t)
			before, _ := os.ReadDir(xdg)

			args := append([]string{"PROJ-1", "--status", "Done"}, tc.args...)
			stdout, stderr, err := runStatusSetCapture(t, args...)

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

func TestRunStatusSet_Profile_CredentialLoadFailureNamesProfileLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newStatusSetProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")
	path, err := auth.ConfigPath("work")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runStatusSetCapture(t, "PROJ-1", "--status", "Done", "--profile", "work")
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

func TestStatusSet_Profile_ProcessExitCode(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "status", "set", "PROJ-1", "--status", "Done", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}
}
