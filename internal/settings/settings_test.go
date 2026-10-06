package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

func withConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func writeSettingsFile(t *testing.T, raw string) {
	t.Helper()
	dir, err := auth.ConfigDir("")
	if err != nil {
		t.Fatalf("auth.ConfigDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_NoFile(t *testing.T) {
	withConfigHome(t)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v, want nil error", err)
	}
	if got.Defaults.Project != "" {
		t.Errorf("Defaults.Project = %q, want empty string when no settings.json exists", got.Defaults.Project)
	}
	if got.Defaults.IssueType != "" {
		t.Errorf("Defaults.IssueType = %q, want empty string when no settings.json exists", got.Defaults.IssueType)
	}
}

func TestLoad_NoDefaultsObject(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v, want nil error", err)
	}
	if got.Defaults.Project != "" {
		t.Errorf("Defaults.Project = %q, want empty string for settings.json with no defaults object", got.Defaults.Project)
	}
	if got.Defaults.IssueType != "" {
		t.Errorf("Defaults.IssueType = %q, want empty string for settings.json with no defaults object", got.Defaults.IssueType)
	}
}

func TestLoad_DefaultsPresentProjectAbsent(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v, want nil error", err)
	}
	if got.Defaults.Project != "" {
		t.Errorf("Defaults.Project = %q, want empty string for a defaults object with no project key", got.Defaults.Project)
	}
	if got.Defaults.IssueType != "" {
		t.Errorf("Defaults.IssueType = %q, want empty string for a defaults object with no issue_type key", got.Defaults.IssueType)
	}
}

func TestLoad_DefaultsProject(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ"}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Defaults.Project != "PROJ" {
		t.Errorf("Defaults.Project = %q, want %q", got.Defaults.Project, "PROJ")
	}
}

func TestLoad_DefaultsIssueType(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"issue_type":"Task"}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Defaults.IssueType != "Task" {
		t.Errorf("Defaults.IssueType = %q, want %q", got.Defaults.IssueType, "Task")
	}
}

func TestLoad_DefaultsCurrentQuarter(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"current_quarter":"FY26-Q1"}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Defaults.CurrentQuarter != "FY26-Q1" {
		t.Errorf("Defaults.CurrentQuarter = %q, want %q", got.Defaults.CurrentQuarter, "FY26-Q1")
	}
}

func TestLoad_DefaultsPreviousNextQuarterAbsent(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"current_quarter":"FY26-Q1"}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Defaults.PreviousQuarter != "" {
		t.Errorf("Defaults.PreviousQuarter = %q, want empty string when previous_quarter is absent", got.Defaults.PreviousQuarter)
	}
	if got.Defaults.NextQuarter != "" {
		t.Errorf("Defaults.NextQuarter = %q, want empty string when next_quarter is absent", got.Defaults.NextQuarter)
	}
}

func TestLoad_DefaultsPreviousQuarter(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"previous_quarter":"FY25-Q4"}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Defaults.PreviousQuarter != "FY25-Q4" {
		t.Errorf("Defaults.PreviousQuarter = %q, want %q", got.Defaults.PreviousQuarter, "FY25-Q4")
	}
	if got.Defaults.NextQuarter != "" {
		t.Errorf("Defaults.NextQuarter = %q, want empty string when next_quarter is absent", got.Defaults.NextQuarter)
	}
}

func TestLoad_DefaultsNextQuarter(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"next_quarter":"FY26-Q2"}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Defaults.NextQuarter != "FY26-Q2" {
		t.Errorf("Defaults.NextQuarter = %q, want %q", got.Defaults.NextQuarter, "FY26-Q2")
	}
	if got.Defaults.PreviousQuarter != "" {
		t.Errorf("Defaults.PreviousQuarter = %q, want empty string when previous_quarter is absent", got.Defaults.PreviousQuarter)
	}
}

func TestSaveLoadRoundTrip_EmptyDefaults(t *testing.T) {
	withConfigHome(t)

	want := &Settings{}
	if err := Save(want, ""); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *got != *want {
		t.Fatalf("Load returned %+v, want %+v", got, want)
	}
}

func TestSaveLoadRoundTrip_FullyPopulatedDefaults(t *testing.T) {
	withConfigHome(t)

	want := &Settings{
		Defaults: Defaults{
			Project:         "PROJ",
			IssueType:       "Task",
			CurrentQuarter:  "FY26-Q1",
			PreviousQuarter: "FY25-Q4",
			NextQuarter:     "FY26-Q2",
		},
	}
	if err := Save(want, ""); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *got != *want {
		t.Fatalf("Load returned %+v, want %+v", got, want)
	}
}

func TestSave_Overwrite(t *testing.T) {
	withConfigHome(t)

	first := &Settings{Defaults: Defaults{Project: "FIRST"}}
	if err := Save(first, ""); err != nil {
		t.Fatalf("Save(first): %v", err)
	}
	second := &Settings{Defaults: Defaults{Project: "SECOND", IssueType: "Bug"}}
	if err := Save(second, ""); err != nil {
		t.Fatalf("Save(second): %v", err)
	}

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *got != *second {
		t.Fatalf("Load returned %+v, want the second settings %+v", got, second)
	}
}

func TestSave_Permissions(t *testing.T) {
	dir := withConfigHome(t)

	if err := Save(&Settings{Defaults: Defaults{Project: "PROJ"}}, ""); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path, err := settingsPath("")
	if err != nil {
		t.Fatalf("settingsPath: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat settings file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("settings file mode = %o, want 0600", perm)
	}

	configDir := filepath.Join(dir, "jirahere")
	di, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("Stat config dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir mode = %o, want 0700", perm)
	}
}

func TestSave_NoLeftoverTempFile(t *testing.T) {
	dir := withConfigHome(t)

	if err := Save(&Settings{Defaults: Defaults{Project: "PROJ"}}, ""); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "jirahere"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("config dir contains %v, want exactly [settings.json]", names)
	}
}

func TestSave_WriteFailureLeavesExistingFileUntouched(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits do not block traversal")
	}
	home := withConfigHome(t)

	if err := Save(&Settings{Defaults: Defaults{Project: "ORIGINAL"}}, ""); err != nil {
		t.Fatalf("Save(original): %v", err)
	}
	path, err := settingsPath("")
	if err != nil {
		t.Fatalf("settingsPath: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile before failed Save: %v", err)
	}

	if err := os.Chmod(home, 0o600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })

	saveErr := Save(&Settings{Defaults: Defaults{Project: "REPLACEMENT"}}, "")

	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatalf("Chmod restore: %v", err)
	}
	if saveErr == nil {
		t.Fatal("Save with an inaccessible config dir parent: expected an error, got nil")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after failed Save: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("settings.json changed after a failed Save: before %q, after %q", before, after)
	}

	configDir := filepath.Join(home, "jirahere")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("config dir contains %v after a failed Save, want exactly [settings.json]", names)
	}
}

func TestLoad_DefaultsPreviousAndNextQuarterTogether(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","issue_type":"Task","current_quarter":"FY26-Q1","previous_quarter":"FY25-Q4","next_quarter":"FY26-Q2"}}`)

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Defaults.Project != "PROJ" {
		t.Errorf("Defaults.Project = %q, want %q", got.Defaults.Project, "PROJ")
	}
	if got.Defaults.IssueType != "Task" {
		t.Errorf("Defaults.IssueType = %q, want %q", got.Defaults.IssueType, "Task")
	}
	if got.Defaults.CurrentQuarter != "FY26-Q1" {
		t.Errorf("Defaults.CurrentQuarter = %q, want %q", got.Defaults.CurrentQuarter, "FY26-Q1")
	}
	if got.Defaults.PreviousQuarter != "FY25-Q4" {
		t.Errorf("Defaults.PreviousQuarter = %q, want %q", got.Defaults.PreviousQuarter, "FY25-Q4")
	}
	if got.Defaults.NextQuarter != "FY26-Q2" {
		t.Errorf("Defaults.NextQuarter = %q, want %q", got.Defaults.NextQuarter, "FY26-Q2")
	}
}

func writeProfileSettingsFile(t *testing.T, prof, raw string) {
	t.Helper()
	dir, err := auth.ConfigDir(prof)
	if err != nil {
		t.Fatalf("auth.ConfigDir(%q): %v", prof, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_Profile_ReadsOnlyItsOwnFile(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"project":"DEFAULTPROJ"}}`)
	writeProfileSettingsFile(t, "work", `{"defaults":{"project":"WORK","issue_type":"Task"}}`)

	got, err := Load("work")
	if err != nil {
		t.Fatalf("Load(work): %v", err)
	}
	want := Defaults{Project: "WORK", IssueType: "Task"}
	if got.Defaults != want {
		t.Errorf("Load(work).Defaults = %+v, want %+v", got.Defaults, want)
	}
}

func TestLoad_Profile_MissingFileAndDir(t *testing.T) {
	withConfigHome(t)
	writeSettingsFile(t, `{"defaults":{"project":"DEFAULTPROJ"}}`)

	got, err := Load("ghost")
	if err != nil {
		t.Fatalf("Load(ghost) without dir: %v", err)
	}
	if got == nil || *got != (Settings{}) {
		t.Errorf("Load(ghost) without dir = %+v, want zero-value Settings", got)
	}

	dir, err := auth.ConfigDir("ghost")
	if err != nil {
		t.Fatalf("auth.ConfigDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err = Load("ghost")
	if err != nil {
		t.Fatalf("Load(ghost) with empty dir: %v", err)
	}
	if got == nil || *got != (Settings{}) {
		t.Errorf("Load(ghost) with empty dir = %+v, want zero-value Settings", got)
	}
}

func TestSave_Profile_WritesOnlyItsOwnFile(t *testing.T) {
	home := withConfigHome(t)

	if err := Save(&Settings{Defaults: Defaults{Project: "WORK"}}, "work"); err != nil {
		t.Fatalf("Save(work): %v", err)
	}

	path, err := settingsPath("work")
	if err != nil {
		t.Fatalf("settingsPath: %v", err)
	}
	if want := filepath.Join(home, "jirahere-profiles", "work", "settings.json"); path != want {
		t.Errorf("settingsPath(work) = %q, want %q", path, want)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings.json: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("settings.json perms = %o, want 600", got)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat profile dir: %v", err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("profile dir perms = %o, want 700", got)
	}
	pi, err := os.Stat(filepath.Join(home, "jirahere-profiles"))
	if err != nil {
		t.Fatalf("stat jirahere-profiles dir: %v", err)
	}
	if got := pi.Mode().Perm(); got != 0o700 {
		t.Errorf("jirahere-profiles dir perms = %o, want 700", got)
	}
	if _, err := os.Stat(filepath.Join(home, "jirahere")); !os.IsNotExist(err) {
		t.Errorf("default config dir was touched by a profile Save: err=%v", err)
	}
}

func TestProfiles_AreIsolated(t *testing.T) {
	withConfigHome(t)

	for prof, proj := range map[string]string{"": "DEF", "a": "AAA", "b": "BBB"} {
		if err := Save(&Settings{Defaults: Defaults{Project: proj}}, prof); err != nil {
			t.Fatalf("Save(%q): %v", prof, err)
		}
	}
	for prof, proj := range map[string]string{"": "DEF", "a": "AAA", "b": "BBB"} {
		got, err := Load(prof)
		if err != nil {
			t.Fatalf("Load(%q): %v", prof, err)
		}
		if got.Defaults.Project != proj {
			t.Errorf("Load(%q).Project = %q, want %q", prof, got.Defaults.Project, proj)
		}
	}

	if err := Save(&Settings{Defaults: Defaults{Project: "AAA2"}}, "a"); err != nil {
		t.Fatalf("Save(a): %v", err)
	}
	for prof, proj := range map[string]string{"": "DEF", "a": "AAA2", "b": "BBB"} {
		got, err := Load(prof)
		if err != nil {
			t.Fatalf("Load(%q): %v", prof, err)
		}
		if got.Defaults.Project != proj {
			t.Errorf("after overwrite Load(%q).Project = %q, want %q", prof, got.Defaults.Project, proj)
		}
	}
}

func TestInvalidProfile_ErrorsBeforeFilesystemAccess(t *testing.T) {
	home := withConfigHome(t)

	for _, bad := range []string{"../evil", "a/b", ".", "..", "has space", "-lead"} {
		if _, err := Load(bad); err == nil {
			t.Errorf("Load(%q): want error, got nil", bad)
		}
		if err := Save(&Settings{Defaults: Defaults{Project: "X"}}, bad); err == nil {
			t.Errorf("Save(%q): want error, got nil", bad)
		}
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("config home not empty after invalid-profile calls: %v", entries)
	}
}
