package quarter

import (
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

func TestFindVisionEpic_EmptyInput(t *testing.T) {
	for name, items := range map[string][]QuarterIssue{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := FindVisionEpic(items)
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if ok {
				t.Errorf("ok = true, want false")
			}
			if got.Key != "" {
				t.Errorf("got %+v, want zero QuarterIssue", got)
			}
		})
	}
}

func TestFindVisionEpic_NoMatch(t *testing.T) {
	items := []QuarterIssue{
		{Key: "PROJ-1", Summary: "Ship the thing"},

		{Key: "PROJ-2", Summary: "team _vision Q1'26"},
		{Key: "PROJ-3", Summary: "Vision Q1'26"},
	}
	got, ok, err := FindVisionEpic(items)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ok {
		t.Errorf("ok = true, want false")
	}
	if got.Key != "" {
		t.Errorf("got %+v, want zero QuarterIssue", got)
	}
}

func TestFindVisionEpic_OneMatch(t *testing.T) {
	items := []QuarterIssue{
		{Key: "PROJ-1", Summary: "Ship the thing"},
		{Key: "PROJ-2", Summary: "Platform _Vision Q1'26"},
		{Key: "PROJ-3", Summary: "Another item"},
	}
	got, ok, err := FindVisionEpic(items)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if got.Key != "PROJ-2" || got.Summary != "Platform _Vision Q1'26" {
		t.Errorf("got %+v, want PROJ-2", got)
	}
}

func TestFindVisionEpic_MarkerAnywhereInSummary(t *testing.T) {
	for _, summary := range []string{"_Vision", "_Vision Q1'26", "Platform _Vision", "a_Visionb"} {
		got, ok, err := FindVisionEpic([]QuarterIssue{{Key: "PROJ-9", Summary: summary}})
		if err != nil || !ok || got.Key != "PROJ-9" {
			t.Errorf("summary %q: got (%+v, %v, %v), want PROJ-9 match", summary, got, ok, err)
		}
	}
}

func TestFindVisionEpic_Ambiguous(t *testing.T) {
	items := []QuarterIssue{
		{Key: "PROJ-1", Summary: "Platform _Vision Q1'26"},
		{Key: "PROJ-2", Summary: "Unrelated"},
		{Key: "PROJ-3", Summary: "Data _Vision Q1'26"},
		{Key: "PROJ-4", Summary: "Growth _Vision Q1'26"},
	}
	got, ok, err := FindVisionEpic(items)
	if err == nil {
		t.Fatalf("err = nil, want ambiguity error")
	}
	if ok {
		t.Errorf("ok = true, want false")
	}
	if got.Key != "" {
		t.Errorf("got %+v, want zero QuarterIssue", got)
	}
	for _, key := range []string{"PROJ-1", "PROJ-3", "PROJ-4"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not name %s", err, key)
		}
	}
	if strings.Contains(err.Error(), "PROJ-2") {
		t.Errorf("error %q names non-matching PROJ-2", err)
	}
}

func visionChild(key, status string, created time.Time) jira.Issue {
	return jira.Issue{
		Key: key,
		Fields: jira.IssueFields{
			Status:  jira.Status{Name: status},
			Created: created,
		},
	}
}

func TestSelectActiveVisionItem_ZeroChildren(t *testing.T) {
	for name, children := range map[string][]jira.Issue{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			got, ok := SelectActiveVisionItem(children)
			if ok {
				t.Errorf("ok = true, want false")
			}
			if got.Key != "" {
				t.Errorf("got %+v, want zero Issue", got)
			}
		})
	}
}

func TestSelectActiveVisionItem_AllDone(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	children := []jira.Issue{
		visionChild("V-1", "Done", base),
		visionChild("V-2", "done", base.Add(time.Hour)),
		visionChild("V-3", "DONE", base.Add(2*time.Hour)),
	}
	got, ok := SelectActiveVisionItem(children)
	if ok {
		t.Errorf("ok = true, want false")
	}
	if got.Key != "" {
		t.Errorf("got %+v, want zero Issue", got)
	}
}

func TestSelectActiveVisionItem_OneNonDone(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	children := []jira.Issue{

		visionChild("V-1", "Done", base.Add(time.Hour)),
		visionChild("V-2", "In Progress", base),
	}
	got, ok := SelectActiveVisionItem(children)
	if !ok || got.Key != "V-2" {
		t.Errorf("got (%q, %v), want (V-2, true)", got.Key, ok)
	}
}

func TestSelectActiveVisionItem_LatestCreatedWins(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	children := []jira.Issue{
		visionChild("V-1", "To Do", base),
		visionChild("V-2", "In Progress", base.Add(2*time.Hour)),
		visionChild("V-3", "To Do", base.Add(time.Hour)),
		visionChild("V-4", "Done", base.Add(3*time.Hour)),
	}
	got, ok := SelectActiveVisionItem(children)
	if !ok || got.Key != "V-2" {
		t.Errorf("got (%q, %v), want (V-2, true)", got.Key, ok)
	}
}

func TestSelectActiveVisionItem_TieFirstWins(t *testing.T) {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	children := []jira.Issue{
		visionChild("V-1", "Done", when),
		visionChild("V-2", "To Do", when),
		visionChild("V-3", "In Progress", when),
	}
	got, ok := SelectActiveVisionItem(children)
	if !ok || got.Key != "V-2" {
		t.Errorf("got (%q, %v), want (V-2, true)", got.Key, ok)
	}
}

func TestSelectActiveVisionItem_DoneIsWholeString(t *testing.T) {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, ok := SelectActiveVisionItem([]jira.Issue{visionChild("V-1", "Done - pending review", when)})
	if !ok || got.Key != "V-1" {
		t.Errorf("got (%q, %v), want (V-1, true)", got.Key, ok)
	}
}
