package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

type createProfileRecorder struct {
	mu      sync.Mutex
	auths   []string
	labels  []string
	project string
	posted  bool
}

func (r *createProfileRecorder) requests() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.auths)
}

func newCreateProfileServer(t *testing.T) *createProfileRecorder {
	t.Helper()
	rec := &createProfileRecorder{}
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"}]},{"id":"10001","key":"WORK","issuetypes":[{"id":"6","name":"Story"}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Fields struct {
					Labels  []string `json:"labels"`
					Project struct {
						ID string `json:"id"`
					} `json:"project"`
				} `json:"fields"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("unmarshal request body: %v", err)
			}
			rec.labels = req.Fields.Labels
			rec.project = req.Fields.Project.ID
			rec.posted = true
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return rec
}

func basicAuth(email, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
}

func saveCreateProfileLogin(t *testing.T, prof, email, token string) {
	t.Helper()
	if err := auth.Save(prof, &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: email, Token: token},
	}); err != nil {
		t.Fatalf("auth.Save(%q): %v", prof, err)
	}
}

func runCreateCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCreate(args) })
	})
	return stdout, stderr, err
}

func TestRunCreate_Profile_UsesProfileCredentialsAndSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newCreateProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","issue_type":"Task","current_quarter":"DEFAULT-Q"}}`)
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORK","issue_type":"Story","current_quarter":"FY26-Q3"}}`)

	stdout, stderr, err := runCreateCapture(t, "--summary", "S", "--profile", "work")
	if err != nil {
		t.Fatalf("runCreate: %v (stderr %q)", err, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if !strings.HasPrefix(stdout, "Created a new Story in WORK.\n") {
		t.Errorf("stdout = %q, want the profile's project and issue type", stdout)
	}
	if !strings.Contains(stdout, "Labels: FY26-Q3. FY26-Q3 was auto-added for the current quarter.\n") {
		t.Errorf("stdout = %q, want the profile's quarter label", stdout)
	}
	if strings.Contains(stdout, "DEFAULT-Q") {
		t.Errorf("stdout = %q leaked the default profile's quarter", stdout)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := basicAuth("work@example.com", "work-token")
	if len(rec.auths) == 0 {
		t.Fatal("no request reached the server")
	}
	for _, got := range rec.auths {
		if got != want {
			t.Errorf("Authorization = %q, want the work profile's credentials", got)
		}
	}
	if !rec.posted || rec.project != "10001" || len(rec.labels) != 1 || rec.labels[0] != "FY26-Q3" {
		t.Errorf("posted=%v project=%q labels=%v, want WORK (10001) with FY26-Q3", rec.posted, rec.project, rec.labels)
	}
}

func TestRunCreate_Profile_FlagOverridesProfileSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	newCreateProfileServer(t)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORK","issue_type":"Story"}}`)

	stdout, stderr, err := runCreateCapture(t, "--summary", "S", "--profile", "work", "--project", "PROJ", "--type", "Task")
	if err != nil {
		t.Fatalf("runCreate: %v (stderr %q)", err, stderr)
	}
	if !strings.HasPrefix(stdout, "Created a new Task in PROJ.\n") {
		t.Errorf("stdout = %q, want flag values to win", stdout)
	}
}

func TestRunCreate_Profile_IsolatedFromFullyConfiguredDefault(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		rec := newCreateProfileServer(t)
		writeSettingsFile(t, `{"defaults":{"project":"PROJ","issue_type":"Task","current_quarter":"FY26-Q1"}}`)

		writeProfileSettings(t, "empty", `{"defaults":{"project":"PROJ","issue_type":"Task"}}`)

		stdout, stderr, err := runCreateCapture(t, "--summary", "S", "--profile", "empty")
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
		if n := rec.requests(); n != 0 {
			t.Errorf("%d request(s) reached Jira; the default profile's credentials were used", n)
		}
	})

	t.Run("no settings", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		rec := newCreateProfileServer(t)
		saveCreateProfileLogin(t, "empty", "e@example.com", "e-token")
		writeSettingsFile(t, `{"defaults":{"project":"PROJ","issue_type":"Task","current_quarter":"FY26-Q1"}}`)

		stdout, stderr, err := runCreateCapture(t, "--summary", "S", "--profile", "empty")
		var usage *errUsage
		if !errors.As(err, &usage) {
			t.Fatalf("err = %v (%T), want *errUsage", err, err)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty", stdout)
		}
		want := "jirahere: no project given and no default project configured; pass --project or set defaults.project (run \"jirahere configure set --profile empty --project <key>\")\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
		if n := rec.requests(); n != 0 {
			t.Errorf("%d request(s) reached Jira", n)
		}
	})
}

func TestRunCreate_Profile_MissingIssueTypeRemediation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := newCreateProfileServer(t)
	saveCreateProfileLogin(t, "work", "w@example.com", "w-token")
	writeSettingsFile(t, `{"defaults":{"issue_type":"Task"}}`)

	_, stderr, err := runCreateCapture(t, "--summary", "S", "--profile", "work", "--project", "PROJ")
	var usage *errUsage
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v (%T), want *errUsage", err, err)
	}
	want := "jirahere: no issue type given and no default issue type configured; pass --type or set defaults.issue_type (run \"jirahere configure set --profile work --type <name>\")\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if n := rec.requests(); n != 0 {
		t.Errorf("%d request(s) reached Jira", n)
	}
}

func TestRunCreate_NoProfile_MessagesUnchanged(t *testing.T) {
	t.Run("missing project", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		_, stderr, err := runCreateCapture(t, "--summary", "S")
		if err == nil {
			t.Fatal("want an error")
		}
		want := "jirahere: no project given and no default project configured; pass --project or set defaults.project\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
	t.Run("missing issue type", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		_, stderr, err := runCreateCapture(t, "--summary", "S", "--project", "PROJ")
		if err == nil {
			t.Fatal("want an error")
		}
		want := "jirahere: no issue type given and no default issue type configured; pass --type or set defaults.issue_type\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
	t.Run("not logged in", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		_, stderr, err := runCreateCapture(t, "--summary", "S", "--project", "PROJ", "--type", "Task")
		if err == nil {
			t.Fatal("want an error")
		}
		want := "jirahere: not logged in. Run \"jirahere login\" first.\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

func TestRunCreate_Profile_CredentialLoadFailureNamesProfileLogin(t *testing.T) {
	for _, tc := range []struct{ prof, want string }{
		{"work", "jirahere: could not load Jira credentials. Run \"jirahere login --profile work\" again.\n"},
		{"", "jirahere: could not load Jira credentials. Run \"jirahere login\" again.\n"},
	} {
		t.Run("profile="+tc.prof, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			rec := newCreateProfileServer(t)
			path, err := auth.ConfigPath(tc.prof)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--summary", "S", "--project", "PROJ", "--type", "Task"}
			if tc.prof != "" {
				args = append(args, "--profile", tc.prof)
			}
			_, stderr, err := runCreateCapture(t, args...)
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %v (%T), want *errSilent", err, err)
			}
			if stderr != tc.want {
				t.Errorf("stderr = %q, want %q", stderr, tc.want)
			}
			if strings.Contains(stderr, "not json") || strings.Contains(stderr, "config.json") {
				t.Errorf("stderr = %q leaked config content or path", stderr)
			}
			if n := rec.requests(); n != 0 {
				t.Errorf("%d request(s) reached Jira", n)
			}
		})
	}
}

func TestRefreshCredentialsFailureFor_ProfileRemediation(t *testing.T) {
	withStatus := &auth.RefreshStatusError{StatusCode: 401}
	cases := []struct {
		name string
		err  error
		prof string
		want string
	}{
		{"status under profile", withStatus, "work", "jirahere: could not refresh Jira credentials (401). Run \"jirahere login --profile work\" again."},
		{"no status under profile", errors.New("boom"), "work", "jirahere: could not refresh Jira credentials. Run \"jirahere login --profile work\" again."},
		{"status default", withStatus, "", "jirahere: could not refresh Jira credentials (401). Run \"jirahere login\" again."},
		{"no status default", errors.New("boom"), "", "jirahere: could not refresh Jira credentials. Run \"jirahere login\" again."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got error
			stderr := captureStderr(t, func() { got = refreshCredentialsFailureFor(tc.err, tc.prof) })
			if got == nil || got.Error() != tc.want {
				t.Errorf("error = %v, want %q", got, tc.want)
			}
			if stderr != tc.want+"\n" {
				t.Errorf("stderr = %q, want %q", stderr, tc.want+"\n")
			}
			if strings.Contains(stderr, "boom") {
				t.Errorf("stderr = %q leaked the underlying error", stderr)
			}
		})
	}

	var a, b error
	captureStderr(t, func() { a = refreshCredentialsFailure(withStatus) })
	captureStderr(t, func() { b = refreshCredentialsFailureFor(withStatus, "") })
	if a.Error() != b.Error() {
		t.Errorf("default-profile text differs: %q vs %q", a, b)
	}
}

func TestRunCreate_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", []string{"--profile", ""}},
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
			rec := newCreateProfileServer(t)

			before, _ := os.ReadDir(xdg)

			args := append([]string{"--summary", "S", "--project", "PROJ", "--type", "Task"}, tc.args...)
			stdout, stderr, err := runCreateCapture(t, args...)

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
			if n := rec.requests(); n != 0 {
				t.Errorf("%d request(s) reached Jira before the usage error", n)
			}
			after, _ := os.ReadDir(xdg)
			if len(after) != len(before) {
				t.Errorf("config dir changed (%d -> %d entries); create did I/O before validating --profile", len(before), len(after))
			}
		})
	}
}

func TestCreate_Profile_ProcessExitCodeAndHelp(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "create", "--summary", "S", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "create", "--help")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("--help: stderr=%q exit=%d, want exit 0 and empty stderr", stderr, exitCode)
	}
	if !strings.Contains(stdout, "--profile profile") || !strings.Contains(stdout, profileFlagUsage[:20]) {
		t.Errorf("--help stdout = %q, want the canonical --profile listing", stdout)
	}
}
