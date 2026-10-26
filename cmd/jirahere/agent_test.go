package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/agent"
)

func TestParseAgentInstallOptions_AcceptsSupportedAppsAndDir(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantApp    string
		wantDir    string
		wantDirSet bool
		wantAgent  string
	}{
		{"codex without dir", []string{"codex"}, "codex", "", false, "jirasistant"},
		{"claude with dir after app", []string{"claude", "--dir", "custom"}, "claude", "custom", true, "jirasistant"},
		{"dir before app", []string{"--dir=custom", "codex"}, "codex", "custom", true, "jirasistant"},
		{"explicit empty dir", []string{"codex", "--dir="}, "codex", "", true, "jirasistant"},
		{"explicit jirasistant agent", []string{"codex", "jirasistant"}, "codex", "", false, "jirasistant"},
		{"explicit jirasistant-req agent", []string{"claude", "jirasistant-req"}, "claude", "", false, "jirasistant-req"},
		{"agent positional with dir", []string{"claude", "jirasistant-req", "--dir", "custom"}, "claude", "custom", true, "jirasistant-req"},
		{"dir between positionals", []string{"claude", "--dir", "custom", "jirasistant-req"}, "claude", "custom", true, "jirasistant-req"},
		{"dir= between positionals", []string{"claude", "--dir=custom", "jirasistant-req"}, "claude", "custom", true, "jirasistant-req"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, opts, err := parseAgentInstallOptions(tt.args)
			if err != nil {
				t.Fatalf("parseAgentInstallOptions(%v): %v", tt.args, err)
			}
			if app != tt.wantApp || opts.dir != tt.wantDir || opts.dirSet != tt.wantDirSet || opts.agent != tt.wantAgent {
				t.Errorf("got app=%q opts=%+v, want app=%q dir=%q dirSet=%v agent=%q", app, opts, tt.wantApp, tt.wantDir, tt.wantDirSet, tt.wantAgent)
			}
		})
	}
}

func TestRunAgentInstall_BodyFreeValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing app", nil, "usage: jirahere agent install"},
		{"unknown app", []string{"cursor"}, "usage: jirahere agent install"},
		{"unknown agent", []string{"codex", "nope"}, "usage: jirahere agent install"},
		{"three positionals", []string{"codex", "jirasistant", "extra"}, "usage: jirahere agent install"},
		{"repeated dir", []string{"codex", "--dir", "one", "--dir", "two"}, "usage: jirahere agent install"},
		{"unknown flag", []string{"codex", "--unknown", "value"}, "flag provided but not defined: --unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runAgentInstall(tt.args) })
				if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, tt.want) {
					t.Errorf("stderr = %q, want one body-free line containing %q", stderr, tt.want)
				}
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

func TestRunAgentInstall_WritesThenReplacesEmbeddedAsset(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "claude-config")
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	explicit := filepath.Join(t.TempDir(), "nested", "custom")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("CODEX_HOME", codexHome)

	tests := []struct {
		name       string
		agentName  string
		app        string
		args       []string
		wantTarget string
	}{
		{"codex", "jirasistant", "codex", []string{"codex"}, filepath.Join(codexHome, "jirasistant.config.toml")},
		{"claude", "jirasistant", "claude", []string{"claude"}, filepath.Join(configDir, "agents", "jirasistant.md")},
		{"claude with dir", "jirasistant", "claude", []string{"claude", "--dir", explicit}, filepath.Join(explicit, "jirasistant.md")},
		{"codex with dir", "jirasistant", "codex", []string{"codex", "--dir", explicit}, filepath.Join(explicit, "jirasistant.config.toml")},
		{"codex jirasistant-req", "jirasistant-req", "codex", []string{"codex", "jirasistant-req"}, filepath.Join(codexHome, "jirasistant-req.config.toml")},
		{"claude jirasistant-req", "jirasistant-req", "claude", []string{"claude", "jirasistant-req"}, filepath.Join(configDir, "agents", "jirasistant-req.md")},
		{"claude jirasistant-req with dir", "jirasistant-req", "claude", []string{"claude", "jirasistant-req", "--dir", explicit}, filepath.Join(explicit, "jirasistant-req.md")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, want, err := agent.Asset(tt.agentName, tt.app)
			if err != nil {
				t.Fatalf("agent.Asset(%q, %q): %v", tt.agentName, tt.app, err)
			}
			for i, wantOut := range []string{"written " + tt.agentName + "\n", "replaced " + tt.agentName + "\n"} {
				if i == 1 {

					if err := os.WriteFile(tt.wantTarget, []byte("stale"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				var runErr error
				stdout := captureStdout(t, func() {
					stderr := captureStderr(t, func() { runErr = runAgentInstall(tt.args) })
					if stderr != "" {
						t.Errorf("run %d: stderr = %q, want empty", i, stderr)
					}
				})
				if runErr != nil {
					t.Fatalf("run %d: %v", i, runErr)
				}
				if stdout != wantOut {
					t.Errorf("run %d: stdout = %q, want %q", i, stdout, wantOut)
				}
				got, err := os.ReadFile(tt.wantTarget)
				if err != nil {
					t.Fatalf("run %d: read target: %v", i, err)
				}
				if string(got) != string(want) {
					t.Errorf("run %d: target content differs from embedded asset", i)
				}
			}
		})
	}
}

func TestRunAgentInstall_JirasistantStdoutUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")

	for _, argsFor := range []func(dir string) []string{
		func(dir string) []string { return []string{"claude", "--dir", dir} },
		func(dir string) []string { return []string{"claude", "jirasistant", "--dir", dir} },
	} {
		dir := t.TempDir()
		for i, want := range []string{"written jirasistant\n", "replaced jirasistant\n"} {
			args := argsFor(dir)
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runAgentInstall(args) })
				if stderr != "" {
					t.Errorf("run %d: stderr = %q, want empty", i, stderr)
				}
			})
			if err != nil {
				t.Fatalf("run %d: %v", i, err)
			}
			if stdout != want {
				t.Errorf("args=%v run %d: stdout = %q, want %q", args, i, stdout, want)
			}
		}
	}
}

func TestRunAgentInstall_WriteFailuresAreBodyFree(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirTarget := filepath.Join(root, "dirtarget")
	if err := os.MkdirAll(filepath.Join(dirTarget, "jirasistant.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		dir  string
		want string
	}{
		{"parent uncreatable", filepath.Join(blocker, "sub"), "jirahere: agent install: could not prepare target directory\n"},
		{"rename onto directory", dirTarget, "jirahere: agent install: failed to write agent file\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runAgentInstall([]string{"claude", "--dir", tt.dir}) })
				if stderr != tt.want {
					t.Errorf("stderr = %q, want %q", stderr, tt.want)
				}
			})
			if err == nil {
				t.Fatal("err = nil, want failure")
			}
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Errorf("err = %v, want it to wrap *errSilent so main doesn't double-print", err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

func TestRunAgentInstall_JirasistantReqInstalls(t *testing.T) {
	for _, app := range []string{"codex", "claude"} {
		t.Run(app, func(t *testing.T) {
			dir := t.TempDir()
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runAgentInstall([]string{app, "jirasistant-req", "--dir", dir}) })
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
			})
			if err != nil {
				t.Fatalf("err = %v, want success", err)
			}
			if stdout != "written jirasistant-req\n" {
				t.Errorf("stdout = %q, want %q", stdout, "written jirasistant-req\n")
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("dir has %d entries, want exactly one written", len(entries))
			}
		})
	}
}

func TestRunAgentInstall_EmbeddedAssetLoadFailureIsBodyFree(t *testing.T) {
	orig := loadAgentAsset
	loadAgentAsset = func(string, string) (string, []byte, error) { return "", nil, errors.New("secret detail") }
	t.Cleanup(func() { loadAgentAsset = orig })
	dir := t.TempDir()

	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runAgentInstall([]string{"codex", "--dir", dir}) })
		if stderr != "jirahere: agent install: could not load embedded agent file\n" {
			t.Errorf("stderr = %q, want body-free load failure", stderr)
		}
	})
	if err == nil || stdout != "" {
		t.Errorf("err = %v stdout = %q, want failure with empty stdout", err, stdout)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dir has %d entries, want none written", len(entries))
	}
}

func TestRunAgentInstall_UnresolvableHomeIsBodyFreeAndPrintsNoTarget(t *testing.T) {
	t.Setenv("HOME", "")
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")
	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runAgentInstall([]string{"claude"}) })
		if stderr != "jirahere: agent install: could not resolve home directory\n" {
			t.Errorf("stderr = %q, want one body-free home-resolution line and no target line", stderr)
		}
	})
	if err == nil {
		t.Fatal("err = nil, want failure")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunAgentInstall_HelpUsesSharedFlagUsage(t *testing.T) {
	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runAgentInstall([]string{"codex", "--help"}) })
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stdout, "usage: jirahere agent install <app> [<agent>] [options]") || !strings.Contains(stdout, "--dir") {
		t.Errorf("stdout = %q, want generated agent install usage with --dir", stdout)
	}
}

func TestRunAgent_NoSubcommandIsUsageError(t *testing.T) {
	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runAgent(nil) })
		if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "usage: jirahere agent install") {
			t.Errorf("stderr = %q, want one body-free usage line", stderr)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "usage: jirahere agent install") {
		t.Fatalf("err = %v, want usage error", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRunAgent_UnknownSubcommandIsUsageError(t *testing.T) {
	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runAgent([]string{"remove", "codex"}) })
		if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "usage: jirahere agent install") {
			t.Errorf("stderr = %q, want one body-free usage line", stderr)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "usage: jirahere agent install") {
		t.Fatalf("err = %v, want usage error", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestMainDispatch_AgentInstallAndRootUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")
	target := filepath.Join(home, ".claude", "agents", "jirasistant.md")
	for _, want := range []string{"written jirasistant\n", "replaced jirasistant\n"} {
		stdout, stderr, exitCode := runJirahereProcess(t, "agent", "install", "claude")
		if exitCode != 0 || stdout != want || stderr != "" {
			t.Errorf("agent install: stdout=%q stderr=%q exit=%d, want %q on stdout and success", stdout, stderr, exitCode, want)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("target mode = %v, want 0644", info.Mode().Perm())
	}

	stdout, stderr, exitCode := runJirahereProcess(t, "--help")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "agent install") {
		t.Errorf("root help: stdout=%q stderr=%q exit=%d, want agent install on stdout and success", stdout, stderr, exitCode)
	}
}

func TestMainDispatch_AgentInstallUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"missing app", []string{"agent", "install"}},
		{"unknown app", []string{"agent", "install", "cursor"}},
		{"unknown agent", []string{"agent", "install", "codex", "extra"}},
		{"three positionals", []string{"agent", "install", "codex", "jirasistant", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != 2 || stdout != "" || !strings.HasPrefix(stderr, "usage: jirahere agent install ") {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d, want body-free agent install usage error", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}

func TestMainPositionalCommandHelp_AgentInstall(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"agent install long", []string{"agent", "install", "--help"}},
		{"agent install short", []string{"agent", "install", "-h"}},
		{"agent group long", []string{"agent", "--help"}},
		{"agent group short", []string{"agent", "-h"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "usage: jirahere agent install <app> [<agent>] [options]") {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d, want help on stdout and exit 0", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}

func TestParseAgentInstallOptions_Specialization(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("SECRET-BODY"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name      string
		args      []string
		wantSpecs []string
		wantDir   string
		wantAgent string
	}{
		{"none", []string{"claude"}, nil, "", "jirasistant"},
		{"one after app", []string{"claude", "--specialization", a}, []string{a}, "", "jirasistant"},
		{"one before app", []string{"--specialization", a, "claude"}, []string{a}, "", "jirasistant"},
		{"equals form", []string{"claude", "--specialization=" + a}, []string{a}, "", "jirasistant"},
		{"between positionals", []string{"claude", "--specialization", a, "jirasistant-req"}, []string{a}, "", "jirasistant-req"},
		{"multiple keep order", []string{"claude", "--specialization", b, "--specialization", a}, []string{b, a}, "", "jirasistant"},
		{"with dir", []string{"claude", "--dir", "d", "--specialization", a, "--specialization", b}, []string{a, b}, "d", "jirasistant"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, opts, err := parseAgentInstallOptions(tt.args)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if strings.Join(opts.specializations, "|") != strings.Join(tt.wantSpecs, "|") || opts.dir != tt.wantDir || opts.agent != tt.wantAgent {
				t.Errorf("opts = %+v, want specs=%v dir=%q agent=%q", opts, tt.wantSpecs, tt.wantDir, tt.wantAgent)
			}
		})
	}
}

func TestParseAgentInstallOptions_SpecializationRejections(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	if err := os.WriteFile(good, []byte("SECRET-BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "locked.md")
	if err := os.WriteFile(unreadable, []byte("SECRET-BODY"), 0o000); err != nil {
		t.Fatal(err)
	}
	canRead := false
	if f, err := os.Open(unreadable); err == nil {
		canRead = true
		_ = f.Close()
	}
	tests := []struct {
		name      string
		args      []string
		want      string
		wantUsage bool
	}{
		{"empty value", []string{"claude", "--specialization", ""}, "--specialization requires a non-empty file path", true},
		{"empty equals", []string{"claude", "--specialization="}, "--specialization requires a non-empty file path", true},
		{"missing file", []string{"claude", "--specialization", filepath.Join(dir, "nope.md")}, "specialization file not found", false},
		{"directory", []string{"claude", "--specialization", dir}, "specialization is not a regular file", false},
		{"second invalid", []string{"claude", "--specialization", good, "--specialization", dir}, "specialization is not a regular file", false},
		{"unreadable", []string{"claude", "--specialization", unreadable}, "specialization file is not readable", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "unreadable" && canRead {
				t.Skip("running with privileges that ignore file modes")
			}
			var err error
			stderr := captureStderr(t, func() { _, _, err = parseAgentInstallOptions(tt.args) })
			if err == nil || !strings.Contains(stderr, tt.want) || strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "SECRET-BODY") {
				t.Fatalf("err=%v stderr=%q, want one body-free line containing %q", err, stderr, tt.want)
			}
			var u *errUsage
			if errors.As(err, &u) != tt.wantUsage {
				t.Errorf("usage error = %v, want %v", errors.As(err, &u), tt.wantUsage)
			}
		})
	}
}

func TestParseAgentInstallOptions_SpecializationDeviceRejected(t *testing.T) {
	if _, err := os.Stat(os.DevNull); err != nil {
		t.Skip("no null device")
	}
	var err error
	stderr := captureStderr(t, func() {
		_, _, err = parseAgentInstallOptions([]string{"claude", "--specialization", os.DevNull})
	})
	var u *errUsage
	if err == nil || errors.As(err, &u) || stderr != "jirahere: agent install: specialization is not a regular file\n" {
		t.Fatalf("err=%v stderr=%q, want exit-1 not-a-regular-file", err, stderr)
	}
}

func TestMainDispatch_AgentInstallSpecialization(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")
	unsetenvForTest(t, "CODEX_HOME")
	spec := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(spec, []byte("---\nname: s\ndescription: d\n---\n\nSECRET-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()

	stdout, stderr, exitCode := runJirahereProcess(t, "agent", "install", "codex", "--dir", target, "--specialization", spec, "--specialization", spec)
	if exitCode != 1 || stdout != "" || !strings.HasPrefix(stderr, "jirahere: agent install: specialization is not a valid Codex agent config") || strings.Contains(stderr, "SECRET-BODY") {
		t.Errorf("codex non-toml spec: stdout=%q stderr=%q exit=%d", stdout, stderr, exitCode)
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Errorf("target dir has %d entries, want nothing written", len(entries))
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Errorf("home .codex exists (err=%v), want nothing written", err)
	}

	_, stderr, exitCode = runJirahereProcess(t, "agent", "install", "claude", "--specialization", "")
	if exitCode != 2 || !strings.HasPrefix(stderr, "jirahere: agent install: --specialization requires") {
		t.Errorf("empty: stderr=%q exit=%d, want exit 2", stderr, exitCode)
	}
	_, stderr, exitCode = runJirahereProcess(t, "agent", "install", "claude", "--specialization", filepath.Join(target, "nope"))
	if exitCode != 1 || stderr != "jirahere: agent install: specialization file not found\n" {
		t.Errorf("missing: stderr=%q exit=%d, want exit 1", stderr, exitCode)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "agent", "install", "claude", "--dir", target)
	if exitCode != 0 || stdout != "written jirasistant\n" || stderr != "" {
		t.Errorf("no flag: stdout=%q stderr=%q exit=%d", stdout, stderr, exitCode)
	}
}

func TestAgentInstallUsageAndHelpShowSpecialization(t *testing.T) {
	if !strings.Contains(agentInstallUsage, "[--specialization <file>]...") {
		t.Errorf("agentInstallUsage = %q, want the --specialization synopsis", agentInstallUsage)
	}
	stdout, stderr, exitCode := runJirahereProcess(t, "agent", "install", "--help")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "--specialization") {
		t.Errorf("help: stdout=%q stderr=%q exit=%d, want --specialization listed", stdout, stderr, exitCode)
	}
	_, stderr, exitCode = runJirahereProcess(t, "agent", "install", "codex", "--dir", "a", "--dir", "b", "--specialization", "x")
	if exitCode != 2 || !strings.Contains(stderr, "[--specialization <file>]...") {
		t.Errorf("repeated dir: stderr=%q exit=%d, want usage with the new synopsis", stderr, exitCode)
	}
}

func unsetenvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}
