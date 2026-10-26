package main

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type descriptionSetProfileRecorder struct {
	mu    sync.Mutex
	auths []string
}

func (r *descriptionSetProfileRecorder) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auths...)
}

func newDescriptionSetProfileServer(t *testing.T) *descriptionSetProfileRecorder {
	t.Helper()
	rec := &descriptionSetProfileRecorder{}
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		if r.URL.Path != "/rest/api/3/issue/PROJ-1" || r.Method != http.MethodPut {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return rec
}

func runDescriptionSetCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runDescriptionSet(args) })
	})
	return stdout, stderr, err
}

func TestRunDescriptionSet_Profile_UsesProfileCredentialsOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newDescriptionSetProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

	stdout, stderr, err := runDescriptionSetCapture(t, "PROJ-1", "--body", "hi", "--profile", "work")
	if err != nil {
		t.Fatalf("runDescriptionSet: %v (stderr %q)", err, stderr)
	}
	if stdout != "Description set: PROJ-1\n" || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want success line/empty", stdout, stderr)
	}
	auths := rec.requests()
	if len(auths) != 1 {
		t.Fatalf("%d request(s), want exactly the one PUT", len(auths))
	}
	if want := basicAuth("work@example.com", "work-token"); auths[0] != want {
		t.Errorf("Authorization = %q, want the work profile's credentials %q", auths[0], want)
	}
}

func TestRunDescriptionSet_Profile_FlagOrderIndependent(t *testing.T) {
	for _, args := range [][]string{
		{"PROJ-1", "--body", "hi", "--profile", "work"},
		{"PROJ-1", "--profile", "work", "--body", "hi"},
		{"--profile", "work", "--body", "hi", "PROJ-1"},
		{"--body", "hi", "--profile", "work", "PROJ-1"},
		{"--profile", "work", "PROJ-1", "--body", "hi"},
		{"--body", "hi", "PROJ-1", "--profile", "work"},
		{"--profile=work", "--body=hi", "PROJ-1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			rec := newDescriptionSetProfileServer(t)
			saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

			stdout, stderr, err := runDescriptionSetCapture(t, args...)
			if err != nil || stdout != "Description set: PROJ-1\n" || stderr != "" {
				t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
			}
			if auths := rec.requests(); len(auths) != 1 || auths[0] != basicAuth("work@example.com", "work-token") {
				t.Errorf("Authorization headers = %v, want only the work profile's", auths)
			}
		})
	}
}

func TestRunDescriptionSet_NoProfile_UsesDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newDescriptionSetProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

	stdout, stderr, err := runDescriptionSetCapture(t, "PROJ-1", "--body", "hi")
	if err != nil || stdout != "Description set: PROJ-1\n" || stderr != "" {
		t.Fatalf("err %v stdout %q stderr %q, want success", err, stdout, stderr)
	}
	if auths := rec.requests(); len(auths) != 1 || auths[0] != basicAuth("test@example.com", "test-token") {
		t.Errorf("Authorization headers = %v, want only the default profile's", auths)
	}
}

func TestRunDescriptionSet_Profile_Unknown_FailsBeforeWrite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newDescriptionSetProfileServer(t)

	stdout, stderr, err := runDescriptionSetCapture(t, "PROJ-1", "--body", "hi", "--profile", "nope")
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v (%T), want *errSilent", err, err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	want := "jirahere: not logged in (profile \"nope\"). Run \"jirahere login --profile nope\" first.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("%d request(s) reached Jira; want zero PUTs", n)
	}
}

func TestRunDescriptionSet_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
	for _, bad := range [][]string{
		{"--profile", ""},
		{"--profile="},
		{"--profile", "../evil"},
		{"--profile", "a/b"},
	} {
		t.Run(strings.Join(bad, " "), func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			rec := newDescriptionSetProfileServer(t)

			args := append([]string{"PROJ-1", "--body", "secret-body"}, bad...)
			stdout, stderr, err := runDescriptionSetCapture(t, args...)
			var usage *errUsage
			if !errors.As(err, &usage) {
				t.Fatalf("err = %v (%T), want *errUsage (exit 2)", err, err)
			}
			if stdout != "" || !strings.Contains(stderr, "--profile") || strings.Contains(stderr, "secret-body") || strings.Contains(stderr, "evil") {
				t.Errorf("stdout/stderr = %q/%q, want body-free usage error naming --profile", stdout, stderr)
			}
			if n := len(rec.requests()); n != 0 {
				t.Errorf("%d request(s) reached Jira", n)
			}
		})
	}
}
