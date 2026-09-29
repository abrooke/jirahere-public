package main

import (
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

const visionSettings = `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q3"}}`

func visionInventory(summaries map[string]string, order ...string) []quarter.QuarterIssue {
	var items []quarter.QuarterIssue
	for _, k := range order {
		items = append(items, quarter.QuarterIssue{
			Key:     k,
			Summary: summaries[k],
			Status:  jira.Status{Name: "In Progress"},
			Created: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Updated: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		})
	}
	return items
}

func visionJiraServer(t *testing.T, childrenBody string, fieldsByKey map[string]string) *[]string {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(childrenBody))
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		got = append(got, key)
		fields, ok := fieldsByKey[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":` + fields + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
	return &got
}

const (
	visionActiveFields = `{"summary":"Active vision item","labels":["fy26-q3"],
		"issuetype":{"id":"10001","name":"Task"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"In Progress"},
		"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Vision details."}]}]}}`

	visionChildrenBody = `{"issues":[
		{"id":"11","key":"PROJ-11","fields":{"summary":"Older","status":{"name":"To Do"},"created":"2026-07-01T10:00:00.000+0000"}},
		{"id":"12","key":"PROJ-12","fields":{"summary":"Active vision item","status":{"name":"In Progress"},"created":"2026-07-05T10:00:00.000+0000"}},
		{"id":"13","key":"PROJ-13","fields":{"summary":"Newest but done","status":{"name":"Done"},"created":"2026-07-09T10:00:00.000+0000"}}
	]}`
)

func TestRunVision_HappyPath_MatchesContextGetLevel0(t *testing.T) {
	for _, format := range []string{"md", "json"} {
		t.Run(format, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, visionSettings)
			stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{
				"PROJ-1": "Unrelated work",
				"PROJ-2": "FY26 _Vision Q3'26",
			}, "PROJ-1", "PROJ-2")})
			requested := visionJiraServer(t, visionChildrenBody, map[string]string{"PROJ-12": visionActiveFields})

			var args []string
			ctxArgs := []string{"PROJ-12"}
			if format == "json" {
				args = []string{"--json"}
				ctxArgs = append(ctxArgs, "--json")
			}
			var err error
			out := captureStdout(t, func() { err = runVision(args) })
			if err != nil {
				t.Fatalf("runVision: %v", err)
			}
			if len(*requested) != 1 || (*requested)[0] != "PROJ-12" {
				t.Errorf("GetIssue keys = %v, want [PROJ-12] (latest-created non-Done child)", *requested)
			}

			var cerr error
			want := captureStdout(t, func() { cerr = runContextGet(ctxArgs) })
			if cerr != nil {
				t.Fatalf("runContextGet: %v", cerr)
			}
			if out != want {
				t.Errorf("vision stdout differs from context get PROJ-12:\n got: %q\nwant: %q", out, want)
			}
			if format == "md" && !strings.HasPrefix(out, "# PROJ-12 — Active vision item (Task, In Progress)") {
				t.Errorf("md stdout = %q", out)
			}
			if format == "json" && !strings.Contains(out, `"key":"PROJ-12"`) && !strings.Contains(out, `"key": "PROJ-12"`) {
				t.Errorf("json stdout = %q, want PROJ-12", out)
			}
		})
	}
}

func TestRunVision_ExplicitMDIsDefault(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, visionSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")})
	visionJiraServer(t, visionChildrenBody, map[string]string{"PROJ-12": visionActiveFields})

	var errDefault, errMD error
	def := captureStdout(t, func() { errDefault = runVision(nil) })
	md := captureStdout(t, func() { errMD = runVision([]string{"--md"}) })
	if errDefault != nil || errMD != nil {
		t.Fatalf("errs = %v, %v", errDefault, errMD)
	}
	if def != md {
		t.Errorf("--md output %q differs from default %q", md, def)
	}
}

func visionExpectFailure(t *testing.T, args []string) string {
	t.Helper()
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runVision(args) })
	})
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v, want *errSilent (exit 1)", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on failure", stdout)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasSuffix(stderr, "\n") {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
	return stderr
}

func TestRunVision_NoVisionEpic(t *testing.T) {
	for _, format := range [][]string{nil, {"--json"}} {
		quarterListLogin(t)
		writeSettingsFile(t, visionSettings)
		stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Unrelated work"}, "PROJ-1")})
		visionJiraServer(t, `{"issues":[]}`, nil)

		stderr := visionExpectFailure(t, format)
		for _, want := range []string{"no Vision epic found", "FY26-Q3", "_Vision", "Create one"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, missing %q", stderr, want)
			}
		}
	}
}

func TestRunVision_AmbiguousVisionEpic(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, visionSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{
		"PROJ-1": "A _Vision Q3",
		"PROJ-2": "Other",
		"PROJ-3": "B _Vision Q3",
	}, "PROJ-1", "PROJ-2", "PROJ-3")})
	visionJiraServer(t, `{"issues":[]}`, nil)

	stderr := visionExpectFailure(t, nil)
	for _, want := range []string{"ambiguous vision epic", "PROJ-1", "PROJ-3"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, missing %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "PROJ-2") {
		t.Errorf("stderr = %q names non-matching PROJ-2", stderr)
	}
	if strings.Contains(stderr, "no Vision epic found") || strings.Contains(stderr, "no active") {
		t.Errorf("stderr = %q is not distinct from the other failure modes", stderr)
	}
}

func TestRunVision_NoActiveChild(t *testing.T) {
	for name, body := range map[string]string{
		"no children": `{"issues":[]}`,
		"all done": `{"issues":[
			{"id":"11","key":"PROJ-11","fields":{"summary":"x","status":{"name":"Done"},"created":"2026-07-01T10:00:00.000+0000"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, visionSettings)
			stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")})
			visionJiraServer(t, body, nil)

			stderr := visionExpectFailure(t, nil)
			for _, want := range []string{"no active Vision item", "PROJ-2", "non-Done"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, missing %q", stderr, want)
				}
			}
		})
	}
}

func TestRunVision_FlagErrors(t *testing.T) {
	stderr := visionExpectFailure(t, []string{"--json", "--md"})
	if !strings.Contains(stderr, "--json and --md may not be used together") {
		t.Errorf("stderr = %q", stderr)
	}
	stderr = visionExpectFailure(t, []string{"extra"})
	if !strings.Contains(stderr, visionUsage) {
		t.Errorf("stderr = %q, want %q", stderr, visionUsage)
	}
	visionExpectFailure(t, []string{"--bogus"})
}

func TestRunVision_NotLoggedInAndUnsetDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if stderr := visionExpectFailure(t, nil); !strings.Contains(stderr, "not logged in") {
		t.Errorf("stderr = %q, want not-logged-in", stderr)
	}

	quarterListLogin(t)
	stubQuarterSearcher(t, &fakeQuarterSearcher{})
	if stderr := visionExpectFailure(t, nil); !strings.Contains(stderr, "defaults.") {
		t.Errorf("stderr = %q, want unset-default message", stderr)
	}
}

func TestRunVision_JiraFailureIsBodyFree(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, visionSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")})
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	const secret = "response-body-secret-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(secret))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}

	stderr := visionExpectFailure(t, nil)
	if strings.Contains(stderr, secret) || !strings.Contains(stderr, "Jira returned an unexpected response (500)") || !strings.Contains(stderr, "PROJ-2") {
		t.Errorf("stderr = %q", stderr)
	}
}

func visionStatusServer(t *testing.T, childrenStatus, issueStatus int, secret string) {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/search/jql" {
			if childrenStatus != http.StatusOK {
				w.WriteHeader(childrenStatus)
				_, _ = w.Write([]byte(secret))
				return
			}
			_, _ = w.Write([]byte(visionChildrenBody))
			return
		}
		w.WriteHeader(issueStatus)
		_, _ = w.Write([]byte(secret))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
}

func TestRunVision_ChildrenOfFailure(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, visionSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")})
	const secret = "children-body-secret-must-not-leak"
	visionStatusServer(t, http.StatusInternalServerError, http.StatusOK, secret)

	stderr := visionExpectFailure(t, nil)
	for _, want := range []string{"could not list children of Vision epic PROJ-2", "Jira returned an unexpected response (500)"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, missing %q", stderr, want)
		}
	}
	if strings.Contains(stderr, secret) {
		t.Errorf("stderr = %q leaks the response body", stderr)
	}
}

func TestRunVision_GetIssueFailure(t *testing.T) {
	const secret = "issue-body-secret-must-not-leak"
	for name, tc := range map[string]struct {
		status int
		wants  []string
	}{
		"404":     {http.StatusNotFound, []string{"active Vision item PROJ-12 was not found (404)"}},
		"non-404": {http.StatusInternalServerError, []string{"could not read active Vision item PROJ-12", "Jira returned an unexpected response (500)"}},
	} {
		t.Run(name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, visionSettings)
			stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")})
			visionStatusServer(t, http.StatusOK, tc.status, secret)

			stderr := visionExpectFailure(t, nil)
			for _, want := range tc.wants {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, missing %q", stderr, want)
				}
			}
			if strings.Contains(stderr, secret) {
				t.Errorf("stderr = %q leaks the response body", stderr)
			}
		})
	}
}

func TestRunVision_HelpToken(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--bogus", "--help"}} {
		var err error
		out := captureStdout(t, func() { err = runVision(args) })
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("runVision(%v) err = %v, want flag.ErrHelp", args, err)
		}
		if !strings.Contains(out, "usage: jirahere vision") {
			t.Errorf("runVision(%v) stdout = %q", args, out)
		}
	}
}
