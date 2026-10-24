package main

import (
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/plan"
)

func TestRunPlanContext_Rejections(t *testing.T) {
	cases := map[string][]string{
		"no window":        {},
		"only from":        {"--from", "2026-10-01"},
		"day and week":     {"--day", "--week"},
		"all windows":      {"--day", "--week", "--month"},
		"bad from":         {"--day", "--from", "2026/10/01"},
		"empty from":       {"--day", "--from", ""},
		"json and md":      {"--day", "--json", "--md"},
		"positional":       {"--day", "extra"},
		"unknown flag":     {"--day", "--bogus"},
		"from missing arg": {"--day", "--from"},
		"day false":        {"--day=false"},
		"day false week":   {"--day=false", "--week=false"},
		"double dash":      {"--day", "--", "extra"},
		"double dash help": {"--day", "--", "--help"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runPlan(append([]string{"context"}, args...)) })
				if !strings.HasPrefix(stderr, "jirahere: ") || strings.Count(strings.TrimRight(stderr, "\n"), "\n") != 0 {
					t.Errorf("stderr = %q, want one jirahere: line", stderr)
				}
			})
			var u *errUsage
			if !errors.As(err, &u) {
				t.Fatalf("err = %v, want *errUsage (exit 2)", err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

func TestRunPlanContext_Valid(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		kind    plan.Kind
		from    string
		format  string
		refresh bool
	}{
		{"day", []string{"--day"}, plan.Day, "", "md", false},
		{"week", []string{"--week"}, plan.Week, "", "md", false},
		{"month json", []string{"--month", "--json"}, plan.Month, "", "json", false},
		{"explicit md", []string{"--week", "--md"}, plan.Week, "", "md", false},
		{"from", []string{"--day", "--from", "2026-10-01"}, plan.Day, "2026-10-01", "md", false},
		{"from=", []string{"--day", "--from=2026-02-28"}, plan.Day, "2026-02-28", "md", false},
		{"repeated window", []string{"--day", "--day"}, plan.Day, "", "md", false},
		{"day false plus week", []string{"--day=false", "--week"}, plan.Week, "", "md", false},
		{"refresh", []string{"--week", "--refresh"}, plan.Week, "", "md", true},
		{"all", []string{"--month", "--from", "2026-10-01", "--json", "--refresh"}, plan.Month, "2026-10-01", "json", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parsePlanContextArgs(c.args)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.kind != c.kind || got.format != c.format || got.refresh != c.refresh {
				t.Errorf("got %+v", got)
			}
			if c.from != "" {
				if want, _ := plan.ParseFrom(c.from); !got.from.Equal(want) {
					t.Errorf("from = %v, want %v", got.from, want)
				}
			} else if d := time.Since(got.from); d > time.Minute || d < -time.Minute {
				t.Errorf("default from = %v, want now", got.from)
			}
		})
	}
}

func TestRunPlanContext_Help(t *testing.T) {
	var err error
	out := captureStdout(t, func() { err = runPlan([]string{"context", "--day", "--help"}) })
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	for _, f := range []string{"--day", "--week", "--month", "--from", "--json", "--md", "--refresh"} {
		if !strings.Contains(out, f) {
			t.Errorf("help missing %s:\n%s", f, out)
		}
	}
}

func TestParsePlanContextArgs_DefaultFromIsToday(t *testing.T) {
	got, err := parsePlanContextArgs([]string{"--day"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	y1, m1, d1 := got.from.Date()
	y2, m2, d2 := now.Date()
	if y1 != y2 || m1 != m2 || d1 != d2 {

		y3, m3, d3 := now.Add(-time.Minute).Date()
		if y1 != y3 || m1 != m3 || d1 != d3 {
			t.Errorf("default from = %v, want today (%v)", got.from, now)
		}
	}
}

func TestRunPlanContext_DoubleDashHelpIsPositional(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() { err = runPlan([]string{"context", "--day", "--", "--help"}) })
	})
	if errors.Is(err, flag.ErrHelp) || out != "" {
		t.Errorf("err = %v, stdout = %q; -- must end help detection", err, out)
	}
}
