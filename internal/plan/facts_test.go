package plan

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

type stubClient struct {
	inv      []quarter.QuarterIssue
	invErr   error
	rel      []jira.Issue
	relErr   error
	issues   map[string]*jira.Issue
	relCalls int
	getCalls []string
}

func (s *stubClient) Inventory(context.Context) ([]quarter.QuarterIssue, error) {
	return s.inv, s.invErr
}

func (s *stubClient) SearchRelations(_ context.Context, _ []string) ([]jira.Issue, error) {
	s.relCalls++
	return s.rel, s.relErr
}

func (s *stubClient) GetIssue(_ context.Context, key string) (*jira.Issue, error) {
	s.getCalls = append(s.getCalls, key)
	if is, ok := s.issues[key]; ok {
		return is, nil
	}
	return nil, errors.New("not found")
}

func ts(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

func qi(key, summary, status string, updated time.Time) quarter.QuarterIssue {
	return quarter.QuarterIssue{Key: key, Summary: summary, Status: jira.Status{Name: status},
		Created: ts(2026, 10, 1, 9), Updated: updated, Assignee: "Ann"}
}

func child(key, parent, status string, created time.Time, resolved *time.Time) jira.Issue {
	var is jira.Issue
	is.Key = key
	is.Fields.Parent = &jira.ParentRef{Key: parent}
	is.Fields.Status = jira.Status{Name: status}
	is.Fields.Created = created
	is.Fields.Resolved = resolved
	return is
}

func tp(t time.Time) *time.Time { return &t }

var win = Window(Week, ts(2026, 10, 16, 15))

func TestGatherNoVisionNoChildren(t *testing.T) {
	c := &stubClient{inv: []quarter.QuarterIssue{
		qi("P-2", "Beta", "To Do", ts(2026, 10, 10, 8)),
		qi("P-1", "Alpha", "In Progress", ts(2026, 10, 16, 8)),
	}}
	f, err := Gather(context.Background(), c, win, "Q4'26")
	if err != nil {
		t.Fatal(err)
	}
	if f.Vision != nil || f.PlanEpic != nil {
		t.Errorf("Vision=%v PlanEpic=%v, want both absent", f.Vision, f.PlanEpic)
	}
	if len(c.getCalls) != 0 {
		t.Errorf("GetIssue calls = %v, want none", c.getCalls)
	}
	if len(f.Items) != 2 || f.Items[0].Key != "P-1" || f.Items[1].Key != "P-2" {
		t.Fatalf("items not sorted by key: %+v", f.Items)
	}
	for _, it := range f.Items {
		if it.LastDone != nil || it.ParentKey != "" {
			t.Errorf("%s: LastDone=%v ParentKey=%q, want none", it.Key, it.LastDone, it.ParentKey)
		}
	}
	p1 := f.Items[0]
	if p1.Summary != "Alpha" || p1.Status != "In Progress" || p1.Assignee != "Ann" || !p1.Created.Equal(ts(2026, 10, 1, 9)) {
		t.Errorf("fields not carried through: %+v", p1)
	}
	if c.relCalls != 1 {
		t.Errorf("SearchRelations calls = %d, want 1", c.relCalls)
	}
}

func TestGatherAgeDays(t *testing.T) {

	tests := []struct {
		name    string
		updated time.Time
		want    int
	}{
		{"same day late", ts(2026, 10, 16, 23), 0},
		{"one day earlier late evening", ts(2026, 10, 15, 23), 1},
		{"ten days", ts(2026, 10, 6, 1), 10},
		{"after window start", ts(2026, 10, 18, 12), -2},
		{"across month", ts(2026, 9, 30, 12), 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &stubClient{inv: []quarter.QuarterIssue{qi("P-1", "x", "To Do", tt.updated)}}
			f, err := Gather(context.Background(), c, win, "Q4'26")
			if err != nil {
				t.Fatal(err)
			}
			if got := f.Items[0].AgeDays; got != tt.want {
				t.Errorf("AgeDays = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGatherDoneChildren(t *testing.T) {
	c := &stubClient{
		inv: []quarter.QuarterIssue{
			qi("P-1", "Epic", "In Progress", ts(2026, 10, 14, 0)),
			qi("P-2", "Other epic", "In Progress", ts(2026, 10, 14, 0)),
			qi("P-3", "Child in quarter", "Done", ts(2026, 10, 14, 0)),
			qi("P-4", "Only open children", "To Do", ts(2026, 10, 14, 0)),
		},
		rel: []jira.Issue{
			child("P-3", "P-1", "Done", ts(2026, 10, 2, 0), tp(ts(2026, 10, 9, 0))),
			child("P-90", "P-1", "Done", ts(2026, 10, 2, 0), tp(ts(2026, 10, 12, 0))),
			child("P-91", "P-1", "In Progress", ts(2026, 10, 2, 0), nil),
			child("P-92", "P-1", "done", ts(2026, 10, 2, 0), tp(ts(2026, 10, 11, 0))),
			child("P-93", "P-2", "Done", ts(2026, 10, 2, 0), nil),
			child("P-94", "P-4", "To Do", ts(2026, 10, 2, 0), nil),
			child("P-1", "P-0", "In Progress", ts(2026, 10, 1, 0), nil),
		},
	}
	f, err := Gather(context.Background(), c, win, "Q4'26")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Item{}
	for _, it := range f.Items {
		by[it.Key] = it
	}
	if d := by["P-1"].LastDone; d == nil || d.Key != "P-90" || !d.Resolved.Equal(ts(2026, 10, 12, 0)) {
		t.Errorf("P-1 LastDone = %+v, want P-90 @ 10-12", d)
	}
	if by["P-1"].ParentKey != "P-0" {
		t.Errorf("P-1 ParentKey = %q, want P-0 (parent outside the quarter)", by["P-1"].ParentKey)
	}
	if by["P-3"].ParentKey != "P-1" {
		t.Errorf("P-3 ParentKey = %q, want P-1", by["P-3"].ParentKey)
	}
	if by["P-2"].LastDone != nil {
		t.Errorf("P-2 LastDone = %+v, want none (Done child undated)", by["P-2"].LastDone)
	}
	if by["P-4"].LastDone != nil {
		t.Errorf("P-4 LastDone = %+v, want none (no Done child)", by["P-4"].LastDone)
	}
}

func TestLastDoneTieGoesToLowestKey(t *testing.T) {
	r := tp(ts(2026, 10, 9, 0))
	got := lastDone([]jira.Issue{
		child("P-7", "P-1", "Done", time.Time{}, r),
		child("P-5", "P-1", "Done", time.Time{}, r),
	})
	if got == nil || got.Key != "P-5" {
		t.Errorf("lastDone = %+v, want P-5", got)
	}
}

func TestLastDoneTieUsesNumericKeyOrder(t *testing.T) {
	r := tp(ts(2026, 10, 9, 0))
	got := lastDone([]jira.Issue{
		child("P-10", "P-1", "Done", time.Time{}, r),
		child("P-9", "P-1", "Done", time.Time{}, r),
	})
	if got == nil || got.Key != "P-9" {
		t.Errorf("lastDone = %+v, want P-9", got)
	}
}

func TestKeyLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"P-9", "P-10", true},
		{"P-10", "P-9", false},
		{"A-10", "B-2", true},
		{"P-1", "P-1", false},
		{"odd", "P-1", false},
		{"P-1", "odd", true},
		{"P", "P-1", true},
		{"P-1", "P", false},
		{"P-007", "P-8", true},
		{"P-8", "P-007", false},
		{"P-007", "P-7", true},
		{"P-7", "P-007", false},
		{"P-99999999999999999999", "P-1", false},
		{"P-1", "P-99999999999999999999", true},
	}
	for _, c := range cases {
		if got := keyLess(c.a, c.b); got != c.want {
			t.Errorf("keyLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	keys := []string{"P-10", "P-9", "A-100", "P-2"}
	sort.Slice(keys, func(i, j int) bool { return keyLess(keys[i], keys[j]) })
	if strings.Join(keys, " ") != "A-100 P-2 P-9 P-10" {
		t.Errorf("sorted = %v", keys)
	}
}

func adfDoc(text string) json.RawMessage { return adf.FromPlainText(text) }

func TestGatherVisionAndPlanEpic(t *testing.T) {
	visionItem := &jira.Issue{Key: "P-11"}
	visionItem.Fields.Summary = "Ship it"
	visionItem.Fields.Description = adfDoc("Be bold.")

	c := &stubClient{
		inv: []quarter.QuarterIssue{
			qi("P-10", "_Vision Q4'26", "In Progress", ts(2026, 10, 1, 0)),
			qi("P-20", "_Plan Q4'26", "In Progress", ts(2026, 10, 1, 0)),
		},
		rel: []jira.Issue{
			child("P-12", "P-10", "Done", ts(2026, 10, 5, 0), tp(ts(2026, 10, 6, 0))),
			child("P-11", "P-10", "In Progress", ts(2026, 10, 3, 0), nil),
			child("P-13", "P-10", "In Progress", ts(2026, 10, 2, 0), nil),
		},
		issues: map[string]*jira.Issue{"P-11": visionItem},
	}
	f, err := Gather(context.Background(), c, win, "Q4'26")
	if err != nil {
		t.Fatal(err)
	}
	if f.Vision == nil || f.Vision.Key != "P-11" || f.Vision.Summary != "Ship it" || f.Vision.Description != "Be bold." {
		t.Errorf("Vision = %+v", f.Vision)
	}
	if f.PlanEpic == nil || f.PlanEpic.Key != "P-20" {
		t.Errorf("PlanEpic = %+v", f.PlanEpic)
	}
	if len(c.getCalls) != 1 {
		t.Errorf("GetIssue calls = %v, want exactly one", c.getCalls)
	}
}

func TestGatherVisionEpicWithoutActiveChildIsAbsent(t *testing.T) {
	c := &stubClient{
		inv: []quarter.QuarterIssue{qi("P-10", "_Vision", "In Progress", ts(2026, 10, 1, 0))},
		rel: []jira.Issue{child("P-12", "P-10", "Done", ts(2026, 10, 5, 0), tp(ts(2026, 10, 6, 0)))},
	}
	f, err := Gather(context.Background(), c, win, "Q4'26")
	if err != nil {
		t.Fatal(err)
	}
	if f.Vision != nil {
		t.Errorf("Vision = %+v, want absent", f.Vision)
	}
}

func TestGatherErrors(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		c    *stubClient
		want string
	}{
		{"inventory", &stubClient{invErr: boom}, "could not list"},
		{"relations", &stubClient{relErr: boom}, "parents and children"},
		{"ambiguous vision", &stubClient{inv: []quarter.QuarterIssue{
			qi("P-1", "_Vision a", "To Do", ts(2026, 10, 1, 0)), qi("P-2", "_Vision b", "To Do", ts(2026, 10, 1, 0))}}, "P-1, P-2"},
		{"ambiguous plan", &stubClient{inv: []quarter.QuarterIssue{
			qi("P-1", "_Plan Q4'26 a", "To Do", ts(2026, 10, 1, 0)), qi("P-2", "_Plan Q4'26 b", "To Do", ts(2026, 10, 1, 0))}}, "P-1, P-2"},
		{"vision read", &stubClient{
			inv: []quarter.QuarterIssue{qi("P-10", "_Vision", "To Do", ts(2026, 10, 1, 0))},
			rel: []jira.Issue{child("P-11", "P-10", "To Do", ts(2026, 10, 3, 0), nil)}}, "P-11"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Gather(context.Background(), tt.c, win, "Q4'26")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}
