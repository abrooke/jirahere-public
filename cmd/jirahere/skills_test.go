package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/skills"
)

func TestParseSkillsInstallOptions_AcceptsSupportedAppsAndInertDir(t *testing.T) {
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
			app, opts, err := parseSkillsInstallOptions(tt.args)
			if err != nil {
				t.Fatalf("parseSkillsInstallOptions(%v): %v", tt.args, err)
			}
			wantDirSet := tt.name != "codex without dir"
			if app != tt.wantApp || opts.dir != tt.wantDir || opts.dirSet != wantDirSet {
				t.Errorf("got app=%q opts=%+v, want app=%q dir=%q", app, opts, tt.wantApp, tt.wantDir)
			}
		})
	}
}

func TestRunSkillsInstall_BodyFreeValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing app", nil, "usage: jirahere skills install"},
		{"unknown app", []string{"cursor"}, "usage: jirahere skills install"},
		{"surplus positional", []string{"codex", "extra"}, "usage: jirahere skills install"},
		{"repeated dir", []string{"codex", "--dir", "one", "--dir", "two"}, "usage: jirahere skills install"},
		{"unknown flag", []string{"codex", "--unknown", "value"}, "flag provided but not defined: --unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runSkillsInstall(tt.args) })
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

func TestRunSkillsInstall_WritesEmbeddedSkillsAndReportsSummary(t *testing.T) {
	target := filepath.Join(t.TempDir(), "custom-skills")
	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runSkillsInstall([]string{"codex", "--dir", target}) })
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
	if err != nil {
		t.Fatalf("runSkillsInstall: %v", err)
	}
	if strings.Count(stdout, "written jirahere-") != 11 || !strings.Contains(stdout, "installed 11 skill(s): 11 written, 0 replaced, 0 pruned\n") {
		t.Errorf("stdout = %q, want nine writes and summary", stdout)
	}
}

func TestRunSkillsInstall_SanitizesStaleSkillNameInPruneOutput(t *testing.T) {
	target := t.TempDir()
	staleName := "jirahere-stale\nforged\x1b[31m"
	if err := os.Mkdir(filepath.Join(target, staleName), 0o755); err != nil {
		t.Fatal(err)
	}

	originalList := listEmbeddedSkills
	listEmbeddedSkills = func() ([]skills.Skill, error) {
		return []skills.Skill{{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}}}, nil
	}
	t.Cleanup(func() { listEmbeddedSkills = originalList })

	var runErr error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { runErr = runSkillsInstall([]string{"codex", "--dir", target}) })
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
	if runErr != nil {
		t.Fatalf("runSkillsInstall: %v", runErr)
	}
	if got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n"); len(got) != 3 || got[1] != "pruned jirahere-staleforged[31m" {
		t.Errorf("stdout = %q, want one sanitized prune record", stdout)
	} else {
		for _, line := range got {
			if strings.ContainsAny(line, "\r\x1b\u009b") {
				t.Errorf("stdout contains terminal controls: %q", stdout)
			}
		}
	}
}

func TestRunSkillsInstall_PartialFailureReportsCompletedWritesAndFailedSkill(t *testing.T) {
	originalList := listEmbeddedSkills
	listEmbeddedSkills = func() ([]skills.Skill, error) {
		return []skills.Skill{
			{Name: "jirahere-alpha", Files: map[string][]byte{"SKILL.md": []byte("alpha")}},
			{Name: "jirahere-beta", Files: map[string][]byte{"../escape": []byte("bad")}},
			{Name: "jirahere-gamma", Files: map[string][]byte{"SKILL.md": []byte("gamma")}},
		}, nil
	}
	t.Cleanup(func() { listEmbeddedSkills = originalList })

	var runErr error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { runErr = runSkillsInstall([]string{"codex", "--dir", t.TempDir()}) })
		if stderr != "jirahere: skills install: failed to write skill jirahere-beta\n" {
			t.Errorf("stderr = %q, want body-free failed skill error", stderr)
		}
	})
	if runErr == nil {
		t.Fatal("runSkillsInstall succeeded, want partial failure")
	}
	if stdout != "written jirahere-alpha\n" {
		t.Errorf("stdout = %q, want only completed skill output", stdout)
	}
}

func TestRunSkillsInstall_HomeUnavailableIsBodyFree(t *testing.T) {
	t.Setenv("HOME", "")
	unsetenvSkillsTest(t, "CODEX_HOME")
	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runSkillsInstall([]string{"codex"}) })
		if stderr != "jirahere: skills install: could not resolve home directory\n" {
			t.Errorf("stderr = %q, want body-free home-resolution error", stderr)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "could not resolve home directory") {
		t.Fatalf("err = %v, want home-resolution error", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func unsetenvSkillsTest(t *testing.T, key string) {
	t.Helper()
	old, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestRunSkillsInstall_RejectsUnsafeTargetWithoutWritingIt(t *testing.T) {
	unsafe := filepath.Join(t.TempDir(), "skills") + "\x1b[31m\r\nforged\u009b"
	for _, tt := range []struct {
		name string
		args []string
		set  func(t *testing.T)
	}{
		{
			name: "explicit dir",
			args: []string{"codex", "--dir", unsafe},
		},
		{
			name: "codex home",
			args: []string{"codex"},
			set:  func(t *testing.T) { t.Setenv("CODEX_HOME", unsafe) },
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set != nil {
				tt.set(t)
			}
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runSkillsInstall(tt.args) })
				if stderr != "jirahere: skills install: could not resolve target directory\n" {
					t.Errorf("stderr = %q, want one body-free target-resolution error", stderr)
				}
				if strings.ContainsAny(stderr, "\x1b\r\u009b") || strings.Count(stderr, "\n") != 1 {
					t.Errorf("stderr contains terminal controls or forged lines: %q", stderr)
				}
			})
			if err == nil || !strings.Contains(err.Error(), "could not resolve target directory") {
				t.Fatalf("err = %v, want target-resolution error", err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

func TestRunSkillsInstall_ExplicitEmptyAppHomeUsesRelativeSkillsRoot(t *testing.T) {
	workdir := t.TempDir()
	originalWorkdir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWorkdir) })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	for _, app := range []string{"claude", "codex"} {
		t.Run(app, func(t *testing.T) {
			var runErr error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { runErr = runSkillsInstall([]string{app}) })
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
			})
			if runErr != nil || !strings.Contains(stdout, "installed 11 skill(s)") {
				t.Errorf("err = %v stdout = %q, want successful installation", runErr, stdout)
			}
		})
	}
}

func TestRunSkillsInstall_HelpUsesSharedFlagUsage(t *testing.T) {
	var err error
	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runSkillsInstall([]string{"codex", "--help"}) })
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stdout, "usage: jirahere skills install [options]") || !strings.Contains(stdout, "--dir") {
		t.Errorf("stdout = %q, want generated skills install usage with --dir", stdout)
	}
}

func TestMainDispatch_SkillsInstallAndRootUsage(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills")
	stdout, stderr, exitCode := runJirahereProcess(t, "skills", "install", "claude", "--dir", target)
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "installed 11 skill(s)") {
		t.Errorf("skills install: stdout=%q stderr=%q exit=%d, want successful installation", stdout, stderr, exitCode)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "--help")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "skills install") {
		t.Errorf("root help: stdout=%q stderr=%q exit=%d, want skills install on stdout and success", stdout, stderr, exitCode)
	}
}
