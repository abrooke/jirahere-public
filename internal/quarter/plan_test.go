package quarter

import (
	"strings"
	"testing"
)

func TestFindPlanEpic_EmptyInput(t *testing.T) {
	for name, items := range map[string][]QuarterIssue{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := FindPlanEpic(items, "Q4'26")
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

func TestFindPlanEpic_NoMatch(t *testing.T) {
	items := []QuarterIssue{
		{Key: "PROJ-1", Summary: "Ship the thing"},

		{Key: "PROJ-2", Summary: "team _plan Q4'26"},
		{Key: "PROJ-3", Summary: "Plan Q4'26"},
	}
	got, ok, err := FindPlanEpic(items, "Q4'26")
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

func TestFindPlanEpic_OneMatch(t *testing.T) {
	items := []QuarterIssue{
		{Key: "PROJ-1", Summary: "Ship the thing"},
		{Key: "PROJ-2", Summary: "Platform _Plan Q4'26"},
		{Key: "PROJ-3", Summary: "Another item"},
	}
	got, ok, err := FindPlanEpic(items, "Q4'26")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if got.Key != "PROJ-2" || got.Summary != "Platform _Plan Q4'26" {
		t.Errorf("got %+v, want PROJ-2", got)
	}
}

func TestFindPlanEpic_Ambiguous(t *testing.T) {
	items := []QuarterIssue{
		{Key: "PROJ-1", Summary: "Platform _Plan Q4'26"},
		{Key: "PROJ-2", Summary: "Unrelated"},
		{Key: "PROJ-3", Summary: "_Plan Q4'26 again"},
		{Key: "PROJ-4", Summary: "Third _Plan Q4'26"},
	}
	got, ok, err := FindPlanEpic(items, "Q4'26")
	if err == nil {
		t.Fatal("err = nil, want ambiguity error")
	}
	if ok || got.Key != "" {
		t.Errorf("got (%+v, %v), want zero, false", got, ok)
	}
	for _, k := range []string{"PROJ-1", "PROJ-3", "PROJ-4"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error %q does not name %s", err, k)
		}
	}
	if strings.Contains(err.Error(), "PROJ-2") {
		t.Errorf("error %q names non-matching PROJ-2", err)
	}
}

func TestFindPlanEpic_RejectsFalsePositives(t *testing.T) {
	items := []QuarterIssue{
		{Key: "PROJ-1", Summary: "Fix _Plan parsing"},
		{Key: "PROJ-2", Summary: "_Plan Q3'26"},
		{Key: "PROJ-3", Summary: "Platform _Plan Q4'26"},
	}
	got, ok, err := FindPlanEpic(items, "Q4'26")
	if err != nil || !ok || got.Key != "PROJ-3" {
		t.Errorf("got (%+v, %v, %v), want PROJ-3 only", got, ok, err)
	}
	if _, ok, err := FindPlanEpic(items[:2], "Q4'26"); ok || err != nil {
		t.Errorf("ok=%v err=%v, want no match for false positives alone", ok, err)
	}
}

func TestFindPlanEpic_EmptyQuarterErrors(t *testing.T) {
	items := []QuarterIssue{{Key: "PROJ-1", Summary: "Fix _Plan parsing"}}
	for _, q := range []string{"", " ", "\t\n"} {
		got, ok, err := FindPlanEpic(items, q)
		if err == nil || ok || got.Key != "" {
			t.Errorf("quarter %q: got (%+v, %v, %v), want zero, false, error", q, got, ok, err)
		}
	}
}

func TestFindPlanEpic_RightBoundary(t *testing.T) {
	tests := []struct {
		name, quarter, summary string
		want                   bool
	}{
		{"longer label rejected", "Q3'26", "_Plan Q3'260", false},
		{"letter suffix rejected", "Q3'26", "_Plan Q3'26b", false},
		{"other quarter rejected", "Q3'26", "_Plan Q2'26", false},
		{"end of summary", "Q3'26", "Platform _Plan Q3'26", true},
		{"followed by space", "Q3'26", "_Plan Q3'26 again", true},
		{"followed by punct", "Q3'26", "_Plan Q3'26: roadmap", true},
		{"later occurrence accepted", "Q3'26", "_Plan Q3'260 vs _Plan Q3'26", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok, err := FindPlanEpic([]QuarterIssue{{Key: "PROJ-1", Summary: tt.summary}}, tt.quarter)
			if err != nil || ok != tt.want {
				t.Errorf("got ok=%v err=%v, want ok=%v", ok, err, tt.want)
			}
		})
	}
}
