package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var expectedSkillNames = []string{
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

var embeddedSkillPrefixes = []string{"jirahere-", "req-"}

func TestList_EmbeddedSkillShape(t *testing.T) {
	skills, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	gotNames := make([]string, 0, len(skills))
	for _, skill := range skills {
		gotNames = append(gotNames, skill.Name)
		ownedPrefix := false
		for _, prefix := range embeddedSkillPrefixes {
			if strings.HasPrefix(skill.Name, prefix) {
				ownedPrefix = true
				break
			}
		}
		if !ownedPrefix {
			t.Errorf("skill name %q does not have an owned prefix (%v)", skill.Name, embeddedSkillPrefixes)
		}

		skillFile, ok := skill.Files["SKILL.md"]
		if !ok {
			t.Errorf("skill %q has no SKILL.md", skill.Name)
			continue
		}
		if got := frontmatterName(skillFile); got != skill.Name {
			t.Errorf("skill %q SKILL.md frontmatter name = %q, want %q", skill.Name, got, skill.Name)
		}
		for relativePath := range skill.Files {
			if relativePath == "" || filepath.IsAbs(relativePath) || strings.HasPrefix(relativePath, "../") {
				t.Errorf("skill %q has unsafe relative path %q", skill.Name, relativePath)
			}
		}
	}
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, expectedSkillNames) {
		t.Errorf("embedded skill names = %v, want %v", gotNames, expectedSkillNames)
	}
}

var loginImperative = regexp.MustCompile(`(?i)(^|[.!?]\s+)run ` + "`jirahere login`")

func TestClone_NoAgentDirectedLoginImperative(t *testing.T) {
	skills, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var body string
	for _, skill := range skills {
		if skill.Name == "jirahere-clone" {
			body = string(skill.Files["SKILL.md"])
		}
	}
	if body == "" {
		t.Fatal("jirahere-clone SKILL.md not found or empty")
	}

	inAuthPrecondition := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			inAuthPrecondition = trimmed == "## Auth precondition"
			continue
		}
		if inAuthPrecondition {
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			continue
		}
		if loginImperative.MatchString(line) {
			t.Errorf("clone SKILL.md body has an agent-directed login imperative outside "+
				"## Auth precondition and the failure-mode table: %q", trimmed)
		}
	}
}

func TestCreate_SkillContentShape(t *testing.T) {
	skills, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var body string
	for _, skill := range skills {
		if skill.Name == "jirahere-create" {
			body = string(skill.Files["SKILL.md"])
		}
	}
	if body == "" {
		t.Fatal("jirahere-create SKILL.md not found or empty")
	}

	if got := frontmatterName([]byte(body)); got != "jirahere-create" {
		t.Errorf("jirahere-create SKILL.md frontmatter name = %q, want %q", got, "jirahere-create")
	}

	for _, heading := range []string{
		"## Purpose",
		"## Auth precondition",
		"## Invocation",
		"## Current-quarter label",
		"## Read the result",
		"## `--parent` link partial failure",
		"## Failures and exit behavior",
	} {
		if !strings.Contains(body, "\n"+heading+"\n") {
			t.Errorf("jirahere-create SKILL.md is missing the %q section", heading)
		}
	}

	inAuthPrecondition := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			inAuthPrecondition = trimmed == "## Auth precondition"
			continue
		}
		if inAuthPrecondition {
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			continue
		}
		if loginImperative.MatchString(line) {
			t.Errorf("create SKILL.md body has an agent-directed login imperative outside "+
				"## Auth precondition and the failure-mode table: %q", trimmed)
		}
	}
}

func TestAssets_ContainsOnlyDirectoriesAndRegularFiles(t *testing.T) {
	entries, err := os.ReadDir("assets")
	if err != nil {
		t.Fatalf("ReadDir assets: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			t.Errorf("assets entry %q is not a directory", entry.Name())
		}
	}

	err = filepath.WalkDir("assets", func(assetPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			t.Errorf("asset %q is a symlink", assetPath)
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			t.Errorf("asset %q has non-regular mode %v", assetPath, info.Mode())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir assets: %v", err)
	}
}

func TestDefaultSet_KeepsOnlyJirahereNamedSkills(t *testing.T) {
	all := []Skill{
		{Name: "jirahere-clone"},
		{Name: "req-create"},
		{Name: "jirahere-create"},
	}
	got := DefaultSet(all)
	var gotNames []string
	for _, s := range got {
		gotNames = append(gotNames, s.Name)
	}
	want := []string{"jirahere-clone", "jirahere-create"}
	if !reflect.DeepEqual(gotNames, want) {
		t.Errorf("DefaultSet names = %v, want %v", gotNames, want)
	}
}

func TestJirasistantReqSet_UnionAndCompleteness(t *testing.T) {
	full := []Skill{
		{Name: "jirahere-clone"},
		{Name: "jirahere-create"},
	}
	allFive := []Skill{
		{Name: "req-create"},
		{Name: "req-done"},
		{Name: "req-close"},
		{Name: "req-update-progress"},
		{Name: "req-update-decisions"},
	}

	t.Run("all five present returns full set plus the five", func(t *testing.T) {
		all := append(append([]Skill{}, full...), allFive...)
		got, err := JirasistantReqSet(all)
		if err != nil {
			t.Fatalf("JirasistantReqSet: %v", err)
		}
		var gotNames []string
		for _, s := range got {
			gotNames = append(gotNames, s.Name)
		}
		sort.Strings(gotNames)
		want := []string{
			"jirahere-clone", "jirahere-create",
			"req-close", "req-create", "req-done", "req-update-decisions", "req-update-progress",
		}
		if !reflect.DeepEqual(gotNames, want) {
			t.Errorf("JirasistantReqSet names = %v, want %v", gotNames, want)
		}
	})

	t.Run("none of the five present fails closed naming all of them", func(t *testing.T) {
		got, err := JirasistantReqSet(full)
		if err == nil {
			t.Fatal("JirasistantReqSet succeeded, want error")
		}
		if got != nil {
			t.Errorf("JirasistantReqSet returned %v skills on error, want none", got)
		}
		for _, name := range JirasistantReqExtraSkills {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not name missing skill %q", err, name)
			}
		}
	})

	t.Run("one of the five missing fails closed naming only that gap", func(t *testing.T) {
		all := append(append([]Skill{}, full...), allFive[1:]...)
		_, err := JirasistantReqSet(all)
		if err == nil {
			t.Fatal("JirasistantReqSet succeeded, want error")
		}
		if !strings.Contains(err.Error(), "req-create") {
			t.Errorf("error %q does not name missing skill %q", err, "req-create")
		}
		for _, present := range []string{"req-done", "req-close", "req-update-progress", "req-update-decisions"} {
			if strings.Contains(err.Error(), present) {
				t.Errorf("error %q names present skill %q as missing", err, present)
			}
		}
	})
}

func frontmatterName(skillFile []byte) string {
	parts := strings.Split(string(skillFile), "---\n")
	if len(parts) < 3 || parts[0] != "" {
		return ""
	}
	for _, line := range strings.Split(parts[1], "\n") {
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
	}
	return ""
}
