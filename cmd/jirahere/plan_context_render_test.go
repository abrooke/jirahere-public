package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/plan"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

type fakePlanClient struct {
	inv    []quarter.QuarterIssue
	rel    []jira.Issue
	vision *jira.Issue
	err    error
}

func (f fakePlanClient) Inventory(context.Context) ([]quarter.QuarterIssue, error) {
	return f.inv, f.err
}
func (f fakePlanClient) SearchRelations(context.Context, []string) ([]jira.Issue, error) {
	return f.rel, nil
}
func (f fakePlanClient) GetIssue(context.Context, string) (*jira.Issue, error) { return f.vision, nil }

func stubPlanClient(t *testing.T, f plan.Client) {
	t.Helper()
	orig := newPlanClient
	newPlanClient = func(auth.Credentials, string, bool) plan.Client { return f }
	t.Cleanup(func() { newPlanClient = orig })
}

func utc(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC) }

func qi(key, summary, status string, updated time.Time) quarter.QuarterIssue {
	q := quarter.QuarterIssue{Key: key, Summary: summary, Created: utc(1, 9), Updated: updated, Assignee: "Ann"}
	q.Status.Name = status
	return q
}

func unassigned(q quarter.QuarterIssue) quarter.QuarterIssue { q.Assignee = ""; return q }

func relIssue(key, parent, status string, resolved *time.Time) jira.Issue {
	i := jira.Issue{Key: key}
	i.Fields.Status.Name = status
	i.Fields.Resolved = resolved
	if parent != "" {
		i.Fields.Parent = &jira.ParentRef{Key: parent}
	}
	return i
}

func sampleClient() fakePlanClient {
	r := utc(9, 10)
	vis := &jira.Issue{Key: "PROJ-7"}
	vis.Fields.Summary = "Be healthy"
	vis.Fields.Description = json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Stay fit.\u0007 R&D"}]},{"type":"paragraph","content":[{"type":"text","text":"## Circuit"}]}]}`)
	return fakePlanClient{
		inv: []quarter.QuarterIssue{
			qi("PROJ-1", "FY26 _Vision Q3'26", "In Progress", utc(10, 9)),
			qi("PROJ-2", "FY26 _Plan Q3'26", "In Progress", utc(11, 9)),
			qi("PROJ-3", "Zone: Home :: Gym Q3'26", "In Progress", utc(5, 12)),
			qi("PROJ-4", "Zone: Gym :: Work Q3'26", "In Progress", utc(9, 10)),
			qi("PROJ-5", `Pipe | cell a\|b R&D <x>`, "To Do", utc(12, 12)),
			unassigned(qi("PROJ-10", "No owner", "To Do", utc(4, 9))),
			qi("PROJ-6", "Zone: Work :: Home Q3'26", "To Do", utc(3, 9)),
			qi("PROJ-7", "Be healthy", "In Progress", utc(2, 9)),
		},
		rel: []jira.Issue{
			relIssue("PROJ-7", "PROJ-1", "In Progress", nil),
			relIssue("PROJ-3", "PROJ-2", "In Progress", nil),
			relIssue("PROJ-5", "PROJ-2", "To Do", nil),
			relIssue("PROJ-9", "PROJ-4", "Done", &r),
		},
		vision: vis,
	}
}

func runPlanContextStubbed(t *testing.T, c fakePlanClient, args ...string) (string, string, error) {
	t.Helper()
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubPlanClient(t, c)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runPlan(append([]string{"context", "--week", "--from", "2026-10-12"}, args...)) })
	})
	return stdout, stderr, err
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s:\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

func TestRunPlanContext_Golden(t *testing.T) {
	cases := []struct {
		name     string
		duplicat bool
		args     []string
		golden   string
	}{
		{"json", false, []string{"--json"}, "plan_context.json.golden"},
		{"md", false, nil, "plan_context.md.golden"},
		{"json circuit error", true, []string{"--json"}, "plan_context_circuit_error.json.golden"},
		{"md circuit error", true, nil, "plan_context_circuit_error.md.golden"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := sampleClient()
			if c.duplicat {

				client.inv = append(client.inv, qi("PROJ-8", "Zone: Gym :: Home Q3'26", "To Do", utc(3, 9)))
			}
			out, stderr, err := runPlanContextStubbed(t, client, c.args...)
			if err != nil || stderr != "" {
				t.Fatalf("err = %v, stderr = %q", err, stderr)
			}
			checkGolden(t, c.golden, out)
			if strings.Contains(c.name, "json") {
				var v map[string]any
				if err := json.Unmarshal([]byte(out), &v); err != nil {
					t.Fatalf("stdout is not one JSON document: %v", err)
				}
			}
		})
	}
}

func TestRunPlanContext_JiraFailure(t *testing.T) {
	out, stderr, err := runPlanContextStubbed(t, fakePlanClient{err: errors.New("boom")})
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v, want *errSilent (exit 1)", err)
	}
	if out != "" || !strings.HasPrefix(stderr, "jirahere: plan context: ") || strings.Count(strings.TrimRight(stderr, "\n"), "\n") != 0 {
		t.Errorf("stdout = %q, stderr = %q", out, stderr)
	}
}

func TestRunPlanContext_NotLoggedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettingsFile(t, planSettings)
	stubPlanClient(t, fakePlanClient{})
	var err error
	out := captureStdout(t, func() { _ = captureStderr(t, func() { err = runPlan([]string{"context", "--day"}) }) })
	var silent *errSilent
	if !errors.As(err, &silent) || out != "" {
		t.Errorf("err = %v, stdout = %q, want exit 1 / empty", err, out)
	}
}

func TestRunPlanContext_StdoutWriteError(t *testing.T) {
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubPlanClient(t, sampleClient())
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = f
	t.Cleanup(func() { os.Stdout = origStdout })
	var runErr error
	stderr := captureStderr(t, func() {
		runErr = runPlan([]string{"context", "--week", "--from", "2026-10-12", "--json"})
	})
	os.Stdout = origStdout
	var silent *errSilent
	if !errors.As(runErr, &silent) || !strings.Contains(stderr, "could not write output") {
		t.Errorf("err = %v, stderr = %q, want exit 1 with write-error line", runErr, stderr)
	}
}

func TestNewBundleCircuit_LastNext(t *testing.T) {
	order := plan.Order{Zones: []plan.Zone{{Name: "A", Key: "P-1"}, {Name: "B", Key: "P-2"}, {Name: "C", Key: "P-3"}}}
	d := func(day int) *plan.Done { return &plan.Done{Key: "P-9", Resolved: utc(day, 9)} }
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	cases := []struct {
		name       string
		closed     bool
		items      []plan.Item
		last, next string
	}{
		{"none done", false, nil, "<nil>", "A"},
		{"middle", false, []plan.Item{{Key: "P-2", LastDone: d(3)}}, "B", "C"},
		{"latest wins", false, []plan.Item{{Key: "P-1", LastDone: d(5)}, {Key: "P-2", LastDone: d(3)}}, "A", "B"},
		{"open tail", false, []plan.Item{{Key: "P-3", LastDone: d(3)}}, "C", "<nil>"},
		{"closed wraps", true, []plan.Item{{Key: "P-3", LastDone: d(3)}}, "C", "A"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := order
			o.Closed = c.closed
			got := newBundleCircuit(o, c.items)
			if str(got.Last) != c.last || str(got.Next) != c.next {
				t.Errorf("last/next = %s/%s, want %s/%s", str(got.Last), str(got.Next), c.last, c.next)
			}
		})
	}
}
