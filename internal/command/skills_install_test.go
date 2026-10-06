package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
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
	}, true)
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

	result, err := InstallSkills(target, []skills.Skill{{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("new")}}}, false)
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

	_, err := InstallSkills(target, []skills.Skill{{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("new")}}}, false)
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
	_, err := InstallSkills(target, []skills.Skill{{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}}}, false)
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

func TestSkillOwnedPrefixes_IsExactlyTheTwoFixedLiterals(t *testing.T) {
	want := []string{"jirahere-", "req-"}
	if !reflect.DeepEqual(SkillOwnedPrefixes, want) {
		t.Errorf("SkillOwnedPrefixes = %v, want %v", SkillOwnedPrefixes, want)
	}
}

func TestValidateSkill_AcceptsEitherOwnedPrefixAndRejectsEverythingElse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		skill   string
		wantErr bool
	}{
		{name: "jirahere prefix accepted", skill: "jirahere-req-create", wantErr: false},
		{name: "req prefix accepted", skill: "req-create", wantErr: false},
		{name: "unrecognized prefix rejected", skill: "other-skill", wantErr: true},
		{name: "empty name rejected", skill: "", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkill(skills.Skill{Name: tc.skill, Files: map[string][]byte{"SKILL.md": []byte("x")}})
			if (err != nil) != tc.wantErr {
				t.Errorf("validateSkill(%q) error = %v, wantErr %v", tc.skill, err, tc.wantErr)
			}
		})
	}
}

func TestInstallSkills_PrunesStaleReqPrefixedDirectoryNotInShipped(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills")
	if strings.Contains(target, layout.Namespace) {
		t.Fatalf("target %q unexpectedly contains %q; this test requires a root without it", target, layout.Namespace)
	}
	if err := os.MkdirAll(filepath.Join(target, "req-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "req-old", "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "req-create", Files: map[string][]byte{"SKILL.md": []byte("create")}},
	}, true)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if !reflect.DeepEqual(result.Pruned, []string{"req-old"}) {
		t.Errorf("Pruned = %v, want [req-old]", result.Pruned)
	}
	if _, statErr := os.Stat(filepath.Join(target, "req-old")); !os.IsNotExist(statErr) {
		t.Errorf("stale req-* directory remains; stat error = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(target, SkillPreviousPrefix+"req-old")); !os.IsNotExist(statErr) {
		t.Errorf("rename-staged sibling %q remains after deletion; stat error = %v", SkillPreviousPrefix+"req-old", statErr)
	}
}

func TestInstallSkills_PruneOmittedLeavesStaleOwnedPrefixDirectoryInPlace(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(target, "req-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "req-old", "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "req-create", Files: map[string][]byte{"SKILL.md": []byte("create")}},
	}, false)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(result.Pruned) != 0 {
		t.Errorf("Pruned = %v, want none", result.Pruned)
	}
	for _, event := range result.Events {
		if event.Action == "pruned" {
			t.Errorf("Events = %+v, want no pruned event", result.Events)
		}
	}
	contents, statErr := os.ReadFile(filepath.Join(target, "req-old", "old.md"))
	if statErr != nil || string(contents) != "old" {
		t.Errorf("stale req-* directory did not survive untouched: %q, %v", contents, statErr)
	}
}

func TestInstallSkills_SelfHealsLeftoverPreviousDirFromAnInterruptedRename(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills")
	stray := SkillPreviousPrefix + "req-old"
	if err := os.MkdirAll(filepath.Join(target, stray), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, stray, "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "req-create", Files: map[string][]byte{"SKILL.md": []byte("create")}},
	}, true)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if !reflect.DeepEqual(result.Pruned, []string{stray}) {
		t.Errorf("Pruned = %v, want [%s]", result.Pruned, stray)
	}
	if _, statErr := os.Stat(filepath.Join(target, stray)); !os.IsNotExist(statErr) {
		t.Errorf("stray previous-* directory remains; stat error = %v", statErr)
	}
}

func TestInstallSkills_SelfHealsPreExistingRenameDestinationIndependentOfVisitOrder(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills")
	stale := filepath.Join(target, "req-old")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	collidingName := SkillPreviousPrefix + "req-old"

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "req-create", Files: map[string][]byte{"SKILL.md": []byte("create")}},
		{Name: collidingName, Files: map[string][]byte{"SKILL.md": []byte("colliding")}},
	}, true)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if !reflect.DeepEqual(result.Pruned, []string{"req-old"}) {
		t.Errorf("Pruned = %v, want [req-old]", result.Pruned)
	}
	if _, statErr := os.Stat(stale); !os.IsNotExist(statErr) {
		t.Errorf("stale req-old remains; stat error = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(target, collidingName)); !os.IsNotExist(statErr) {
		t.Errorf("rename destination %q remains after prune; stat error = %v", collidingName, statErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(target, "req-create", "SKILL.md"))
	if readErr != nil || string(contents) != "create" {
		t.Errorf("unrelated shipped skill req-create = %q, %v; want create", contents, readErr)
	}
}

func TestInstallSkills_ClearExistingRenameDestinationFailureSurfacesAsInstallError(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills")
	stale := filepath.Join(target, "req-old")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	collidingName := SkillPreviousPrefix + "req-old"
	t.Setenv("HOME", filepath.Join(target, collidingName))

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "req-create", Files: map[string][]byte{"SKILL.md": []byte("create")}},
		{Name: collidingName, Files: map[string][]byte{"SKILL.md": []byte("colliding")}},
	}, true)

	var installErr *SkillsInstallError
	if !errors.As(err, &installErr) || installErr.Skill != "req-old" {
		t.Fatalf("err = %v, want *SkillsInstallError naming req-old", err)
	}
	if !strings.Contains(err.Error(), "clear existing rename destination") {
		t.Errorf("err = %v, want it to wrap the clear-step message", err)
	}
	var outside *safedelete.ErrOutsideBase
	if !errors.As(err, &outside) {
		t.Fatalf("err = %v, want it to wrap *safedelete.ErrOutsideBase", err)
	}
	if !strings.Contains(outside.Reason, "home directory") {
		t.Errorf("rejection Reason = %q, want the home-directory rule", outside.Reason)
	}
	wantEvents := []SkillsInstallEvent{
		{Action: "written", Name: collidingName},
		{Action: "written", Name: "req-create"},
	}
	if !reflect.DeepEqual(result.Events, wantEvents) {
		t.Errorf("result.Events = %+v, want prior writes preserved %+v", result.Events, wantEvents)
	}
	if _, statErr := os.Stat(stale); statErr != nil {
		t.Errorf("stale req-old removed despite a guard rejection on the clear step: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(target, collidingName)); statErr != nil {
		t.Errorf("colliding destination removed despite a guard rejection: %v", statErr)
	}
}

func TestInstallSkills_InspectRenameDestinationFailureSurfacesAsInstallError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NAME_MAX-based ENAMETOOLONG trigger is not portable to Windows path semantics")
	}

	target := filepath.Join(t.TempDir(), "skills")
	name := "req-" + strings.Repeat("x", 240)
	stale := filepath.Join(target, name)
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkills(target, nil, true)

	var installErr *SkillsInstallError
	if !errors.As(err, &installErr) || installErr.Skill != name {
		t.Fatalf("err = %v, want *SkillsInstallError naming the long stale entry", err)
	}
	if !strings.Contains(err.Error(), "inspect rename destination") {
		t.Errorf("err = %v, want it to wrap the inspect-step message", err)
	}
	if len(result.Pruned) != 0 {
		t.Errorf("Pruned = %v, want none", result.Pruned)
	}
	if _, statErr := os.Stat(stale); statErr != nil {
		t.Errorf("stale entry removed despite an inspect-step failure: %v", statErr)
	}
}

func TestInstallSkills_PruneSweepLeavesUnrecognizedPrefixDirectoriesAlone(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "other-stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "other-stale", "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkills(target, []skills.Skill{
		{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}},
	}, true)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(result.Pruned) != 0 {
		t.Errorf("Pruned = %v, want none", result.Pruned)
	}
	if _, statErr := os.Stat(filepath.Join(target, "other-stale")); statErr != nil {
		t.Errorf("unrecognized-prefix directory removed: %v", statErr)
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
			}, true)
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
	}, true)

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

var wantRealEmbeddedSkillNames = []string{
	"jirahere-clone",
	"jirahere-comment-add",
	"jirahere-comment-list",
	"jirahere-configure-get",
	"jirahere-context-get",
	"jirahere-create",
	"jirahere-move",
	"jirahere-quarter-list",
	"jirahere-rehome-children",
	"jirahere-status-set",
	"jirahere-vision",
	"req-close",
	"req-create",
	"req-done",
	"req-update-decisions",
	"req-update-progress",
}

func TestInstallSkills_JirasistantReqEndToEndAgainstRealEmbeddedBundle(t *testing.T) {
	all, err := skills.List()
	if err != nil {
		t.Fatalf("skills.List: %v", err)
	}

	gotNames := make([]string, 0, len(all))
	for _, s := range all {
		gotNames = append(gotNames, s.Name)
	}
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, wantRealEmbeddedSkillNames) {
		t.Fatalf("skills.List names = %v, want %v", gotNames, wantRealEmbeddedSkillNames)
	}

	reqSet, err := skills.JirasistantReqSet(all)
	if err != nil {
		t.Fatalf("JirasistantReqSet: %v", err)
	}
	reqSetNames := make([]string, 0, len(reqSet))
	for _, s := range reqSet {
		reqSetNames = append(reqSetNames, s.Name)
	}
	sort.Strings(reqSetNames)
	if !reflect.DeepEqual(reqSetNames, wantRealEmbeddedSkillNames) {
		t.Fatalf("JirasistantReqSet names = %v, want %v", reqSetNames, wantRealEmbeddedSkillNames)
	}

	target := t.TempDir()
	firstResult, err := InstallSkills(target, reqSet, true)
	if err != nil {
		t.Fatalf("InstallSkills (jirasistant-req set): %v", err)
	}
	if len(firstResult.Written) != 16 || len(firstResult.Replaced) != 0 || len(firstResult.Pruned) != 0 {
		t.Errorf("first install result = %+v, want 16 written, 0 replaced, 0 pruned", firstResult)
	}

	firstDirs, err := skillDirNames(target)
	if err != nil {
		t.Fatalf("skillDirNames after first install: %v", err)
	}
	if !reflect.DeepEqual(firstDirs, wantRealEmbeddedSkillNames) {
		t.Fatalf("target dirs after first install = %v, want %v", firstDirs, wantRealEmbeddedSkillNames)
	}

	secondResult, err := InstallSkills(target, skills.DefaultSet(all), true)
	if err != nil {
		t.Fatalf("InstallSkills (default set): %v", err)
	}
	wantPruned := []string{"req-close", "req-create", "req-done", "req-update-decisions", "req-update-progress"}
	gotPruned := append([]string{}, secondResult.Pruned...)
	sort.Strings(gotPruned)
	if !reflect.DeepEqual(gotPruned, wantPruned) {
		t.Fatalf("second install pruned = %v, want %v", gotPruned, wantPruned)
	}
	if len(secondResult.Written) != 0 || len(secondResult.Replaced) != 11 {
		t.Errorf("second install result = %+v, want 0 written, 11 replaced", secondResult)
	}

	remainingDirs, err := skillDirNames(target)
	if err != nil {
		t.Fatalf("skillDirNames after prune: %v", err)
	}
	wantRemaining := []string{
		"jirahere-clone",
		"jirahere-comment-add",
		"jirahere-comment-list",
		"jirahere-configure-get",
		"jirahere-context-get",
		"jirahere-create",
		"jirahere-move",
		"jirahere-quarter-list",
		"jirahere-rehome-children",
		"jirahere-status-set",
		"jirahere-vision",
	}
	if !reflect.DeepEqual(remainingDirs, wantRemaining) {
		t.Fatalf("target dirs after prune = %v, want %v", remainingDirs, wantRemaining)
	}
}

func skillDirNames(target string) ([]string, error) {
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("non-directory entry %q in target", entry.Name())
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func TestInstallSkills_StopsAfterFailedSkillAndKeepsEarlierWrites(t *testing.T) {
	target := t.TempDir()
	result, err := InstallSkills(target, []skills.Skill{
		{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}},
		{Name: "jirahere-beta", Files: map[string][]byte{"../escape": []byte("bad")}},
		{Name: "jirahere-gamma", Files: map[string][]byte{"SKILL.md": []byte("gamma")}},
	}, false)
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
