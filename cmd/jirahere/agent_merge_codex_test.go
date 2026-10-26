package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/aslanbrooke/jirahere/internal/agent"
)

const codexSpecSrc = "description = \"Spec desc.\"\nsandbox_mode = \"workspace-write\"\ndeveloper_instructions = '''\nSPEC-BODY-LINE\n'''\n"

func TestAgentInstallCodex_SingleSpecializationEndToEnd(t *testing.T) {
	isolateHome(t)
	spec := writeSpec(t, "myspec.config.toml", codexSpecSrc)
	dir := t.TempDir()

	for i, want := range []string{"written jirasistant-myspec\n", "replaced jirasistant-myspec\n"} {
		stdout, stderr, code := runJirahereProcess(t, "agent", "install", "codex", "--dir", dir, "--specialization", spec)
		if code != 0 || stderr != "" || stdout != want {
			t.Fatalf("run %d: stdout=%q stderr=%q exit=%d, want %q", i, stdout, stderr, code, want)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "jirasistant-myspec.config.toml" {
		t.Fatalf("dir entries = %v, want only jirasistant-myspec.config.toml", entries)
	}
	got, err := os.ReadFile(filepath.Join(dir, "jirasistant-myspec.config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	_, emb, _ := agent.Asset("jirasistant", "codex")
	want, err := agent.MergeCodex(emb, []byte(codexSpecSrc))
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("installed file differs from agent.MergeCodex(embedded, spec) (err=%v)", err)
	}
	var m map[string]any
	if _, err := toml.Decode(string(got), &m); err != nil {
		t.Fatalf("installed file is not valid TOML: %v", err)
	}
	if m["sandbox_mode"] != "workspace-write" {
		t.Errorf("sandbox_mode = %v", m["sandbox_mode"])
	}
	if di := m["developer_instructions"].(string); strings.Index(di, "You are jirasistant") > strings.Index(di, "SPEC-BODY-LINE") || !strings.Contains(di, "SPEC-BODY-LINE") {
		t.Errorf("embedded text must precede spec text")
	}
	if _, after, _ := agent.Asset("jirasistant", "codex"); !bytes.Equal(after, emb) {
		t.Error("embedded asset changed")
	}
}

func TestAgentInstallCodex_MultipleSpecializationsAndReqAgent(t *testing.T) {
	isolateHome(t)
	a := writeSpec(t, "a.toml", "developer_instructions = \"BODY-A\"\n")
	b := writeSpec(t, "b.config.toml", "developer_instructions = \"BODY-B\"\n")
	dir := t.TempDir()
	stdout, stderr, code := runJirahereProcess(t, "agent", "install", "codex", "jirasistant-req", "--dir", dir, "--specialization", a, "--specialization", b)
	if code != 0 || stderr != "" || stdout != "written jirasistant-req-a-b\n" {
		t.Fatalf("stdout=%q stderr=%q exit=%d", stdout, stderr, code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "jirasistant-req-a-b.config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "BODY-A") || strings.Index(s, "BODY-A") > strings.Index(s, "BODY-B") {
		t.Errorf("merged file wrong spec order")
	}
}

func TestAgentInstallCodex_DefaultDirUsesCombinedName(t *testing.T) {
	home := isolateHome(t)
	spec := writeSpec(t, "x.toml", "developer_instructions = \"B\"\n")
	_, stderr, code := runJirahereProcess(t, "agent", "install", "codex", "--specialization", spec)
	if code != 0 || stderr != "" {
		t.Fatalf("stderr=%q exit=%d", stderr, code)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "jirasistant-x.config.toml")); err != nil {
		t.Errorf("default-dir combined file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "jirasistant.config.toml")); !os.IsNotExist(err) {
		t.Errorf("plain jirasistant.config.toml must not be written (err=%v)", err)
	}
}

func TestAgentInstallCodex_NoFlagUnchanged(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	stdout, stderr, code := runJirahereProcess(t, "agent", "install", "codex", "--dir", dir)
	if code != 0 || stderr != "" || stdout != "written jirasistant\n" {
		t.Fatalf("stdout=%q stderr=%q exit=%d", stdout, stderr, code)
	}
	_, want, _ := agent.Asset("jirasistant", "codex")
	got, err := os.ReadFile(filepath.Join(dir, "jirasistant.config.toml"))
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("no-flag install must write the embedded file byte-for-byte (err=%v)", err)
	}
}

func TestAgentInstallCodex_RejectionsWriteNothingAndAreBodyFree(t *testing.T) {
	home := isolateHome(t)
	good := writeSpec(t, "good.toml", "developer_instructions = \"G\"\n")
	tests := []struct {
		name string
		spec string
	}{
		{"invalid toml", writeSpec(t, "bad.toml", "developer_instructions = \"SECRET-BODY\n")},
		{"no developer_instructions", writeSpec(t, "nodi.toml", "description = \"SECRET-BODY\"\n")},
		{"granular", writeSpec(t, "gran.toml", "developer_instructions = \"SECRET-BODY\"\n[approval_policy.granular]\nsandbox_approval = true\n")},
		{"untrusted", writeSpec(t, "untr.toml", "developer_instructions = \"SECRET-BODY\"\napproval_policy = \"untrusted\"\n")},
		{"on-failure", writeSpec(t, "onf.toml", "developer_instructions = \"SECRET-BODY\"\napproval_policy = \"on-failure\"\n")},
		{"unknown sandbox", writeSpec(t, "sb.toml", "developer_instructions = \"SECRET-BODY\"\nsandbox_mode = \"yolo\"\n")},
		{"unsafe stem", writeSpec(t, "has space.toml", "developer_instructions = \"SECRET-BODY\"\n")},
		{"stem too long", writeSpec(t, strings.Repeat("a", 232)+".toml", "developer_instructions = \"SECRET-BODY\"\n")},
		{"empty stem", writeSpec(t, ".toml", "developer_instructions = \"SECRET-BODY\"\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			stdout, stderr, code := runJirahereProcess(t, "agent", "install", "codex", "--dir", dir, "--specialization", good, "--specialization", tt.spec)
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
			if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
				t.Errorf("home .codex created (err=%v)", err)
			}
		})
	}
}

func TestAgentInstallCodex_MalformedEmbeddedAssetIsInternalErrorBodyFree(t *testing.T) {
	isolateHome(t)
	orig := loadAgentAsset
	loadAgentAsset = func(string, string) (string, []byte, error) { return "x", []byte("SECRET not toml ="), nil }
	t.Cleanup(func() { loadAgentAsset = orig })
	spec := writeSpec(t, "s.toml", "developer_instructions = \"B\"\n")
	dir := t.TempDir()

	var err error
	stderr := captureStderr(t, func() {
		err = runAgentInstall([]string{"codex", "--dir", dir, "--specialization", spec})
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
