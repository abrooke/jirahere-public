package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/agent"
)

func writeSpec(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")
	unsetenvForTest(t, "CODEX_HOME")
	return home
}

func TestAgentInstallClaude_SingleSpecializationEndToEnd(t *testing.T) {
	isolateHome(t)
	const specSrc = "---\nname: spec\ndescription: Spec desc.\nmodel: opus\ntools: Task\ncolor: blue\n---\n\nSPEC-BODY-LINE\n"
	spec := writeSpec(t, "myspec.md", specSrc)
	dir := t.TempDir()

	for i, want := range []string{"written jirasistant-myspec\n", "replaced jirasistant-myspec\n"} {
		stdout, stderr, code := runJirahereProcess(t, "agent", "install", "claude", "--dir", dir, "--specialization", spec)
		if code != 0 || stderr != "" || stdout != want {
			t.Fatalf("run %d: stdout=%q stderr=%q exit=%d, want %q", i, stdout, stderr, code, want)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "jirasistant-myspec.md" {
		t.Fatalf("dir entries = %v, want only jirasistant-myspec.md (no stray temp files)", entries)
	}
	got, err := os.ReadFile(filepath.Join(dir, "jirasistant-myspec.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, emb, _ := agent.Asset("jirasistant", "claude")
	want, err := agent.MergeClaude(emb, []byte(specSrc))
	if err != nil {
		t.Fatalf("MergeClaude: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("installed file differs from agent.MergeClaude(embedded, spec)")
	}
	s := string(got)
	if !strings.Contains(s, "\nname: jirasistant-spec\n") || !strings.Contains(s, "SPEC-BODY-LINE") || !strings.Contains(s, "color: blue") {
		t.Errorf("installed file missing merged name/body/key")
	}
	if s := strings.Index(s, "You are jirasistant"); s < 0 || s > strings.Index(string(got), "SPEC-BODY-LINE") {
		t.Errorf("embedded body must precede specialization body")
	}

	_, after, _ := agent.Asset("jirasistant", "claude")
	if !bytes.Equal(after, emb) {
		t.Error("embedded asset changed")
	}
}

func TestAgentInstallClaude_MultipleSpecializationsAndReqAgent(t *testing.T) {
	isolateHome(t)
	a := writeSpec(t, "a.md", "---\nname: a\ndescription: A.\n---\n\nBODY-A\n")
	b := writeSpec(t, "b.md", "---\nname: b\ndescription: B.\n---\n\nBODY-B\n")
	dir := t.TempDir()
	stdout, stderr, code := runJirahereProcess(t, "agent", "install", "claude", "jirasistant-req", "--dir", dir, "--specialization", a, "--specialization", b)
	if code != 0 || stderr != "" || stdout != "written jirasistant-req-a-b\n" {
		t.Fatalf("stdout=%q stderr=%q exit=%d", stdout, stderr, code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "jirasistant-req-a-b.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "name: jirasistant-req-a-b") || !strings.Contains(s, "BODY-A") || strings.Index(s, "BODY-A") > strings.Index(s, "BODY-B") {
		t.Errorf("merged file wrong name or spec order")
	}
}

func TestAgentInstallClaude_DefaultDirUsesCombinedName(t *testing.T) {
	home := isolateHome(t)
	spec := writeSpec(t, "x.md", "---\nname: x\ndescription: d\n---\n\nB\n")
	_, stderr, code := runJirahereProcess(t, "agent", "install", "claude", "--specialization", spec)
	if code != 0 || stderr != "" {
		t.Fatalf("stderr=%q exit=%d", stderr, code)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "agents", "jirasistant-x.md")); err != nil {
		t.Errorf("default-dir combined file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "agents", "jirasistant.md")); !os.IsNotExist(err) {
		t.Errorf("plain jirasistant.md must not be written (err=%v)", err)
	}
}

func TestAgentInstallClaude_RejectionsWriteNothingAndAreBodyFree(t *testing.T) {
	home := isolateHome(t)
	good := writeSpec(t, "good.md", "---\nname: g\ndescription: d\n---\n\nB\n")
	tests := []struct {
		name string
		spec string
	}{
		{"no frontmatter", writeSpec(t, "nofm.md", "SECRET-BODY\n")},
		{"unterminated", writeSpec(t, "unterm.md", "---\nname: s\n\nSECRET-BODY\n")},
		{"unparseable", writeSpec(t, "bad.md", "---\nname: [s\n---\n\nSECRET-BODY\n")},
		{"empty body", writeSpec(t, "nobody.md", "---\nname: s\ndescription: d\n---\n")},
		{"bad tool name", writeSpec(t, "tool.md", "---\nname: s\ndescription: d\ntools: Read Write\n---\n\nSECRET-BODY\n")},
		{"unsafe stem", writeSpec(t, "has space.md", "---\nname: s\ndescription: d\n---\n\nSECRET-BODY\n")},
		{"stem too long", writeSpec(t, strings.Repeat("a", 241)+".md", "---\nname: s\ndescription: d\n---\n\nSECRET-BODY\n")},
		{"empty stem", writeSpec(t, ".md", "---\nname: s\ndescription: d\n---\n\nSECRET-BODY\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			stdout, stderr, code := runJirahereProcess(t, "agent", "install", "claude", "--dir", dir, "--specialization", good, "--specialization", tt.spec)
			if code != 1 || stdout != "" {
				t.Fatalf("stdout=%q exit=%d, want empty stdout exit 1", stdout, code)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "jirahere: agent install: ") {
				t.Errorf("stderr = %q, want one 'jirahere: agent install: ...' line", stderr)
			}
			if strings.Contains(stderr, "SECRET-BODY") || strings.Contains(stderr, tt.spec) {
				t.Errorf("stderr leaks body or path: %q", stderr)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("dir has %d entries, want nothing written", len(entries))
			}
			if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
				t.Errorf("home .claude created (err=%v)", err)
			}
		})
	}
}

func TestAgentInstallClaude_NoFlagUnchanged(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	stdout, stderr, code := runJirahereProcess(t, "agent", "install", "claude", "--dir", dir)
	if code != 0 || stderr != "" || stdout != "written jirasistant\n" {
		t.Fatalf("stdout=%q stderr=%q exit=%d", stdout, stderr, code)
	}
	_, want, _ := agent.Asset("jirasistant", "claude")
	got, err := os.ReadFile(filepath.Join(dir, "jirasistant.md"))
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("no-flag install must write the embedded file byte-for-byte (err=%v)", err)
	}
}

func TestAgentInstallClaude_MalformedEmbeddedAssetIsInternalErrorBodyFree(t *testing.T) {
	isolateHome(t)
	orig := loadAgentAsset
	loadAgentAsset = func(string, string) (string, []byte, error) { return "x", []byte("SECRET no frontmatter"), nil }
	t.Cleanup(func() { loadAgentAsset = orig })
	spec := writeSpec(t, "s.md", "---\nname: s\n---\n\nBODY\n")
	dir := t.TempDir()

	var err error
	stderr := captureStderr(t, func() {
		err = runAgentInstall([]string{"claude", "--dir", dir, "--specialization", spec})
	})
	if err == nil {
		t.Fatal("err = nil, want failure")
	}
	if want := "jirahere: agent install: embedded agent file could not be merged (internal error)\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dir has %d entries, want none", len(entries))
	}
}

func TestAgentInstallClaude_UnsupportedConstructNamedBodyFreeNothingWritten(t *testing.T) {
	home := isolateHome(t)
	good := writeSpec(t, "good.md", "---\nname: g\ndescription: d\n---\n\nB\n")
	tests := []struct{ name, content, want string }{
		{"block scalar", "---\nname: s\ndescription: |\n  SECRET-VALUE\n---\n\nSECRET-BODY\n", "block scalar (key \"description\", line 3)"},
		{"flow list", "---\nname: s\ntools: [SECRET-VALUE, B]\n---\n\nSECRET-BODY\n", "flow list (key \"tools\", line 3)"},
		{"single-quoted", "---\nname: 'SECRET-VALUE'\n---\n\nSECRET-BODY\n", "single-quoted value (key \"name\", line 2)"},
		{"unsupported escape", "---\nname: \"SECRET-VALUE\\n\"\n---\n\nSECRET-BODY\n", "unsupported escape (key \"name\", line 2)"},
		{"blank line", "---\nname: s\n\ndescription: SECRET-VALUE\n---\n\nSECRET-BODY\n", "blank line in frontmatter (line 3)"},
		{"comment line", "---\nname: s\n# SECRET-VALUE\n---\n\nSECRET-BODY\n", "comment line in frontmatter (line 3)"},
		{"byte-order mark", "\xef\xbb\xbf---\nname: SECRET-VALUE\n---\n\nSECRET-BODY\n", "byte-order mark (line 1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := writeSpec(t, "bad.md", tt.content)
			dir := t.TempDir()
			stdout, stderr, code := runJirahereProcess(t, "agent", "install", "claude", "--dir", dir, "--specialization", good, "--specialization", spec)
			if code != 1 || stdout != "" {
				t.Fatalf("stdout=%q exit=%d, want empty stdout exit 1", stdout, code)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "jirahere: agent install: ") || !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want one line containing %q", stderr, tt.want)
			}
			if strings.Contains(stderr, "SECRET") || strings.Contains(stderr, spec) {
				t.Errorf("stderr leaks body, value or path: %q", stderr)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("dir has %d entries, want nothing written", len(entries))
			}
			if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
				t.Errorf("home .claude created (err=%v)", err)
			}
		})
	}
}
