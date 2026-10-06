package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

func profileSettingsPath(t *testing.T, prof string) string {
	t.Helper()
	dir, err := auth.ConfigDir(prof)
	if err != nil {
		t.Fatalf("auth.ConfigDir(%q): %v", prof, err)
	}
	return filepath.Join(dir, "settings.json")
}

func TestRunConfigureSet_Profile_IsolatedAndCreatedOnFirstWrite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ"}}`)
	defaultBefore := readSettingsFile(t)

	otherPath := profileSettingsPath(t, "other")
	if err := os.MkdirAll(filepath.Dir(otherPath), 0o700); err != nil {
		t.Fatal(err)
	}
	const otherBody = `{"defaults":{"project":"OTHERPROJ"}}`
	if err := os.WriteFile(otherPath, []byte(otherBody), 0o600); err != nil {
		t.Fatal(err)
	}

	workPath := profileSettingsPath(t, "work")
	if _, err := os.Stat(workPath); !os.IsNotExist(err) {
		t.Fatalf("profile work settings.json exists before first write (stat err = %v)", err)
	}

	var err error
	out := captureStdout(t, func() {
		err = runConfigureSet([]string{"--profile", "work", "--project", "WORKPROJ", "--type", "Task"})
	})
	if err != nil {
		t.Fatalf("runConfigureSet: %v", err)
	}
	want := "defaults.project set to WORKPROJ (profile \"work\").\ndefaults.issue_type set to Task (profile \"work\").\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}

	s, err := settings.Load("work")
	if err != nil {
		t.Fatal(err)
	}
	if s.Defaults.Project != "WORKPROJ" || s.Defaults.IssueType != "Task" {
		t.Errorf("work defaults = %+v, want project WORKPROJ, type Task", s.Defaults)
	}
	if got := readSettingsFile(t); got != defaultBefore {
		t.Errorf("default settings.json changed: before %q, after %q", defaultBefore, got)
	}
	if got, _ := os.ReadFile(otherPath); string(got) != otherBody {
		t.Errorf("other profile settings.json changed: %q", got)
	}

	captureStdout(t, func() {
		err = runConfigureSet([]string{"--profile", "work", "--current-quarter", "FY26-Q3"})
	})
	if err != nil {
		t.Fatalf("second runConfigureSet: %v", err)
	}
	s, _ = settings.Load("work")
	if s.Defaults.Project != "WORKPROJ" || s.Defaults.CurrentQuarter != "FY26-Q3" {
		t.Errorf("work defaults after second write = %+v", s.Defaults)
	}
}

func TestRunConfigureSet_Profile_DoesNotSeedFromDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ","issue_type":"Bug"}}`)

	captureStdout(t, func() {
		if err := runConfigureSet([]string{"--profile", "work", "--next-quarter", "FY26-Q4"}); err != nil {
			t.Fatal(err)
		}
	})
	s, err := settings.Load("work")
	if err != nil {
		t.Fatal(err)
	}
	want := settings.Defaults{NextQuarter: "FY26-Q4"}
	if s.Defaults != want {
		t.Errorf("work defaults = %+v, want %+v", s.Defaults, want)
	}
}

func TestRunConfigureSet_Profile_AloneIsNoFlagsUsageError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runConfigureSet([]string{"--profile", "work"}) })
	})
	if err == nil || !strings.Contains(err.Error(), configureSetUsage) {
		t.Fatalf("err = %v, want %q", err, configureSetUsage)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if stderr != configureSetUsage+"\n" {
		t.Errorf("stderr = %q, want exactly the usage line", stderr)
	}
	if _, statErr := os.Stat(profileSettingsPath(t, "work")); !os.IsNotExist(statErr) {
		t.Errorf("profile work settings.json created by --profile alone (stat err = %v)", statErr)
	}
	assertNoSettingsFile(t)
}

func TestRunConfigureSet_Profile_InvalidOrEmptyName(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"empty", []string{"--profile", "", "--project", "P"}, "jirahere: --profile was given but is empty; a profile name must not be empty.\n"},
		{"empty alone", []string{"--profile", ""}, "jirahere: --profile was given but is empty; a profile name must not be empty.\n"},
		{"dotdot", []string{"--profile", "..", "--project", "P"}, "jirahere: invalid --profile: profile name must not be \"..\".\n"},
		{"path separator", []string{"--profile", "a/b", "--project", "P"}, ""},
		{"leading dot", []string{"--profile", ".hidden", "--project", "P"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", home)

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runConfigureSet(tt.args) })
			})
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Errorf("err = %v, want *errSilent", err)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "--profile") {
				t.Errorf("stderr = %q, want one body-free line naming --profile", stderr)
			}
			if tt.want != "" && stderr != tt.want {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
			if strings.Contains(stderr, "a/b") || strings.Contains(stderr, ".hidden") {
				t.Errorf("stderr echoes the profile name: %q", stderr)
			}
			entries, _ := os.ReadDir(home)
			if len(entries) != 0 {
				t.Errorf("config home not empty after rejected name: %v", entries)
			}
		})
	}
}

func TestRunConfigureSet_ProfileAbsent_OutputUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	var err error
	out := captureStdout(t, func() { err = runConfigureSet([]string{"--project", "PROJ"}) })
	if err != nil {
		t.Fatal(err)
	}
	if out != "defaults.project set to PROJ.\n" {
		t.Errorf("stdout = %q, want the pre-profile line", out)
	}
	if got := readSettingsFile(t); got != "{\n  \"defaults\": {\n    \"project\": \"PROJ\"\n  }\n}\n" {
		t.Errorf("settings.json = %q", got)
	}
	if _, statErr := os.Stat(filepath.Join(home, "jirahere-profiles")); !os.IsNotExist(statErr) {
		t.Errorf("profiles dir created without --profile (stat err = %v)", statErr)
	}
}

func TestRunConfigureSet_HelpListsProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var err error
	out := captureStdout(t, func() { err = runConfigure([]string{"set", "--help"}) })
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out, "--profile") || !strings.Contains(out, "run against the named") {
		t.Errorf("help stdout = %q, want a --profile entry", out)
	}
}
