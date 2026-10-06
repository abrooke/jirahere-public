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
	"assets/claude/jirasistant-req.md",
	"assets/claude/jirasistant.md",
	"assets/codex/jirasistant-req.config.toml",
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

	for _, agentName := range []string{"jirasistant", "jirasistant-req"} {
		_, claudeFile, err := Asset(agentName, "claude")
		if err != nil {
			t.Fatalf("Asset(%s, claude): %v", agentName, err)
		}
		if got := frontmatterField(claudeFile, "name"); got != agentName {
			t.Errorf("Asset(%s, claude) frontmatter name = %q, want %q", agentName, got, agentName)
		}

		_, codexFile, err := Asset(agentName, "codex")
		if err != nil {
			t.Fatalf("Asset(%s, codex): %v", agentName, err)
		}
		var parsed map[string]any
		if _, err := toml.Decode(string(codexFile), &parsed); err != nil {
			t.Fatalf("Asset(%s, codex) is not valid TOML: %v", agentName, err)
		}
		for _, key := range []string{"sandbox_mode", "approval_policy", "network_access", "description", "developer_instructions"} {
			if _, ok := parsed[key]; !ok {
				t.Errorf("Asset(%s, codex) missing required key %q", agentName, key)
			}
		}
	}
}

func TestAsset_Claude(t *testing.T) {
	tests := []struct {
		agent        string
		wantFilename string
	}{
		{"jirasistant", "jirasistant.md"},
		{"jirasistant-req", "jirasistant-req.md"},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			filename, contents, err := Asset(tt.agent, "claude")
			if err != nil {
				t.Fatalf("Asset(%s, claude): %v", tt.agent, err)
			}
			if filename != tt.wantFilename {
				t.Errorf("filename = %q, want %q", filename, tt.wantFilename)
			}
			if got := frontmatterField(contents, "name"); got != tt.agent {
				t.Errorf("frontmatter name = %q, want %q", got, tt.agent)
			}
			for _, field := range []string{"description", "tools", "model"} {
				if frontmatterField(contents, field) == "" {
					t.Errorf("frontmatter %q missing or empty", field)
				}
			}
		})
	}
}

func TestAsset_JirasistantReq_HasWriteEditTools(t *testing.T) {
	_, contents, err := Asset("jirasistant-req", "claude")
	if err != nil {
		t.Fatalf("Asset(jirasistant-req, claude): %v", err)
	}
	tools := frontmatterField(contents, "tools")
	for _, want := range []string{"Write", "Edit"} {
		found := false
		for _, tool := range strings.Split(tools, ",") {
			if strings.TrimSpace(tool) == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("frontmatter tools = %q, missing %q", tools, want)
		}
	}
}

func TestAsset_Codex(t *testing.T) {
	tests := []struct {
		agent        string
		wantFilename string
	}{
		{"jirasistant", "jirasistant.config.toml"},
		{"jirasistant-req", "jirasistant-req.config.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			filename, contents, err := Asset(tt.agent, "codex")
			if err != nil {
				t.Fatalf("Asset(%s, codex): %v", tt.agent, err)
			}
			if filename != tt.wantFilename {
				t.Errorf("filename = %q, want %q", filename, tt.wantFilename)
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
		})
	}
}

var bundledAgents = []string{"jirasistant", "jirasistant-req"}

func claudeBody(t *testing.T, agent string) string {
	t.Helper()
	_, contents, err := Asset(agent, "claude")
	if err != nil {
		t.Fatalf("Asset(%s, claude): %v", agent, err)
	}
	parts := strings.SplitN(string(contents), "---\n", 3)
	if len(parts) < 3 {
		t.Fatalf("claude asset has no frontmatter block")
	}
	return parts[2]
}

func codexConfig(t *testing.T, agent string) map[string]any {
	t.Helper()
	_, contents, err := Asset(agent, "codex")
	if err != nil {
		t.Fatalf("Asset(%s, codex): %v", agent, err)
	}
	var parsed map[string]any
	if _, err := toml.Decode(string(contents), &parsed); err != nil {
		t.Fatalf("codex asset is not valid TOML: %v", err)
	}
	return parsed
}

func codexInstructions(t *testing.T, agent string) string {
	t.Helper()
	s, _ := codexConfig(t, agent)["developer_instructions"].(string)
	return s
}

func TestAssets_BodiesInSync(t *testing.T) {
	for _, agent := range bundledAgents {
		t.Run(agent, func(t *testing.T) {
			claude := strings.TrimSpace(claudeBody(t, agent))
			codex := strings.TrimSpace(codexInstructions(t, agent))
			if claude == "" {
				t.Fatal("claude body is empty")
			}
			if claude != codex {
				t.Error("claude body and codex developer_instructions differ; keep the two formats' prose identical")
			}
		})
	}
}

func TestAssets_Codex_Posture(t *testing.T) {
	for _, agent := range bundledAgents {
		t.Run(agent, func(t *testing.T) {
			parsed := codexConfig(t, agent)
			if got := parsed["sandbox_mode"]; got != "read-only" {
				t.Errorf("sandbox_mode = %v, want read-only", got)
			}
			if got := parsed["network_access"]; got != false {
				t.Errorf("network_access = %v, want false", got)
			}
			if got := parsed["approval_policy"]; got != "on-request" {
				t.Errorf("approval_policy = %v, want on-request", got)
			}
		})
	}
}

func TestAssets_DescriptionsInSync(t *testing.T) {
	for _, agent := range bundledAgents {
		t.Run(agent, func(t *testing.T) {
			_, contents, err := Asset(agent, "claude")
			if err != nil {
				t.Fatalf("Asset(%s, claude): %v", agent, err)
			}
			claude := frontmatterField(contents, "description")
			codex, _ := codexConfig(t, agent)["description"].(string)
			if claude == "" {
				t.Fatal("claude description is empty")
			}
			if claude != codex {
				t.Errorf("claude description %q != codex description %q", claude, codex)
			}
		})
	}
}

func TestAssets_NameEveryShippedSkill(t *testing.T) {
	all, err := skills.List()
	if err != nil {
		t.Fatalf("skills.List: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no shipped skills found")
	}

	required := map[string][]skills.Skill{
		"jirasistant":     skills.DefaultSet(all),
		"jirasistant-req": all,
	}

	for _, agent := range bundledAgents {
		t.Run(agent, func(t *testing.T) {
			body := claudeBody(t, agent)
			for _, sk := range required[agent] {
				if !strings.Contains(body, "`"+sk.Name+"`") {
					t.Errorf("persona does not reference shipped skill %q", sk.Name)
				}
			}
		})
	}
}

var genericContentForbidden = []string{
	"STUB", "L0101", "reqs/", "this repo", "orchestrat", "requirements role",
	".my-agents", ".claude/", "coder", "reviewer",
}

func TestAssets_GenericContent(t *testing.T) {
	for _, agent := range bundledAgents {
		t.Run(agent, func(t *testing.T) {
			_, claudeFile, err := Asset(agent, "claude")
			if err != nil {
				t.Fatalf("Asset(%s, claude): %v", agent, err)
			}
			_, codexFile, err := Asset(agent, "codex")
			if err != nil {
				t.Fatalf("Asset(%s, codex): %v", agent, err)
			}
			codexDesc, _ := codexConfig(t, agent)["description"].(string)

			for app, text := range map[string]string{
				"claude":             claudeBody(t, agent),
				"codex":              codexInstructions(t, agent),
				"claude description": frontmatterField(claudeFile, "description"),
				"codex description":  codexDesc,
				"codex file":         string(codexFile),
			} {
				lower := strings.ToLower(text)
				for _, f := range genericContentForbidden {
					if strings.Contains(lower, strings.ToLower(f)) {
						t.Errorf("%s persona contains repo-specific/placeholder text %q", app, f)
					}
				}
			}
		})
	}
}

func TestAssets_ShellQuotingRecipe(t *testing.T) {
	const recipe = "`'\\''`"
	for _, agent := range bundledAgents {
		t.Run(agent, func(t *testing.T) {
			_, codexFile, err := Asset(agent, "codex")
			if err != nil {
				t.Fatalf("Asset(%s, codex): %v", agent, err)
			}
			for app, text := range map[string]string{
				"claude": claudeBody(t, agent),
				"codex":  codexInstructions(t, agent),
			} {
				if !strings.Contains(text, recipe) {
					t.Errorf("%s persona is missing the single-quote escape recipe %s", app, recipe)
				}
			}

			if got := strings.Count(string(codexFile), "'''"); got != 2 {
				t.Errorf("codex file has %d runs of three single quotes, want exactly 2 (the developer_instructions delimiters)", got)
			}
		})
	}
}

func TestAssets_PartialFailureRule(t *testing.T) {
	for _, agent := range bundledAgents {
		t.Run(agent, func(t *testing.T) {
			body := claudeBody(t, agent)
			for _, want := range []string{
				"`Created a new ...`", "`Cloning ...`", "`Moving ...`", "`Rehoming ...`",
				"`Parent set:`", "`Summary:`", "`Labels:`", "`Descendant ...`", "`Reparented ...`",
				"lone `jirahere: ...` diagnostic",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("persona outcome rule is missing %s", want)
				}
			}
		})
	}
}

var jirasistantReqForbiddenTokens = []string{
	"R#####", "J#####", "Done When", "Done-When", "done/", "closed/",
}

func TestAssets_JirasistantReq_Content(t *testing.T) {
	bodies := map[string]string{
		"claude": claudeBody(t, "jirasistant-req"),
		"codex":  codexInstructions(t, "jirasistant-req"),
	}

	for app, body := range bodies {
		t.Run(app, func(t *testing.T) {
			for _, skill := range []string{
				"req-create", "req-done", "req-close", "req-update-progress", "req-update-decisions",
			} {
				if !strings.Contains(body, "`"+skill+"`") {
					t.Errorf("jirasistant-req persona does not name installed skill %q", skill)
				}
			}

			decompose := "Decompose:"
			if !strings.Contains(body, decompose) {
				t.Fatalf("jirasistant-req persona is missing a %q section", decompose)
			}
			if !strings.Contains(body, "jirahere context get") {
				t.Error("jirasistant-req decompose section does not read the source item via `jirahere context get`")
			}
			if !strings.Contains(body, "writes nothing to Jira") {
				t.Error("jirasistant-req decompose section does not state that decompose performs no Jira writes")
			}
			if !strings.Contains(body, "if that skill defines no such field, say so and ask the user where to record it") {
				t.Error("jirasistant-req decompose section is missing the ask-the-user fallback for when the installed req-create skill defines no source-key field")
			}

			lower := strings.ToLower(body)
			for _, f := range []string{".claude/", ".my-agents/", "coder", "reviewer", "orchestrator"} {
				if strings.Contains(lower, strings.ToLower(f)) {
					t.Errorf("jirasistant-req persona contains this repo's own dev-tooling text %q", f)
				}
			}
			for _, f := range jirasistantReqForbiddenTokens {
				if strings.Contains(body, f) {
					t.Errorf("jirasistant-req persona contains req-* format detail %q that belongs to the installed skills, not the persona", f)
				}
			}
		})
	}
}

func TestAsset_UnknownApp(t *testing.T) {
	for _, app := range []string{"", "gemini", "Claude"} {
		filename, contents, err := Asset("jirasistant", app)
		if err == nil {
			t.Errorf("Asset(jirasistant, %q) error = nil, want error", app)
		}
		if filename != "" || contents != nil {
			t.Errorf("Asset(jirasistant, %q) = (%q, %d bytes), want zero values", app, filename, len(contents))
		}
	}
}

func TestAsset_UnknownAgent(t *testing.T) {
	for _, agentName := range []string{"", "gemini", "Jirasistant"} {
		filename, contents, err := Asset(agentName, "claude")
		if err == nil {
			t.Errorf("Asset(%q, claude) error = nil, want error", agentName)
		}
		if filename != "" || contents != nil {
			t.Errorf("Asset(%q, claude) = (%q, %d bytes), want zero values", agentName, filename, len(contents))
		}
	}
}

func TestAsset_ReturnsCopy(t *testing.T) {
	_, first, err := Asset("jirasistant", "claude")
	if err != nil {
		t.Fatalf("Asset: %v", err)
	}
	first[0] = 'X'
	_, second, err := Asset("jirasistant", "claude")
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
