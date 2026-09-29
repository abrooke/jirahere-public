package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

func writeSettingsFile(t *testing.T, raw string) {
	t.Helper()
	dir, err := auth.ConfigDir("")
	if err != nil {
		t.Fatalf("auth.ConfigDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

const createUsageLine = "usage: jirahere create --summary <text> [--project <key>] [--type <name>] [--description <text>] [--parent <key>] [--label <label>]... [--suppress-auto-quarter] [--profile <name>]"

func TestRunCreate_PreflightErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "unknown flag",
			args:    []string{"--summary", "Widget rollout", "--nope"},
			wantMsg: "jirahere: flag provided but not defined: --nope",
		},
		{
			name:    "surplus positional after flags",
			args:    []string{"--summary", "x", "junk"},
			wantMsg: createUsageLine,
		},
		{
			name:    "surplus positional before --summary wins over missing --summary",
			args:    []string{"junk", "--summary", "x"},
			wantMsg: createUsageLine,
		},
		{
			name:    "explicit -- terminator then a positional",
			args:    []string{"--summary", "x", "--", "junk"},
			wantMsg: createUsageLine,
		},
		{
			name:    "missing --summary",
			args:    []string{"--project", "PROJ"},
			wantMsg: "jirahere: --summary is required and must not be empty.",
		},
		{
			name:    "--summary checked before --label",
			args:    []string{"--label", "bad label"},
			wantMsg: "jirahere: --summary is required and must not be empty.",
		},
		{
			name:    "empty --summary",
			args:    []string{"--summary", ""},
			wantMsg: "jirahere: --summary is required and must not be empty.",
		},
		{
			name:    "empty --label",
			args:    []string{"--summary", "Widget rollout", "--label", ""},
			wantMsg: "jirahere: --label must not be empty.",
		},
		{
			name:    "whitespace in --label",
			args:    []string{"--summary", "Widget rollout", "--label", "team widgets"},
			wantMsg: `jirahere: "team widgets" is not a valid Jira label (labels can't contain whitespace).`,
		},
		{
			name:    "tab in a later --label",
			args:    []string{"--summary", "Widget rollout", "--label", "fy26-q3", "--label", "team\twidgets"},
			wantMsg: `jirahere: "team\twidgets" is not a valid Jira label (labels can't contain whitespace).`,
		},
		{
			name:    "--description present with an empty value",
			args:    []string{"--summary", "Widget rollout", "--description", ""},
			wantMsg: "jirahere: --description was given but is empty; an empty description is not allowed.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runCreate(tt.args) })

			if err == nil {
				t.Fatalf("runCreate(%v) = nil, want an error", tt.args)
			}
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %T, want *errSilent (body-free, already printed to stderr)", err)
			}
			if got := strings.TrimRight(stderr, "\n"); got != tt.wantMsg {
				t.Errorf("stderr = %q, want %q", got, tt.wantMsg)
			}
			if strings.Contains(stderr, "Created a new") {
				t.Errorf("stderr = %q, a pre-flight failure must abort before the create call", stderr)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want exactly one line", stderr)
			}
		})
	}
}

func TestMapCreateError_ResolveProjectVsValidateParent(t *testing.T) {
	status := &jira.StatusError{StatusCode: 500, Body: "untrusted response body"}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "resolve project",
			err: &command.ErrCreateResolveProject{
				Subject:   `project "PROJ"`,
				Operation: command.CreatePreflightResolveProject,
				Key:       "PROJ",
				Err:       status,
			},
			want: `jirahere: create could not resolve project "PROJ": Jira returned an unexpected response (500).`,
		},
		{
			name: "validate parent",
			err: &command.ErrCreateValidateParent{
				Subject: `parent "PROJ-9"`,
				Key:     "PROJ-9",
				Err:     status,
			},
			want: `jirahere: create could not validate parent "PROJ-9": Jira returned an unexpected response (500).`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mapCreateError(tc.err)
			if err == nil || err.Error() != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMapCreateError_SubtaskUndetermined(t *testing.T) {
	err := mapCreateError(&command.ErrCreateSubtaskUndetermined{IssueTypeName: "Epic", ProjectKey: "PROJ"})
	want := `jirahere: create cannot tell whether issue type "Epic" in project "PROJ" is a subtask type (Jira did not report it); pass --parent only for a type known to accept it, or omit --parent to create without one.`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestRunCreate_ValidInvocationCreatesAndReports(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"summary, project, and type", []string{"--summary", "Widget rollout Q3'26", "--project", "PROJ", "--type", "Task"}},
		{
			"all single-valued flags",
			[]string{
				"--summary", "Widget rollout", "--project", "PROJ", "--type", "Epic",
				"--description", "some plain text", "--parent", "PROJ-1",
			},
		},
		{
			"repeated --label accepted",
			[]string{"--summary", "Widget rollout", "--project", "PROJ", "--type", "Task", "--label", "fy26-q3", "--label", "team-widgets"},
		},
		{

			"trailing -- with no positional",
			[]string{"--summary", "Widget rollout", "--project", "PROJ", "--type", "Task", "--"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			var posts int
			createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rest/api/3/issue/createmeta":
					_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task","subtask":false},{"id":"6","name":"Epic","subtask":false}]}]}`))
				case r.URL.Path == "/rest/api/3/issue/PROJ-1" && r.Method == http.MethodGet:
					_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-1","fields":{}}`))
				case r.URL.Path == "/rest/api/3/issue/PROJ-101" && r.Method == http.MethodPut:
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
					posts++
					_, _ = w.Write([]byte(`{"id":"10010","key":"PROJ-101"}`))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})

			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runCreate(tt.args) })
			})

			if err != nil {
				t.Fatalf("runCreate(%v) = %v, want nil (success)", tt.args, err)
			}
			if posts != 1 {
				t.Errorf("CreateIssue POSTs = %d, want exactly 1", posts)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty on success", stderr)
			}
			if !hasOwnLine(stdout, "PROJ-101") {
				t.Errorf("stdout = %q, want the new key %q alone on its own line", stdout, "PROJ-101")
			}
			if !strings.Contains(stdout, "Created a new ") {
				t.Errorf("stdout = %q, want it to name the resolved project and type", stdout)
			}
		})
	}
}

func hasOwnLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

const createIssueTypeErrMsg = "jirahere: no issue type given and no default issue type configured; pass --type or set defaults.issue_type"

func TestResolveIssueType(t *testing.T) {
	tests := []struct {
		name          string
		flagIssueType string
		s             *settings.Settings
		want          string
		wantErr       bool
	}{
		{"flag given, no settings", "Task", nil, "Task", false},
		{"flag given, wins over settings default", "Bug", &settings.Settings{Defaults: settings.Defaults{IssueType: "Task"}}, "Bug", false},
		{"flag omitted, settings default used", "", &settings.Settings{Defaults: settings.Defaults{IssueType: "Task"}}, "Task", false},
		{"flag omitted, nil settings", "", nil, "", true},
		{"flag omitted, settings with no defaults.issue_type", "", &settings.Settings{}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			var err error
			stderr := captureStderr(t, func() { got, err = resolveIssueType(tt.flagIssueType, tt.s, "") })

			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveIssueType(%q, %+v) = nil error, want an error", tt.flagIssueType, tt.s)
				}
				var silent *errSilent
				if !errors.As(err, &silent) {
					t.Fatalf("err = %T, want *errSilent (body-free)", err)
				}
				if got := strings.TrimRight(stderr, "\n"); got != createIssueTypeErrMsg {
					t.Errorf("stderr = %q, want %q", got, createIssueTypeErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveIssueType(%q, %+v): %v", tt.flagIssueType, tt.s, err)
			}
			if got != tt.want {
				t.Errorf("resolveIssueType(%q, %+v) = %q, want %q", tt.flagIssueType, tt.s, got, tt.want)
			}
		})
	}
}

func TestRunCreate_IssueTypeResolution(t *testing.T) {
	t.Run("--type given wins over defaults.issue_type", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		createTestLogin(t, nil)
		writeSettingsFile(t, `{"defaults":{"issue_type":"Task"}}`)

		var err error
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() {
				err = runCreate([]string{"--summary", "Widget rollout", "--project", "PROJ", "--type", "Bug"})
			})
		})
		if err != nil {
			t.Fatalf("runCreate = %v, want nil (success)", err)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "Bug") || !hasOwnLine(stdout, "PROJ-101") {
			t.Errorf("stdout = %q, want the resolved type Bug and the key on its own line", stdout)
		}
	})

	t.Run("--type omitted uses defaults.issue_type", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		createTestLogin(t, nil)
		writeSettingsFile(t, `{"defaults":{"issue_type":"Task"}}`)

		var err error
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() {
				err = runCreate([]string{"--summary", "Widget rollout", "--project", "PROJ"})
			})
		})
		if err != nil {
			t.Fatalf("runCreate = %v, want nil (success)", err)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "Task") || !hasOwnLine(stdout, "PROJ-101") {
			t.Errorf("stdout = %q, want the resolved type Task and the key on its own line", stdout)
		}
	})

	t.Run("both absent fails before later create work", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		var err error
		stderr := captureStderr(t, func() {
			err = runCreate([]string{"--summary", "Widget rollout", "--project", "PROJ"})
		})
		var silent *errSilent
		if !errors.As(err, &silent) {
			t.Fatalf("err = %T, want *errSilent (body-free)", err)
		}
		if got := strings.TrimRight(stderr, "\n"); got != createIssueTypeErrMsg {
			t.Errorf("stderr = %q, want %q", got, createIssueTypeErrMsg)
		}
		if strings.Count(stderr, "\n") != 1 {
			t.Errorf("stderr = %q, want exactly one line", stderr)
		}
	})
}

const createProjectErrMsg = "jirahere: no project given and no default project configured; pass --project or set defaults.project"

func TestResolveProject(t *testing.T) {
	tests := []struct {
		name        string
		flagProject string
		s           *settings.Settings
		want        string
		wantErr     bool
	}{
		{"flag given, no settings", "PROJ", nil, "PROJ", false},
		{"flag given, wins over settings default", "FLAG", &settings.Settings{Defaults: settings.Defaults{Project: "CFG"}}, "FLAG", false},
		{"flag omitted, settings default used", "", &settings.Settings{Defaults: settings.Defaults{Project: "CFG"}}, "CFG", false},
		{"flag omitted, nil settings", "", nil, "", true},
		{"flag omitted, settings with no defaults.project", "", &settings.Settings{}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			var err error
			stderr := captureStderr(t, func() { got, err = resolveProject(tt.flagProject, tt.s, "") })

			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveProject(%q, %+v) = nil error, want an error", tt.flagProject, tt.s)
				}
				var silent *errSilent
				if !errors.As(err, &silent) {
					t.Fatalf("err = %T, want *errSilent (body-free)", err)
				}
				if got := strings.TrimRight(stderr, "\n"); got != createProjectErrMsg {
					t.Errorf("stderr = %q, want %q", got, createProjectErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveProject(%q, %+v): %v", tt.flagProject, tt.s, err)
			}
			if got != tt.want {
				t.Errorf("resolveProject(%q, %+v) = %q, want %q", tt.flagProject, tt.s, got, tt.want)
			}
		})
	}
}

func TestRunCreate_ProjectResolution(t *testing.T) {
	t.Run("--project given, no settings file", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		createTestLogin(t, nil)

		var err error
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() {
				err = runCreate([]string{"--summary", "Widget rollout", "--project", "PROJ", "--type", "Task"})
			})
		})

		if err != nil {
			t.Fatalf("runCreate = %v, want nil (success)", err)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "in PROJ.") || !hasOwnLine(stdout, "PROJ-101") {
			t.Errorf("stdout = %q, want the resolved project PROJ and the key on its own line", stdout)
		}
	})

	t.Run("--project omitted, defaults.project configured", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		createTestLogin(t, nil)
		writeSettingsFile(t, `{"defaults":{"project":"PROJ"}}`)

		var err error
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() {
				err = runCreate([]string{"--summary", "Widget rollout", "--type", "Task"})
			})
		})

		if err != nil {
			t.Fatalf("runCreate = %v, want nil (success)", err)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "in PROJ.") || !hasOwnLine(stdout, "PROJ-101") {
			t.Errorf("stdout = %q, want the resolved project PROJ and the key on its own line", stdout)
		}
	})

	t.Run("--project omitted, settings without defaults.project", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if err := auth.Save("", &auth.Config{
			Provider: auth.ProviderAPIToken,
			Site:     "acme.atlassian.net",
			APIToken: &auth.APITokenConfig{Email: "test@example.com", Token: "test-token"},
		}); err != nil {
			t.Fatalf("auth.Save: %v", err)
		}
		writeSettingsFile(t, `{"defaults":{}}`)

		var err error
		stderr := captureStderr(t, func() {
			err = runCreate([]string{"--summary", "Widget rollout"})
		})

		var silent *errSilent
		if !errors.As(err, &silent) {
			t.Fatalf("err = %T, want *errSilent (body-free)", err)
		}
		if got := strings.TrimRight(stderr, "\n"); got != createProjectErrMsg {
			t.Errorf("stderr = %q, want %q", got, createProjectErrMsg)
		}
		if strings.Count(stderr, "\n") != 1 {
			t.Errorf("stderr = %q, want exactly one line", stderr)
		}
	})

	t.Run("--project omitted, no settings file at all", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		var err error
		stderr := captureStderr(t, func() {
			err = runCreate([]string{"--summary", "Widget rollout"})
		})

		var silent *errSilent
		if !errors.As(err, &silent) {
			t.Fatalf("err = %T, want *errSilent (body-free)", err)
		}
		if got := strings.TrimRight(stderr, "\n"); got != createProjectErrMsg {
			t.Errorf("stderr = %q, want %q", got, createProjectErrMsg)
		}
	})
}

func TestRunCreate_Help(t *testing.T) {
	for _, flagForm := range []string{"-h", "--help"} {
		t.Run(flagForm, func(t *testing.T) {
			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() { err = runCreate([]string{flagForm}) })
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}

			for _, want := range []string{"--summary string", "--project string", "--type string", "--description string", "--parent string", "--label value", "--profile profile"} {
				if !strings.Contains(stdoutOut, want) {
					t.Errorf("stdout = %q, want it to contain %q (create's option listing)", stdoutOut, want)
				}
			}
			if stderrOut != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderrOut)
			}
		})
	}
}

func TestCreateLabelListFlag_Accumulates(t *testing.T) {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	var labels labelListFlag
	fs.Var(&labels, "label", "")

	if err := fs.Parse([]string{"--label", "a", "--label", "b", "--label", "c"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual([]string(labels), want) {
		t.Errorf("labels = %v, want %v", []string(labels), want)
	}
}

func TestCreateLabels(t *testing.T) {
	tests := []struct {
		name           string
		userLabels     []string
		currentQuarter string
		want           []string
	}{
		{
			name:           "appends configured quarter after user labels",
			userLabels:     []string{"team-widgets", "priority-high"},
			currentQuarter: "FY26-Q1",
			want:           []string{"team-widgets", "priority-high", "FY26-Q1"},
		},
		{
			name:           "deduplicates user labels and configured quarter",
			userLabels:     []string{"team-widgets", "FY26-Q1", "team-widgets"},
			currentQuarter: "FY26-Q1",
			want:           []string{"team-widgets", "FY26-Q1"},
		},
		{
			name:       "empty configured quarter is a no-op",
			userLabels: []string{"team-widgets", "team-widgets", "priority-high"},
			want:       []string{"team-widgets", "priority-high"},
		},
		{
			name:           "configured value is accepted as-is",
			userLabels:     []string{"team-widgets"},
			currentQuarter: "not-a-quarter",
			want:           []string{"team-widgets", "not-a-quarter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := createLabels(tt.userLabels, tt.currentQuarter); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("createLabels(%v, %q) = %v, want %v", tt.userLabels, tt.currentQuarter, got, tt.want)
			}
		})
	}
}

func TestCreateLabelHasWhitespace(t *testing.T) {
	withWhitespace := []string{"a b", "a\tb", "a\nb", "a\vb", " leading", "trailing "}
	for _, s := range withWhitespace {
		if !labelHasWhitespace(s) {
			t.Errorf("labelHasWhitespace(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"fy26-q3", "team-widgets", "UPPER_case-1", ""} {
		if labelHasWhitespace(s) {
			t.Errorf("labelHasWhitespace(%q) = true, want false", s)
		}
	}
}

func createTestLogin(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: "test@example.com", Token: "test-token"},
	}); err != nil {
		t.Fatalf("auth.Save: %v", err)
	}
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/rest/api/3/issue/createmeta":
				_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"},{"id":"6","name":"Epic"},{"id":"7","name":"Bug"}]}]}`))
			case r.URL.Path == "/rest/api/3/issue/PROJ-1" && r.Method == http.MethodGet:
				_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-1","fields":{}}`))
			case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
				w.WriteHeader(http.StatusNoContent)
			case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
				_, _ = w.Write([]byte(`{"id":"10010","key":"PROJ-101"}`))
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	original := http.DefaultTransport
	http.DefaultTransport = createServerTransport{srv: srv, next: original}
	t.Cleanup(func() { http.DefaultTransport = original })
}

type createServerTransport struct {
	srv  *httptest.Server
	next http.RoundTripper
}

func (rt createServerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(rt.srv.URL)
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme, cloned.URL.Host, cloned.Host = target.Scheme, target.Host, target.Host
	return rt.next.RoundTrip(cloned)
}

func TestRunCreate_RemotePreflightIsOrderedAndBodyFree(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		metadata string
		parent   int
		want     string
		paths    []string
	}{
		{
			name:     "project missing stops before type and parent",
			args:     []string{"--summary", "x", "--project", "MISSING", "--type", "Task", "--parent", "PROJ-1"},
			metadata: `{"projects":[]}`,
			want:     `jirahere: create project "MISSING" is not available for creation.`,
			paths:    []string{"/rest/api/3/issue/createmeta"},
		},
		{
			name:     "type missing lists available types before parent",
			args:     []string{"--summary", "x", "--project", "PROJ", "--type", "Bug", "--parent", "PROJ-1"},
			metadata: `{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"6","name":"Epic"},{"id":"5","name":"Task"}]}]}`,
			want:     `jirahere: create issue type "Bug" is not available for project "PROJ"; available types: Epic, Task.`,
			paths:    []string{"/rest/api/3/issue/createmeta"},
		},
		{
			name:     "parent missing follows resolved project and type",
			args:     []string{"--summary", "x", "--project", "PROJ", "--type", "Task", "--parent", "PROJ-9"},
			metadata: `{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"}]}]}`,
			parent:   http.StatusNotFound,
			want:     `jirahere: create parent "PROJ-9" was not found (404).`,
			paths:    []string{"/rest/api/3/issue/createmeta", "/rest/api/3/issue/PROJ-9"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			var paths []string
			createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path == "/rest/api/3/issue/createmeta" {
					_, _ = w.Write([]byte(tt.metadata))
					return
				}
				w.WriteHeader(tt.parent)
				_, _ = w.Write([]byte("untrusted response body"))
			})
			var err error
			stderr := captureStderr(t, func() { err = runCreate(tt.args) })
			if err == nil {
				t.Fatal("runCreate = nil, want pre-flight failure")
			}
			if got := strings.TrimSpace(stderr); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(paths, tt.paths) {
				t.Errorf("request paths = %v, want %v", paths, tt.paths)
			}
		})
	}
}

func createCapturePost(t *testing.T, key string) *map[string]any {
	t.Helper()
	captured := new(map[string]any)
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("CreateIssue body is not JSON: %v (%s)", err, body)
			}
			*captured = payload.Fields
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return captured
}

func TestRunCreate_DescriptionEncodedAsADF(t *testing.T) {
	t.Run("given: ADF document sent", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		fields := createCapturePost(t, "PROJ-7")

		var err error
		_ = captureStdout(t, func() {
			err = runCreate([]string{
				"--summary", "S", "--project", "PROJ", "--type", "Task",
				"--description", "para one\nstill one\n\n*para* two",
			})
		})
		if err != nil {
			t.Fatalf("runCreate = %v, want nil", err)
		}

		desc, ok := (*fields)["description"]
		if !ok {
			t.Fatalf("CreateIssue fields = %v, want a description key", *fields)
		}
		descJSON, _ := json.Marshal(desc)
		got := string(descJSON)
		for _, want := range []string{
			`"type":"doc"`, `"type":"paragraph"`, `"type":"hardBreak"`,
			`"text":"para one"`, `"text":"still one"`, `"text":"*para* two"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("description ADF = %s, want it to contain %s", got, want)
			}
		}
	})

	t.Run("omitted: no description field sent", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		fields := createCapturePost(t, "PROJ-7")

		var err error
		_ = captureStdout(t, func() {
			err = runCreate([]string{"--summary", "S", "--project", "PROJ", "--type", "Task"})
		})
		if err != nil {
			t.Fatalf("runCreate = %v, want nil", err)
		}
		if _, ok := (*fields)["description"]; ok {
			t.Errorf("CreateIssue fields = %v, want no description key when --description is omitted", *fields)
		}
	})
}

func TestRunCreate_SuccessOutputShape(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	writeSettingsFile(t, `{"defaults":{"current_quarter":"FY26-Q1"}}`)

	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			err = runCreate([]string{"--summary", "Widget rollout", "--project", "PROJ", "--type", "Task", "--label", "team-widgets"})
		})
	})
	if err != nil {
		t.Fatalf("runCreate = %v, want nil", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	want := "Created a new Task in PROJ.\n" +
		"PROJ-123\n" +
		"Labels: team-widgets, FY26-Q1. FY26-Q1 was auto-added for the current quarter.\n" +
		"Done.\n"
	if stdout != want {
		t.Errorf("stdout =\n%q\nwant\n%q", stdout, want)
	}
}

func TestRunCreate_NoAutoQuarterWhenUserPassedIt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	writeSettingsFile(t, `{"defaults":{"current_quarter":"FY26-Q1"}}`)

	var err error
	stdout := captureStdout(t, func() {
		err = runCreate([]string{"--summary", "S", "--project", "PROJ", "--type", "Task", "--label", "FY26-Q1"})
	})
	if err != nil {
		t.Fatalf("runCreate = %v, want nil", err)
	}
	if strings.Contains(stdout, "auto-added") {
		t.Errorf("stdout = %q, want no auto-added note when the user passed the quarter label", stdout)
	}
	if !strings.Contains(stdout, "Labels: FY26-Q1.") {
		t.Errorf("stdout = %q, want a plain labels line", stdout)
	}
}

func TestRunCreate_SuppressAutoQuarter(t *testing.T) {
	tests := []struct {
		name           string
		settingsJSON   string
		args           []string
		wantLabelsLine string
	}{
		{
			name:           "flag given with current_quarter set omits it and shows no auto-added marker",
			settingsJSON:   `{"defaults":{"current_quarter":"FY26-Q1"}}`,
			args:           []string{"--summary", "S", "--project", "PROJ", "--type", "Task", "--label", "team-widgets", "--suppress-auto-quarter"},
			wantLabelsLine: "Labels: team-widgets.\n",
		},
		{
			name:           "flag given with current_quarter unset is an unchanged no-op",
			settingsJSON:   `{}`,
			args:           []string{"--summary", "S", "--project", "PROJ", "--type", "Task", "--label", "team-widgets", "--suppress-auto-quarter"},
			wantLabelsLine: "Labels: team-widgets.\n",
		},
		{
			name:           "flag alongside --label values submits exactly those values, deduplicated",
			settingsJSON:   `{"defaults":{"current_quarter":"FY26-Q1"}}`,
			args:           []string{"--summary", "S", "--project", "PROJ", "--type", "Task", "--label", "a", "--label", "b", "--label", "a", "--suppress-auto-quarter"},
			wantLabelsLine: "Labels: a, b.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			var gotLabels []string
			createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rest/api/3/issue/createmeta":
					_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"}]}]}`))
				case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
					body, _ := io.ReadAll(r.Body)
					var req struct {
						Fields struct {
							Labels []string `json:"labels"`
						} `json:"fields"`
					}
					if err := json.Unmarshal(body, &req); err != nil {
						t.Fatalf("unmarshal request body: %v", err)
					}
					gotLabels = req.Fields.Labels
					_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123"}`))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
			})
			writeSettingsFile(t, tt.settingsJSON)

			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() {
					err = runCreate(tt.args)
				})
			})
			if err != nil {
				t.Fatalf("runCreate = %v, want nil", err)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if !strings.Contains(stdout, tt.wantLabelsLine) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tt.wantLabelsLine)
			}
			if strings.Contains(stdout, "auto-added") {
				t.Errorf("stdout = %q, want no auto-added-quarter marker with --suppress-auto-quarter", stdout)
			}
			if want := strings.Split(strings.TrimSuffix(strings.TrimPrefix(tt.wantLabelsLine, "Labels: "), ".\n"), ", "); !reflect.DeepEqual(gotLabels, want) {
				t.Errorf("submitted labels = %v, want %v", gotLabels, want)
			}
		})
	}
}

func TestRunCreate_CreateCallFailureIsBodyFree(t *testing.T) {
	const secret = "SENSITIVE-Jira-detail-token-xyz"
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task"}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorMessages":["` + secret + `"]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	var err error
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			err = runCreate([]string{"--summary", "S", "--project", "PROJ", "--type", "Task"})
		})
	})

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %T, want *errSilent (body-free)", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (the key line must not be printed on failure)", stdout)
	}
	if strings.Contains(stderr, secret) {
		t.Errorf("stderr = %q, must not echo the Jira response body", stderr)
	}
	want := "jirahere: create failed: Jira returned an unexpected response (400). Nothing was created."
	if got := strings.TrimRight(stderr, "\n"); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestRunCreate_ComposedInputReachesCreateCall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fields := createCapturePost(t, "PROJ-9")
	writeSettingsFile(t, `{"defaults":{"current_quarter":"FY26-Q1"}}`)

	var err error
	_ = captureStdout(t, func() {
		err = runCreate([]string{
			"--summary", "Widget rollout", "--project", "PROJ", "--type", "Task",
			"--label", "team-widgets", "--label", "team-widgets",
		})
	})
	if err != nil {
		t.Fatalf("runCreate = %v, want nil", err)
	}

	if got := (*fields)["summary"]; got != "Widget rollout" {
		t.Errorf("fields[summary] = %#v, want %q", got, "Widget rollout")
	}
	labels, _ := (*fields)["labels"].([]any)
	got := make([]string, len(labels))
	for i, l := range labels {
		got[i], _ = l.(string)
	}
	if !reflect.DeepEqual(got, []string{"team-widgets", "FY26-Q1"}) {
		t.Errorf("fields[labels] = %#v, want the de-duplicated L0022 list [team-widgets FY26-Q1]", got)
	}
}

func TestPrintCreateResult_ParentSet(t *testing.T) {
	res := &command.CreateResult{Key: "PROJ-123", ParentKey: "PROJ-1", ParentAttempted: true}
	out := captureStdout(t, func() {
		printCreateResult("PROJ", "Task", res, []string{"team-widgets"}, "", false)
	})

	want := "Created a new Task in PROJ.\n" +
		"PROJ-123\n" +
		"Labels: team-widgets.\n" +
		"Parent set: PROJ-123 -> PROJ-1.\n" +
		"Done.\n"
	if out != want {
		t.Errorf("output =\n%q\nwant\n%q", out, want)
	}
}

func TestPrintCreateResult_ParentFailsBodyFree(t *testing.T) {
	const secret = "SENSITIVE-set-parent-body-must-not-leak"
	res := &command.CreateResult{
		Key: "PROJ-123", ParentKey: "PROJ-1", ParentAttempted: true,
		ParentErr: &jira.StatusError{StatusCode: 400, Body: secret},
	}
	out := captureStdout(t, func() {
		printCreateResult("PROJ", "Task", res, []string{"team-widgets"}, "", false)
	})

	want := "Created a new Task in PROJ.\n" +
		"PROJ-123\n" +
		"Labels: team-widgets.\n" +
		"Could not set PROJ-1 as PROJ-123's parent: Jira returned an unexpected response (400). Set one with the parent/child link primitive once you have a valid target.\n"
	if out != want {
		t.Errorf("output =\n%q\nwant\n%q", out, want)
	}
	if strings.Contains(out, secret) {
		t.Errorf("output leaked the Jira response body: %q", out)
	}
	if strings.Contains(out, "Done.") {
		t.Errorf("output = %q, want no \"Done.\" line on a partial failure", out)
	}
}

func TestPrintCreateResult_SanitizesParentFailureLine(t *testing.T) {
	const poison = "\x1b[31mX\x1b[0m\r\nFORGED\u009bOSC"
	res := &command.CreateResult{
		Key: poison, ParentKey: poison, ParentAttempted: true,
		ParentErr: errors.New("network"),
	}
	out := captureStdout(t, func() {
		printCreateResult("PROJ", "Task", res, nil, "", false)
	})
	for _, control := range []string{"\x1b", "\r", "\u009b"} {
		if strings.Contains(out, control) {
			t.Errorf("output contains control %q: %q", control, out)
		}
	}
	if want := 4; strings.Count(out, "\n") != want {
		t.Errorf("output has %d lines, want %d: %q", strings.Count(out, "\n"), want, out)
	}
}

func TestRunCreate_ParentLinkFailureExitsNonzeroBodyFree(t *testing.T) {
	const secret = "SENSITIVE-set-parent-detail-token-xyz"
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var methods []string
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task","subtask":false}]}]}`))
		case r.URL.Path == "/rest/api/3/issue/PROJ-1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-1","fields":{}}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"10010","key":"PROJ-101"}`))
		case r.URL.Path == "/rest/api/3/issue/PROJ-101" && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorMessages":["` + secret + `"]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			err = runCreate([]string{"--summary", "S", "--project", "PROJ", "--type", "Task", "--parent", "PROJ-1"})
		})
	})

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %T, want *errSilent (body-free, nonzero exit)", err)
	}
	if !hasOwnLine(stdout, "PROJ-101") {
		t.Errorf("stdout = %q, want the new key on its own line even on a parent-link failure", stdout)
	}
	want := "Could not set PROJ-1 as PROJ-101's parent: Jira returned an unexpected response (400). Set one with the parent/child link primitive once you have a valid target."
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain the body-free failure line %q", stdout, want)
	}
	if strings.Contains(stdout, "Done.") {
		t.Errorf("stdout = %q, want no \"Done.\" on a partial failure", stdout)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Errorf("output leaked the Jira response body: stdout=%q stderr=%q", stdout, stderr)
	}
	for _, m := range methods {
		if strings.HasPrefix(m, http.MethodDelete+" ") {
			t.Errorf("issued %q, want no DELETE (the created item is never rolled back)", m)
		}
	}
}

func TestRunCreate_ParentLinkSuccessReportsAndExitsZero(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var sawSetParent bool
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task","subtask":false}]}]}`))
		case r.URL.Path == "/rest/api/3/issue/PROJ-1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-1","fields":{}}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"10010","key":"PROJ-123"}`))
		case r.URL.Path == "/rest/api/3/issue/PROJ-123" && r.Method == http.MethodPut:
			sawSetParent = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			err = runCreate([]string{"--summary", "S", "--project", "PROJ", "--type", "Task", "--parent", "PROJ-1"})
		})
	})
	if err != nil {
		t.Fatalf("runCreate = %v, want nil", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty on success", stderr)
	}
	if !sawSetParent {
		t.Error("SetParent PUT was never issued")
	}
	if !strings.Contains(stdout, "Parent set: PROJ-123 -> PROJ-1.\n") {
		t.Errorf("stdout = %q, want the \"Parent set:\" line", stdout)
	}
	if !strings.HasSuffix(stdout, "Done.\n") {
		t.Errorf("stdout = %q, want a closing \"Done.\"", stdout)
	}
}
