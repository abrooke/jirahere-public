package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/settings"
)

func TestRunConfigure_BadOrAbsentSubcommand(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"absent", nil},
		{"bad", []string{"bogus"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			var err error
			stderr := captureStderr(t, func() { err = runConfigure(tt.args) })
			if err == nil || !strings.Contains(err.Error(), configureUsage) {
				t.Fatalf("err = %v, want %q", err, configureUsage)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
			assertNoSettingsFile(t)
		})
	}
}

func TestRunConfigure_SurplusPositionalPerVerb(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		wantUsage string
	}{
		{"set plus surplus positional", []string{"set", "--project", "PROJ", "extra"}, configureSetUsage},
		{"get plus surplus positional", []string{"get", "extra"}, configureGetUsage},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			var err error
			stderr := captureStderr(t, func() { err = runConfigure(tt.args) })
			if err == nil || !strings.Contains(err.Error(), tt.wantUsage) {
				t.Fatalf("err = %v, want %q", err, tt.wantUsage)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
			assertNoSettingsFile(t)
		})
	}
}

func TestRunConfigure_HelpToken(t *testing.T) {
	for _, tt := range []struct {
		args       []string
		wantPrefix string
	}{
		{[]string{"--help"}, "usage: jirahere configure set"},
		{[]string{"-h"}, "usage: jirahere configure set"},
		{[]string{"set", "--help"}, "usage: jirahere configure set"},
		{[]string{"get", "--help"}, "usage: jirahere configure get"},
		{[]string{"get", "-h"}, "usage: jirahere configure get"},
	} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		var err error
		out := captureStdout(t, func() { err = runConfigure(tt.args) })
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("runConfigure(%v) err = %v, want flag.ErrHelp", tt.args, err)
		}
		if !strings.Contains(out, tt.wantPrefix) {
			t.Errorf("runConfigure(%v) help stdout = %q, want prefix %q", tt.args, out, tt.wantPrefix)
		}
		assertNoSettingsFile(t)
	}
}

func TestRunConfigureSet_NoFlags(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runConfigureSet(nil) })
	})
	if err == nil || !strings.Contains(err.Error(), configureSetUsage) {
		t.Fatalf("err = %v, want %q", err, configureSetUsage)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one body-free error line", stderr)
	}
	assertNoSettingsFile(t)
}

func TestRunConfigureSet_EmptyValueRejectedPerFlag(t *testing.T) {
	for _, tt := range []struct {
		flag string
	}{
		{"project"},
		{"type"},
		{"current-quarter"},
		{"previous-quarter"},
		{"next-quarter"},
	} {
		t.Run(tt.flag, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			writeSettingsFile(t, `{"defaults":{"project":"OLD","issue_type":"OLD","current_quarter":"OLD","previous_quarter":"OLD","next_quarter":"OLD"}}`)
			before := readSettingsFile(t)

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runConfigureSet([]string{"--" + tt.flag, ""}) })
			})
			if err == nil || !strings.Contains(err.Error(), "--"+tt.flag) || !strings.Contains(err.Error(), "empty") {
				t.Fatalf("err = %v, want a body-free error naming --%s as empty", err, tt.flag)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
			if after := readSettingsFile(t); after != before {
				t.Errorf("settings.json changed: before %q, after %q", before, after)
			}
		})
	}
}

func TestRunConfigureSet_EachFlagAlone(t *testing.T) {
	for _, tt := range []struct {
		name       string
		args       []string
		wantFields settings.Defaults
		wantLine   string
	}{
		{
			name:       "project",
			args:       []string{"--project", "NEWPROJ"},
			wantFields: settings.Defaults{Project: "NEWPROJ", IssueType: "OLD", CurrentQuarter: "OLD", PreviousQuarter: "OLD", NextQuarter: "OLD"},
			wantLine:   "defaults.project set to NEWPROJ.",
		},
		{
			name:       "type",
			args:       []string{"--type", "Bug"},
			wantFields: settings.Defaults{Project: "OLD", IssueType: "Bug", CurrentQuarter: "OLD", PreviousQuarter: "OLD", NextQuarter: "OLD"},
			wantLine:   "defaults.issue_type set to Bug.",
		},
		{
			name:       "current-quarter",
			args:       []string{"--current-quarter", "Q3'26"},
			wantFields: settings.Defaults{Project: "OLD", IssueType: "OLD", CurrentQuarter: "Q3'26", PreviousQuarter: "OLD", NextQuarter: "OLD"},
			wantLine:   "defaults.current_quarter set to Q3'26.",
		},
		{
			name:       "previous-quarter",
			args:       []string{"--previous-quarter", "Q2'26"},
			wantFields: settings.Defaults{Project: "OLD", IssueType: "OLD", CurrentQuarter: "OLD", PreviousQuarter: "Q2'26", NextQuarter: "OLD"},
			wantLine:   "defaults.previous_quarter set to Q2'26.",
		},
		{
			name:       "next-quarter",
			args:       []string{"--next-quarter", "Q4'26"},
			wantFields: settings.Defaults{Project: "OLD", IssueType: "OLD", CurrentQuarter: "OLD", PreviousQuarter: "OLD", NextQuarter: "Q4'26"},
			wantLine:   "defaults.next_quarter set to Q4'26.",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			writeSettingsFile(t, `{"defaults":{"project":"OLD","issue_type":"OLD","current_quarter":"OLD","previous_quarter":"OLD","next_quarter":"OLD"}}`)

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runConfigureSet(tt.args) })
			})
			if err != nil {
				t.Fatalf("runConfigureSet(%v): %v", tt.args, err)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty on success", stderr)
			}
			if strings.TrimRight(out, "\n") != tt.wantLine {
				t.Errorf("stdout = %q, want %q", out, tt.wantLine)
			}

			s, lerr := settings.Load("")
			if lerr != nil {
				t.Fatalf("settings.Load: %v", lerr)
			}
			if s.Defaults != tt.wantFields {
				t.Errorf("Defaults = %+v, want %+v", s.Defaults, tt.wantFields)
			}
		})
	}
}

func TestRunConfigureSet_MultipleFlagsTogether(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			err = runConfigureSet([]string{"--next-quarter", "Q4'26", "--project", "PROJ", "--type", "Task"})
		})
	})
	if err != nil {
		t.Fatalf("runConfigureSet: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty on success", stderr)
	}
	wantLines := []string{
		"defaults.next_quarter set to Q4'26.",
		"defaults.project set to PROJ.",
		"defaults.issue_type set to Task.",
	}
	if out != strings.Join(wantLines, "\n")+"\n" {
		t.Errorf("stdout = %q, want %q in flag-name order", out, strings.Join(wantLines, "\n")+"\n")
	}

	s, lerr := settings.Load("")
	if lerr != nil {
		t.Fatalf("settings.Load: %v", lerr)
	}
	want := settings.Defaults{Project: "PROJ", IssueType: "Task", NextQuarter: "Q4'26"}
	if s.Defaults != want {
		t.Errorf("Defaults = %+v, want %+v", s.Defaults, want)
	}
}

func TestRunConfigureSet_PreExistingFieldsNotNamedSurviveUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","issue_type":"Task","current_quarter":"Q1'26","previous_quarter":"Q4'25","next_quarter":"Q2'26"}}`)

	if _, err := runConfigureSetCapture(t, []string{"--current-quarter", "Q3'26"}); err != nil {
		t.Fatalf("runConfigureSet: %v", err)
	}

	s, err := settings.Load("")
	if err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	want := settings.Defaults{
		Project:         "PROJ",
		IssueType:       "Task",
		CurrentQuarter:  "Q3'26",
		PreviousQuarter: "Q4'25",
		NextQuarter:     "Q2'26",
	}
	if s.Defaults != want {
		t.Errorf("Defaults = %+v, want %+v (only current_quarter changed)", s.Defaults, want)
	}
}

func TestRunConfigureSet_SaveFailureLeavesFileUnmodifiedAndExitsNonzero(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits do not block traversal")
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	writeSettingsFile(t, `{"defaults":{"project":"ORIGINAL"}}`)
	before := readSettingsFile(t)

	if err := os.Chmod(home, 0o600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runConfigureSet([]string{"--project", "REPLACEMENT"}) })
	})

	if rerr := os.Chmod(home, 0o700); rerr != nil {
		t.Fatalf("Chmod restore: %v", rerr)
	}

	if err == nil {
		t.Fatal("runConfigureSet with an inaccessible config dir: expected an error, got nil")
	}
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Errorf("err = %v, want an *errSilent (already printed to stderr)", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty on a Save failure", out)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one sanitized error line", stderr)
	}

	after := readSettingsFile(t)
	if after != before {
		t.Errorf("settings.json changed after a failed Save: before %q, after %q", before, after)
	}
}

const legacyQuarterLabel = "FY26-Q3"

func TestRunConfigureSet_QuarterLabelFormat(t *testing.T) {
	flags := []string{"current-quarter", "previous-quarter", "next-quarter"}
	for _, fl := range flags {
		t.Run(fl+"/valid", func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if _, err := runConfigureSetCapture(t, []string{"--" + fl, "Q3'26"}); err != nil {
				t.Fatalf("runConfigureSet: %v", err)
			}
		})
		for _, bad := range []string{legacyQuarterLabel, "Q5'26", "Q3'2026", "q3'26", "Q3'26 ", "Q0'26"} {
			t.Run(fl+"/"+bad, func(t *testing.T) {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				var err error
				stderr := captureStderr(t, func() { err = runConfigureSet([]string{"--" + fl, bad}) })
				var usageErr *errUsage
				if !errors.As(err, &usageErr) {
					t.Fatalf("err = %v, want *errUsage (exit 2)", err)
				}
				for _, want := range []string{"--" + fl, fmt.Sprintf("%q", bad), "QQ'YY", "Q3'26"} {
					if !strings.Contains(stderr, want) {
						t.Errorf("stderr = %q, missing %q", stderr, want)
					}
				}
				assertNoSettingsFile(t)
			})
		}
	}
}

func TestRunConfigureSet_QuarterLabelInvalidWritesNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const raw = `{"defaults":{"project":"OLD","current_quarter":"Q1'26"}}`
	writeSettingsFile(t, raw)

	var err error
	_ = captureStderr(t, func() {
		err = runConfigureSet([]string{"--project", "NEW", "--current-quarter", "Q3'26", "--next-quarter", legacyQuarterLabel})
	})
	if err == nil {
		t.Fatal("err = nil, want usage error")
	}
	s, lerr := settings.Load("")
	if lerr != nil {
		t.Fatalf("settings.Load: %v", lerr)
	}
	want := settings.Defaults{Project: "OLD", CurrentQuarter: "Q1'26"}
	if s.Defaults != want {
		t.Errorf("Defaults = %+v, want %+v (nothing written)", s.Defaults, want)
	}
}

func TestRunConfigureGet_SurplusPositional(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runConfigureGet([]string{"extra"}) })
	})
	if err == nil || !strings.Contains(err.Error(), configureGetUsage) {
		t.Fatalf("err = %v, want %q", err, configureGetUsage)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one body-free error line", stderr)
	}
}

func TestRunConfigureGet_Text(t *testing.T) {
	for _, tt := range []struct {
		name    string
		raw     string
		wantOut string
	}{
		{
			name: "all unset",
			raw:  "",
			wantOut: "project: (unset)\n" +
				"issue_type: (unset)\n" +
				"current_quarter: (unset)\n" +
				"previous_quarter: (unset)\n" +
				"next_quarter: (unset)\n",
		},
		{
			name: "some set",
			raw:  `{"defaults":{"project":"PROJ","current_quarter":"Q3'26"}}`,
			wantOut: "project: PROJ\n" +
				"issue_type: (unset)\n" +
				"current_quarter: Q3'26\n" +
				"previous_quarter: (unset)\n" +
				"next_quarter: (unset)\n",
		},
		{
			name: "all set",
			raw:  `{"defaults":{"project":"PROJ","issue_type":"Task","current_quarter":"Q3'26","previous_quarter":"Q2'26","next_quarter":"Q4'26"}}`,
			wantOut: "project: PROJ\n" +
				"issue_type: Task\n" +
				"current_quarter: Q3'26\n" +
				"previous_quarter: Q2'26\n" +
				"next_quarter: Q4'26\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if tt.raw != "" {
				writeSettingsFile(t, tt.raw)
			}

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runConfigureGet(nil) })
			})
			if err != nil {
				t.Fatalf("runConfigureGet: %v", err)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty on success", stderr)
			}
			if out != tt.wantOut {
				t.Errorf("stdout = %q, want %q", out, tt.wantOut)
			}
		})
	}
}

func TestRunConfigureGet_JSON(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		want configureGetDoc
	}{
		{
			name: "all unset",
			raw:  "",
			want: configureGetDoc{},
		},
		{
			name: "some set",
			raw:  `{"defaults":{"project":"PROJ","current_quarter":"Q3'26"}}`,
			want: configureGetDoc{Project: strPtr("PROJ"), CurrentQuarter: strPtr("Q3'26")},
		},
		{
			name: "all set",
			raw:  `{"defaults":{"project":"PROJ","issue_type":"Task","current_quarter":"Q3'26","previous_quarter":"Q2'26","next_quarter":"Q4'26"}}`,
			want: configureGetDoc{
				Project:         strPtr("PROJ"),
				IssueType:       strPtr("Task"),
				CurrentQuarter:  strPtr("Q3'26"),
				PreviousQuarter: strPtr("Q2'26"),
				NextQuarter:     strPtr("Q4'26"),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if tt.raw != "" {
				writeSettingsFile(t, tt.raw)
			}

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runConfigureGet([]string{"--json"}) })
			})
			if err != nil {
				t.Fatalf("runConfigureGet: %v", err)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty on success", stderr)
			}

			var got configureGetDoc
			if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
				t.Fatalf("json.Unmarshal(%q): %v", out, uerr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("decoded = %+v, want %+v (raw stdout %q)", derefDoc(got), derefDoc(tt.want), out)
			}
			if trimmed := strings.TrimRight(out, "\n"); !json.Valid([]byte(trimmed)) {
				t.Errorf("stdout = %q, want exactly one valid JSON document", out)
			}
		})
	}
}

func TestRunConfigureGet_LoadFailure(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"text", nil},
		{"json", []string{"--json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			writeSettingsFile(t, `not valid json`)

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runConfigureGet(tt.args) })
			})

			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %v, want an *errSilent (already printed to stderr)", err)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty on a Load failure", out)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one sanitized error line", stderr)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func derefDoc(d configureGetDoc) [5]string {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return [5]string{deref(d.Project), deref(d.IssueType), deref(d.CurrentQuarter), deref(d.PreviousQuarter), deref(d.NextQuarter)}
}

func runConfigureSetCapture(t *testing.T, args []string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() { err = runConfigureSet(args) })
	})
	return out, err
}

func assertNoSettingsFile(t *testing.T) {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	path := filepath.Join(dir, "jirahere", "settings.json")
	if _, statErr := os.Stat(path); statErr == nil {
		t.Errorf("settings.json unexpectedly exists at %s", path)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("Stat(%s): %v", path, statErr)
	}
}

func readSettingsFile(t *testing.T) string {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "jirahere", "settings.json"))
	if err != nil {
		t.Fatalf("ReadFile settings.json: %v", err)
	}
	return string(data)
}
