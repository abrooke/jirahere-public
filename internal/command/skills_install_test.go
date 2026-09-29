package command

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
	"github.com/aslanbrooke/jirahere/internal/skills"
)

func TestInstallSkills_WritesReplacesPrunesAndPreservesUnrelatedEntries(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nested", "skills")
	if err := os.MkdirAll(filepath.Join(target, "jirahere-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "jirahere-old", "old.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "jirahere-alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "jirahere-alpha", "old.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "user-skill.md"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "jirahere-beta", Files: map[string][]byte{"nested/SKILL.md": []byte("beta")}},
		{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}},
	})
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if !reflect.DeepEqual(result.Written, []string{"jirahere-beta"}) || !reflect.DeepEqual(result.Replaced, []string{"jirahere-alpha"}) || !reflect.DeepEqual(result.Pruned, []string{"jirahere-old"}) {
		t.Errorf("result = %+v", result)
	}
	for filename, want := range map[string]string{
		"jirahere-alpha/SKILL.md":       "alpha",
		"jirahere-beta/nested/SKILL.md": "beta",
		"user-skill.md":                 "keep",
	} {
		contents, readErr := os.ReadFile(filepath.Join(target, filename))
		if readErr != nil || string(contents) != want {
			t.Errorf("%s = %q, %v; want %q", filename, contents, readErr, want)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "jirahere-old")); !os.IsNotExist(err) {
		t.Errorf("stale skill remains; stat error = %v", err)
	}
	for _, filename := range []string{".", "jirahere-alpha", "jirahere-beta", "jirahere-beta/nested"} {
		info, statErr := os.Stat(filepath.Join(target, filename))
		if statErr != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %v, %v; want 0755", filename, info.Mode(), statErr)
		}
	}
	info, err := os.Stat(filepath.Join(target, "jirahere-beta/nested/SKILL.md"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, %v; want 0644", info.Mode(), err)
	}
}

func TestInstallSkills_ReplacesUsingAtomicExchangeAndCleansOwnedTemporaryPaths(t *testing.T) {
	target := t.TempDir()
	destination := filepath.Join(target, "jirahere-alpha")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalExchange := exchangeSkillDirectories
	t.Cleanup(func() { exchangeSkillDirectories = originalExchange })
	var gotStage, gotDestination string
	exchangeSkillDirectories = func(stage, live string) error {
		gotStage, gotDestination = stage, live
		old := stage + "-old"
		if err := os.Rename(live, old); err != nil {
			return err
		}
		if err := os.Rename(stage, live); err != nil {
			return err
		}
		return os.Rename(old, stage)
	}

	result, err := InstallSkills(target, []skills.Skill{{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("new")}}})
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if !reflect.DeepEqual(result.Replaced, []string{"jirahere-alpha"}) {
		t.Errorf("replaced = %v, want jirahere-alpha", result.Replaced)
	}
	if gotDestination != destination || !strings.HasPrefix(filepath.Base(gotStage), "jirahere-stage-") {
		t.Errorf("exchange paths = (%q, %q), want owned stage and destination %q", gotStage, gotDestination, destination)
	}
	contents, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	if err != nil || string(contents) != "new" {
		t.Errorf("replacement contents = %q, %v; want new", contents, err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "jirahere-stage-") || strings.HasPrefix(entry.Name(), "jirahere-previous-") {
			t.Errorf("temporary owned path remains: %s", entry.Name())
		}
	}
}

func TestInstallSkills_RefusesReplacementBeforeMutatingLiveDestinationWhenExchangeUnavailable(t *testing.T) {
	target := t.TempDir()
	destination := filepath.Join(target, "jirahere-alpha")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalExchange := exchangeSkillDirectories
	t.Cleanup(func() { exchangeSkillDirectories = originalExchange })
	exchangeSkillDirectories = func(stage, live string) error {
		return errors.New("exchange unavailable")
	}

	_, err := InstallSkills(target, []skills.Skill{{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("new")}}})
	var installErr *SkillsInstallError
	if !errors.As(err, &installErr) || installErr.Skill != "jirahere-alpha" {
		t.Fatalf("error = %v, want failed jirahere-alpha", err)
	}
	contents, readErr := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	if readErr != nil || string(contents) != "old" {
		t.Errorf("live contents = %q, %v; want old", contents, readErr)
	}
}

func TestInstallSkills_CreatesMissingTargetParentsWithNormalizedModes(t *testing.T) {
	target := filepath.Join(t.TempDir(), "one", "two", "skills")
	_, err := InstallSkills(target, []skills.Skill{{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}}})
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	for _, directory := range []string{filepath.Dir(filepath.Dir(target)), filepath.Dir(target), target} {
		info, statErr := os.Stat(directory)
		if statErr != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %v, %v; want 0755", directory, info.Mode(), statErr)
		}
	}
}

func TestSkillsNamespacePrefixes_PinnedToSharedNamespace(t *testing.T) {
	cases := []struct{ got, want string }{
		{SkillDirPrefix, "jirahere-"},
		{SkillStagePrefix, "jirahere-stage-"},
		{SkillPreviousPrefix, "jirahere-previous-"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("prefix = %q, want byte-identical %q", c.got, c.want)
		}
		if !strings.Contains(c.got, layout.Namespace) {
			t.Errorf("prefix %q does not contain the shared namespace token %q", c.got, layout.Namespace)
		}
	}
}

func TestGuardNamespaceLockGovernsSkillsCleanupPaths(t *testing.T) {
	base := t.TempDir()
	if strings.Contains(base, layout.Namespace) {
		t.Skipf("temp dir %q unexpectedly contains %q; namespace-rejection setup not valid here", base, layout.Namespace)
	}
	if err := os.Mkdir(filepath.Join(base, "not-ours"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := safedelete.RemoveAll(base, filepath.Join(base, "not-ours"))
	var outside *safedelete.ErrOutsideBase
	if !errors.As(err, &outside) {
		t.Fatalf("RemoveAll of a non-namespaced path: err = %v, want *safedelete.ErrOutsideBase", err)
	}
	if !strings.Contains(outside.Reason, "namespace token") {
		t.Errorf("rejection Reason = %q, want it to name the namespace token", outside.Reason)
	}
	if _, statErr := os.Stat(filepath.Join(base, "not-ours")); statErr != nil {
		t.Fatalf("non-namespaced dir removed despite a rejection: %v", statErr)
	}

	for _, prefix := range []string{SkillDirPrefix, SkillStagePrefix, SkillPreviousPrefix} {
		if !strings.Contains(prefix, layout.Namespace) {
			t.Errorf("real cleanup prefix %q lacks the namespace token; guard would reject genuine deletions", prefix)
		}
	}
}

func TestInstallSkills_PruneSweepSkipsNamespacedNonDirectories(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, target string) string
	}{
		{
			name: "regular file",
			setup: func(t *testing.T, target string) string {
				p := filepath.Join(target, "jirahere-notes.md")
				if err := os.WriteFile(p, []byte("keep me"), 0o644); err != nil {
					t.Fatal(err)
				}
				return p
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, target string) string {
				pointee := filepath.Join(t.TempDir(), "real")
				if err := os.MkdirAll(pointee, 0o755); err != nil {
					t.Fatal(err)
				}
				p := filepath.Join(target, "jirahere-link")
				if err := os.Symlink(pointee, p); err != nil {
					t.Fatal(err)
				}
				return p
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := t.TempDir()
			entry := tc.setup(t, target)

			result, err := InstallSkills(target, []skills.Skill{
				{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}},
			})
			if err != nil {
				t.Fatalf("InstallSkills: %v", err)
			}
			if _, statErr := os.Lstat(entry); statErr != nil {
				t.Errorf("namespaced non-directory removed by the prune sweep: %v", statErr)
			}
			for _, pruned := range result.Pruned {
				if pruned == filepath.Base(entry) {
					t.Errorf("result.Pruned = %v, want %q not reported as pruned", result.Pruned, filepath.Base(entry))
				}
			}
		})
	}
}

func TestInstallSkills_GuardRejectionSurfacesAsInstallErrorWithPriorEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stale := filepath.Join(home, "jirahere-old")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "skills")
	if err := os.Symlink(home, target); err != nil {
		t.Fatalf("symlink skills root at home: %v", err)
	}

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}},
	})

	wantEvents := []SkillsInstallEvent{{Action: "written", Name: "jirahere-alpha"}}
	if !reflect.DeepEqual(result.Events, wantEvents) {
		t.Errorf("result.Events = %+v, want prior write preserved %+v", result.Events, wantEvents)
	}

	var installErr *SkillsInstallError
	if !errors.As(err, &installErr) || installErr.Skill != "jirahere-old" {
		t.Fatalf("err = %v, want *SkillsInstallError naming jirahere-old", err)
	}
	var outside *safedelete.ErrOutsideBase
	if !errors.As(err, &outside) {
		t.Fatalf("err = %v, want it to wrap *safedelete.ErrOutsideBase", err)
	}
	if !strings.Contains(outside.Reason, "home directory") {
		t.Errorf("rejection Reason = %q, want the home-directory rule", outside.Reason)
	}
	if _, statErr := os.Stat(stale); statErr != nil {
		t.Fatalf("stale directory removed despite a guard rejection: %v", statErr)
	}
}

func TestInstallSkills_StopsAfterFailedSkillAndKeepsEarlierWrites(t *testing.T) {
	target := t.TempDir()
	result, err := InstallSkills(target, []skills.Skill{
		{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}},
		{Name: "jirahere-beta", Files: map[string][]byte{"../escape": []byte("bad")}},
		{Name: "jirahere-gamma", Files: map[string][]byte{"SKILL.md": []byte("gamma")}},
	})
	var installErr *SkillsInstallError
	if !errors.As(err, &installErr) || installErr.Skill != "jirahere-beta" {
		t.Fatalf("error = %v, want failed jirahere-beta", err)
	}
	if !reflect.DeepEqual(result.Written, []string{"jirahere-alpha"}) {
		t.Errorf("written = %v, want only alpha", result.Written)
	}
	if _, err := os.Stat(filepath.Join(target, "jirahere-alpha", "SKILL.md")); err != nil {
		t.Errorf("earlier skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "jirahere-gamma")); !os.IsNotExist(err) {
		t.Errorf("later skill was attempted; stat error = %v", err)
	}
}
