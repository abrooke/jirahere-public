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
		name    string
		args    []string
		wantApp string
		wantDir string
	}{
		{"codex without dir", []string{"codex"}, "codex", ""},
		{"claude with dir after app", []string{"claude", "--dir", "custom"}, "claude", "custom"},
		{"dir before app", []string{"--dir=custom", "codex"}, "codex", "custom"},
		{"explicit empty dir", []string{"codex", "--dir="}, "codex", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, opts, err := parseAgentInstallOptions(tt.args)
			if err != nil {
				t.Fatalf("parseAgentInstallOptions(%v): %v", tt.args, err)
			}
			wantDirSet := tt.name != "codex without dir"
			if app != tt.wantApp || opts.dir != tt.wantDir || opts.dirSet != wantDirSet {
				t.Errorf("got app=%q opts=%+v, want app=%q dir=%q", app, opts, tt.wantApp, tt.wantDir)
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
		{"surplus positional", []string{"codex", "extra"}, "usage: jirahere agent install"},
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
		app        string
		args       []string
		wantTarget string
	}{
		{"codex", "codex", []string{"codex"}, filepath.Join(codexHome, "jirasistant.config.toml")},
		{"claude", "claude", []string{"claude"}, filepath.Join(configDir, "agents", "jirasistant.md")},
		{"claude with dir", "claude", []string{"claude", "--dir", explicit}, filepath.Join(explicit, "jirasistant.md")},
		{"codex with dir", "codex", []string{"codex", "--dir", explicit}, filepath.Join(explicit, "jirasistant.config.toml")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, want, err := agent.Asset(tt.app)
			if err != nil {
				t.Fatalf("agent.Asset(%q): %v", tt.app, err)
			}
			for i, wantOut := range []string{"written jirasistant\n", "replaced jirasistant\n"} {
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

func TestRunAgentInstall_EmbeddedAssetLoadFailureIsBodyFree(t *testing.T) {
	orig := loadAgentAsset
	loadAgentAsset = func(string) (string, []byte, error) { return "", nil, errors.New("secret detail") }
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
	if !strings.Contains(stdout, "usage: jirahere agent install [options]") || !strings.Contains(stdout, "--dir") {
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
		{"surplus positional", []string{"agent", "install", "codex", "extra"}},
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
			if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "usage: jirahere agent install [options]") {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d, want help on stdout and exit 0", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}

func unsetenvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}
