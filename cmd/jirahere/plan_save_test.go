package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/plan"
)

func planSaveBodyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunPlanSave_Rejections(t *testing.T) {
	body := planSaveBodyFile(t, "plan text\n")
	empty := planSaveBodyFile(t, "")
	missing := filepath.Join(t.TempDir(), "nope.md")
	tests := []struct {
		name    string
		args    []string
		wantMsg string

		usage bool
	}{
		{"missing kind", []string{"--body-file", body}, "a kind (day, week or month) is required", true},
		{"no args", nil, "a kind (day, week or month) is required", true},
		{"unknown kind", []string{"year", "--body-file", body}, `unknown kind "year"`, true},
		{"kind wrong case", []string{"Day", "--body-file", body}, `unknown kind "Day"`, true},
		{"extra positional", []string{"day", "week", "--body-file", body}, `unexpected argument "week"`, true},
		{"extra positional after flags", []string{"day", "--body-file", body, "extra"}, `unexpected argument "extra"`, true},
		{"missing body-file", []string{"day"}, "--body-file is required", true},
		{"body-file unreadable", []string{"day", "--body-file", missing}, "jirahere: plan save: ", false},
		{"body-file empty", []string{"day", "--body-file", empty}, "jirahere: plan save: ", false},
		{"invalid date", []string{"day", "--body-file", body, "--date", "2026-13-40"}, "--date: invalid date", true},
		{"date wrong form", []string{"week", "--body-file", body, "--date", "10/17/2026"}, "--date: invalid date", true},
		{"empty date", []string{"month", "--body-file", body, "--date="}, "--date: invalid date", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runPlan(append([]string{"save"}, tc.args...)) })
			if err == nil {
				t.Fatal("err = nil, want rejection")
			}
			var us *errUsage
			if gotUsage := errors.As(err, &us); gotUsage != tc.usage {
				t.Errorf("usage error (exit 2) = %v, want %v: %v", gotUsage, tc.usage, err)
			}
			if !strings.HasPrefix(stderr, "jirahere: plan save: ") || strings.Count(strings.TrimRight(stderr, "\n"), "\n") != 0 {
				t.Errorf("stderr = %q, want one jirahere: plan save: line", stderr)
			}
			if !strings.Contains(stderr, tc.wantMsg) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.wantMsg)
			}
			if strings.Contains(stderr, "not implemented") {
				t.Errorf("stderr = %q: pre-flight should have rejected before the stub", stderr)
			}
		})
	}
}

func TestRunPlanSave_InlineBodyFlagRejected(t *testing.T) {
	var err error
	captureStderr(t, func() { err = runPlan([]string{"save", "day", "--body", "x"}) })
	var us *errUsage
	if !errors.As(err, &us) {
		t.Errorf("err = %v, want unknown-flag usage error", err)
	}
}

func TestParsePlanSaveArgs_Valid(t *testing.T) {
	body := planSaveBodyFile(t, "\xef\xbb\xbfhello\r\n")
	want := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
	tests := []struct {
		name     string
		args     []string
		stdin    string
		kind     plan.Kind
		body     string
		wantDate bool
	}{
		{"day file", []string{"day", "--body-file", body}, "", plan.Day, "hello", false},
		{"week file with date", []string{"week", "--body-file", body, "--date", "2026-10-17"}, "", plan.Week, "hello", true},
		{"month flags first", []string{"--body-file", body, "--date=2026-10-17", "month"}, "", plan.Month, "hello", true},
		{"day stdin", []string{"day", "--body-file", "-"}, "from stdin\n", plan.Day, "from stdin", false},
		{"kind after double dash", []string{"--body-file", body, "--", "week"}, "", plan.Week, "hello", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePlanSaveArgs(tc.args, strings.NewReader(tc.stdin))
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.kind != tc.kind || got.body != tc.body {
				t.Errorf("got kind=%q body=%q, want %q %q", got.kind, got.body, tc.kind, tc.body)
			}
			if tc.wantDate && !got.from.Equal(want) {
				t.Errorf("from = %v, want %v", got.from, want)
			}
			if !tc.wantDate && time.Since(got.from) > time.Minute {
				t.Errorf("from = %v, want ~now", got.from)
			}
		})
	}
}

func TestParsePlanSaveArgs_BadDateBeforeBodyRead(t *testing.T) {
	_, err := parsePlanSaveArgs([]string{"day", "--body-file", "-", "--date", "nope"}, strings.NewReader(""))
	var us *errUsage
	if !errors.As(err, &us) {
		t.Errorf("err = %v, want usage error", err)
	}
}

func TestRunPlanSave_Help(t *testing.T) {
	var err error
	out := captureStdout(t, func() { err = runPlan([]string{"save", "--help"}) })
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out, "--body-file") || !strings.Contains(out, "no inline body flag") {
		t.Errorf("help = %q, want --body-file and the no-inline note", out)
	}
}
