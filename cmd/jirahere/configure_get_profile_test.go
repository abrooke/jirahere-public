package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfileSettings(t *testing.T, prof, body string) {
	t.Helper()
	path := profileSettingsPath(t, prof)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runConfigureGetCapture(t *testing.T, args []string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runConfigureGet(args) })
	})
	return stdout, stderr, err
}

const configureGetAllUnsetText = "project: (unset)\nissue_type: (unset)\ncurrent_quarter: (unset)\nprevious_quarter: (unset)\nnext_quarter: (unset)\n"

func TestRunConfigureGet_Profile_Isolated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ","issue_type":"Bug","next_quarter":"DEF-NEXT"}}`)
	writeProfileSettings(t, "other", `{"defaults":{"project":"OTHERPROJ","previous_quarter":"OTHER-PREV"}}`)
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ","current_quarter":"FY26-Q3"}}`)

	out, stderr, err := runConfigureGetCapture(t, []string{"--profile", "work"})
	if err != nil {
		t.Fatalf("runConfigureGet: %v", err)
	}
	wantText := "project: WORKPROJ\nissue_type: (unset)\ncurrent_quarter: FY26-Q3\nprevious_quarter: (unset)\nnext_quarter: (unset)\n"
	if out != wantText || stderr != "" {
		t.Errorf("text stdout = %q stderr = %q, want %q and empty stderr", out, stderr, wantText)
	}

	out, stderr, err = runConfigureGetCapture(t, []string{"--json", "--profile", "work"})
	if err != nil {
		t.Fatalf("runConfigureGet --json: %v", err)
	}
	wantJSON := "{\n  \"project\": \"WORKPROJ\",\n  \"issue_type\": null,\n  \"current_quarter\": \"FY26-Q3\",\n  \"previous_quarter\": null,\n  \"next_quarter\": null\n}\n"
	if out != wantJSON || stderr != "" {
		t.Errorf("json stdout = %q stderr = %q, want %q and empty stderr", out, stderr, wantJSON)
	}

	out, _, err = runConfigureGetCapture(t, []string{"--profile", "other"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "project: OTHERPROJ\nissue_type: (unset)\ncurrent_quarter: (unset)\nprevious_quarter: OTHER-PREV\nnext_quarter: (unset)\n"; out != want {
		t.Errorf("other stdout = %q, want %q", out, want)
	}
	out, _, err = runConfigureGetCapture(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "project: DEFPROJ\nissue_type: Bug\ncurrent_quarter: (unset)\nprevious_quarter: (unset)\nnext_quarter: DEF-NEXT\n"; out != want {
		t.Errorf("default stdout = %q, want %q", out, want)
	}
}

func TestRunConfigureGet_Profile_NoSettingsIsAllUnset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ"}}`)
	writeProfileSettings(t, "emptyfile", `{}`)

	for _, prof := range []string{"missing", "emptyfile"} {
		out, stderr, err := runConfigureGetCapture(t, []string{"--profile", prof})
		if err != nil {
			t.Fatalf("profile %s: %v", prof, err)
		}
		if out != configureGetAllUnsetText || stderr != "" {
			t.Errorf("profile %s: stdout = %q stderr = %q, want all-unset and empty stderr", prof, out, stderr)
		}
	}

	out, _, err := runConfigureGetCapture(t, []string{"--json", "--profile", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"project\": null,\n  \"issue_type\": null,\n  \"current_quarter\": null,\n  \"previous_quarter\": null,\n  \"next_quarter\": null\n}\n"
	if out != want {
		t.Errorf("json stdout = %q, want %q", out, want)
	}
	if _, statErr := os.Stat(filepath.Dir(profileSettingsPath(t, "missing"))); !os.IsNotExist(statErr) {
		t.Errorf("configure get created profile missing (stat err = %v)", statErr)
	}
}

func TestRunConfigureGet_Profile_InvalidOrEmptyName(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"empty", []string{"--profile", ""}, "jirahere: --profile was given but is empty; a profile name must not be empty.\n"},
		{"empty json", []string{"--json", "--profile", ""}, "jirahere: --profile was given but is empty; a profile name must not be empty.\n"},
		{"dotdot", []string{"--profile", ".."}, "jirahere: invalid --profile: profile name must not be \"..\".\n"},
		{"path separator", []string{"--profile", "a/b"}, ""},
		{"leading dot", []string{"--json", "--profile", ".hidden"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", home)

			out, stderr, err := runConfigureGetCapture(t, tt.args)
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

func TestRunConfigureGet_ProfileAbsent_OutputUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"PROJ"}}`)
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ"}}`)

	out, _, err := runConfigureGetCapture(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "project: PROJ\nissue_type: (unset)\ncurrent_quarter: (unset)\nprevious_quarter: (unset)\nnext_quarter: (unset)\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestRunConfigureGet_Profile_LoadFailureIsScoped(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"PROJ"}}`)
	writeProfileSettings(t, "bad", `{not json`)

	out, stderr, err := runConfigureGetCapture(t, []string{"--profile", "bad"})
	if err == nil {
		t.Fatal("expected an error for malformed profile settings")
	}
	if out != "" || !strings.HasPrefix(stderr, "jirahere: could not load settings: ") || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stdout = %q stderr = %q, want empty stdout and one load-failure line", out, stderr)
	}
	if _, _, err := runConfigureGetCapture(t, nil); err != nil {
		t.Errorf("default profile read failed: %v", err)
	}
}

func TestRunConfigureGet_ProfileNamedDefault_IsOrdinaryProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"NOFLAGPROJ"}}`)

	out, _, err := runConfigureGetCapture(t, []string{"--profile", "default"})
	if err != nil {
		t.Fatal(err)
	}
	if out != configureGetAllUnsetText {
		t.Errorf("stdout = %q, want all-unset (must not fall back to the no-flag default)", out)
	}

	writeProfileSettings(t, "default", `{"defaults":{"project":"NAMEDDEFAULT"}}`)
	out, _, err = runConfigureGetCapture(t, []string{"--profile", "default"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "project: NAMEDDEFAULT\nissue_type: (unset)\ncurrent_quarter: (unset)\nprevious_quarter: (unset)\nnext_quarter: (unset)\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestRunConfigureGet_HelpListsProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var err error
	out := captureStdout(t, func() { err = runConfigure([]string{"get", "--help"}) })
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out, "--profile") || !strings.Contains(out, "run against the named") {
		t.Errorf("help stdout = %q, want a --profile entry", out)
	}
}
