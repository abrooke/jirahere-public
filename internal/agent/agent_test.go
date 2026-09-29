package agent

import (
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/aslanbrooke/jirahere/internal/skills"
)

var expectedAssetPaths = []string{
	"assets/claude/jirasistant.md",
	"assets/codex/jirasistant.config.toml",
}

func TestEmbeddedAssets_Shape(t *testing.T) {
	var got []string
	err := fs.WalkDir(embeddedAssets, "assets", func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		got = append(got, p)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			t.Errorf("%q is not a regular file (mode %v)", p, info.Mode())
		}
		if info.Size() == 0 {
			t.Errorf("%q is empty", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, expectedAssetPaths) {
		t.Errorf("embedded asset files = %v, want %v", got, expectedAssetPaths)
	}
}

func TestAsset_Claude(t *testing.T) {
	filename, contents, err := Asset("claude")
	if err != nil {
		t.Fatalf("Asset(claude): %v", err)
	}
	if filename != "jirasistant.md" {
		t.Errorf("filename = %q, want jirasistant.md", filename)
	}
	if got := frontmatterField(contents, "name"); got != "jirasistant" {
		t.Errorf("frontmatter name = %q, want jirasistant", got)
	}
	for _, field := range []string{"description", "tools", "model"} {
		if frontmatterField(contents, field) == "" {
			t.Errorf("frontmatter %q missing or empty", field)
		}
	}
}

func TestAsset_Codex(t *testing.T) {
	filename, contents, err := Asset("codex")
	if err != nil {
		t.Fatalf("Asset(codex): %v", err)
	}
	if filename != "jirasistant.config.toml" {
		t.Errorf("filename = %q, want jirasistant.config.toml", filename)
	}

	var parsed map[string]any
	if _, err := toml.Decode(string(contents), &parsed); err != nil {
		t.Fatalf("codex asset is not valid TOML: %v", err)
	}
	for _, key := range []string{"sandbox_mode", "approval_policy", "network_access", "description", "developer_instructions"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("codex asset missing required key %q", key)
		}
	}
}

func claudeBody(t *testing.T) string {
	t.Helper()
	_, contents, err := Asset("claude")
	if err != nil {
		t.Fatalf("Asset(claude): %v", err)
	}
	parts := strings.SplitN(string(contents), "---\n", 3)
	if len(parts) < 3 {
		t.Fatalf("claude asset has no frontmatter block")
	}
	return parts[2]
}

func codexConfig(t *testing.T) map[string]any {
	t.Helper()
	_, contents, err := Asset("codex")
	if err != nil {
		t.Fatalf("Asset(codex): %v", err)
	}
	var parsed map[string]any
	if _, err := toml.Decode(string(contents), &parsed); err != nil {
		t.Fatalf("codex asset is not valid TOML: %v", err)
	}
	return parsed
}

func codexInstructions(t *testing.T) string {
	t.Helper()
	s, _ := codexConfig(t)["developer_instructions"].(string)
	return s
}

func TestAssets_BodiesInSync(t *testing.T) {
	claude := strings.TrimSpace(claudeBody(t))
	codex := strings.TrimSpace(codexInstructions(t))
	if claude == "" {
		t.Fatal("claude body is empty")
	}
	if claude != codex {
		t.Error("claude body and codex developer_instructions differ; keep the two formats' prose identical")
	}
}

func TestAssets_Codex_Posture(t *testing.T) {
	parsed := codexConfig(t)
	if got := parsed["sandbox_mode"]; got != "read-only" {
		t.Errorf("sandbox_mode = %v, want read-only", got)
	}
	if got := parsed["network_access"]; got != false {
		t.Errorf("network_access = %v, want false", got)
	}
	if got := parsed["approval_policy"]; got != "on-request" {
		t.Errorf("approval_policy = %v, want on-request", got)
	}
}

func TestAssets_DescriptionsInSync(t *testing.T) {
	_, contents, err := Asset("claude")
	if err != nil {
		t.Fatalf("Asset(claude): %v", err)
	}
	claude := frontmatterField(contents, "description")
	codex, _ := codexConfig(t)["description"].(string)
	if claude == "" {
		t.Fatal("claude description is empty")
	}
	if claude != codex {
		t.Errorf("claude description %q != codex description %q", claude, codex)
	}
}

func TestAssets_NameEveryShippedSkill(t *testing.T) {
	shipped, err := skills.List()
	if err != nil {
		t.Fatalf("skills.List: %v", err)
	}
	if len(shipped) == 0 {
		t.Fatal("no shipped skills found")
	}
	body := claudeBody(t)
	for _, sk := range shipped {
		if !strings.Contains(body, "`"+sk.Name+"`") {
			t.Errorf("persona does not reference shipped skill %q", sk.Name)
		}
	}
}

func TestAssets_GenericContent(t *testing.T) {
	forbidden := []string{"STUB", "L0101", "reqs/", "this repo", "orchestrat", "requirements role", ".my-agents"}
	_, claudeFile, err := Asset("claude")
	if err != nil {
		t.Fatalf("Asset(claude): %v", err)
	}
	_, codexFile, err := Asset("codex")
	if err != nil {
		t.Fatalf("Asset(codex): %v", err)
	}
	codexDesc, _ := codexConfig(t)["description"].(string)

	for app, text := range map[string]string{
		"claude":             claudeBody(t),
		"codex":              codexInstructions(t),
		"claude description": frontmatterField(claudeFile, "description"),
		"codex description":  codexDesc,
		"codex file":         string(codexFile),
	} {
		lower := strings.ToLower(text)
		for _, f := range forbidden {
			if strings.Contains(lower, strings.ToLower(f)) {
				t.Errorf("%s persona contains repo-specific/placeholder text %q", app, f)
			}
		}
	}
}

func TestAssets_ShellQuotingRecipe(t *testing.T) {
	const recipe = "`'\\''`"
	_, codexFile, err := Asset("codex")
	if err != nil {
		t.Fatalf("Asset(codex): %v", err)
	}
	for app, text := range map[string]string{
		"claude": claudeBody(t),
		"codex":  codexInstructions(t),
	} {
		if !strings.Contains(text, recipe) {
			t.Errorf("%s persona is missing the single-quote escape recipe %s", app, recipe)
		}
	}

	if got := strings.Count(string(codexFile), "'''"); got != 2 {
		t.Errorf("codex file has %d runs of three single quotes, want exactly 2 (the developer_instructions delimiters)", got)
	}
}

func TestAssets_PartialFailureRule(t *testing.T) {
	body := claudeBody(t)
	for _, want := range []string{
		"`Created a new ...`", "`Cloning ...`", "`Moving ...`", "`Rehoming ...`",
		"`Parent set:`", "`Summary:`", "`Labels:`", "`Descendant ...`", "`Reparented ...`",
		"lone `jirahere: ...` diagnostic",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("persona outcome rule is missing %s", want)
		}
	}
}

func TestAsset_UnknownApp(t *testing.T) {
	for _, app := range []string{"", "gemini", "Claude"} {
		filename, contents, err := Asset(app)
		if err == nil {
			t.Errorf("Asset(%q) error = nil, want error", app)
		}
		if filename != "" || contents != nil {
			t.Errorf("Asset(%q) = (%q, %d bytes), want zero values", app, filename, len(contents))
		}
	}
}

func TestAsset_ReturnsCopy(t *testing.T) {
	_, first, err := Asset("claude")
	if err != nil {
		t.Fatalf("Asset: %v", err)
	}
	first[0] = 'X'
	_, second, err := Asset("claude")
	if err != nil {
		t.Fatalf("Asset: %v", err)
	}
	if second[0] == 'X' {
		t.Error("mutating returned bytes changed the embedded asset")
	}
}

func frontmatterField(file []byte, key string) string {
	parts := strings.SplitN(string(file), "---\n", 3)
	if len(parts) < 3 || parts[0] != "" {
		return ""
	}
	for _, line := range strings.Split(parts[1], "\n") {
		if strings.HasPrefix(line, key+":") {
			value := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
			return strings.Trim(value, `"`)
		}
	}
	return ""
}
