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
}

func TestList_EmbeddedSkillShape(t *testing.T) {
	skills, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	gotNames := make([]string, 0, len(skills))
	for _, skill := range skills {
		gotNames = append(gotNames, skill.Name)
		if !strings.HasPrefix(skill.Name, "jirahere-") {
			t.Errorf("skill name %q does not have jirahere- prefix", skill.Name)
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
