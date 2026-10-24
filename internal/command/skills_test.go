package command

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSkillsInstallTarget(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "claude-config")
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("SKILLS_TARGET_PART", "from-env")

	tests := []struct {
		name      string
		app       string
		targetDir string
		explicit  bool
		want      string
	}{
		{"explicit target expands tilde and environment", "claude", "~/$SKILLS_TARGET_PART", true, filepath.Join(home, "from-env")},
		{"claude config directory", "claude", "", false, filepath.Join(configDir, "skills")},
		{"codex home", "codex", "", false, filepath.Join(codexHome, "skills")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetDir *string
			if tt.explicit {
				targetDir = &tt.targetDir
			}
			got, err := ResolveSkillsInstallTarget(tt.app, targetDir)
			if err != nil {
				t.Fatalf("ResolveSkillsInstallTarget(%q, %q): %v", tt.app, tt.targetDir, err)
			}
			if got != tt.want {
				t.Errorf("target = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSkillsInstallTarget_DefaultsAndAbsolutizes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")
	unsetenvForTest(t, "CODEX_HOME")

	for _, tt := range []struct {
		app  string
		want string
	}{
		{"claude", filepath.Join(home, ".claude", "skills")},
		{"codex", filepath.Join(home, ".codex", "skills")},
	} {
		got, err := ResolveSkillsInstallTarget(tt.app, nil)
		if err != nil {
			t.Fatalf("ResolveSkillsInstallTarget(%q, default): %v", tt.app, err)
		}
		if got != tt.want {
			t.Errorf("ResolveSkillsInstallTarget(%q, default) = %q, want %q", tt.app, got, tt.want)
		}
	}

	explicitRelativeTarget := "relative-skills"
	got, err := ResolveSkillsInstallTarget("codex", &explicitRelativeTarget)
	if err != nil {
		t.Fatalf("ResolveSkillsInstallTarget explicit relative target: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("explicit relative target = %q, want absolute path", got)
	}
}

func TestResolveSkillsInstallTarget_ExplicitEmptyAppHomeOverridesDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	want, err := filepath.Abs("skills")
	if err != nil {
		t.Fatalf("filepath.Abs(skills): %v", err)
	}

	for _, app := range []string{"claude", "codex"} {
		t.Run(app, func(t *testing.T) {
			got, err := ResolveSkillsInstallTarget(app, nil)
			if err != nil {
				t.Fatalf("ResolveSkillsInstallTarget(%q, default): %v", app, err)
			}
			if got != want {
				t.Errorf("target = %q, want explicitly empty app home to resolve %q", got, want)
			}
		})
	}
}

func TestResolveSkillsInstallTarget_RejectsTerminalUnsafePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	unsafe := filepath.Join(t.TempDir(), "skills") + "\x1b[31m\r\nforged\u202e"
	for _, tt := range []struct {
		name      string
		app       string
		targetDir *string
	}{
		{"explicit dir", "codex", &unsafe},
		{"claude config dir", "claude", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.targetDir == nil {
				t.Setenv("CLAUDE_CONFIG_DIR", unsafe)
			}
			got, err := ResolveSkillsInstallTarget(tt.app, tt.targetDir)
			if !errors.Is(err, ErrSkillsTargetUnsafe) {
				t.Fatalf("ResolveSkillsInstallTarget(%q, %q) error = %v, want ErrSkillsTargetUnsafe", tt.app, unsafe, err)
			}
			if got != "" {
				t.Errorf("target = %q, want empty on unsafe path", got)
			}
		})
	}
}

func unsetenvForTest(t *testing.T, key string) {
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

func TestResolveSkillsInstallTarget_HomeUnavailable(t *testing.T) {
	t.Setenv("HOME", "")
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")

	_, err := ResolveSkillsInstallTarget("claude", nil)
	if !errors.Is(err, ErrSkillsHomeUnavailable) {
		t.Fatalf("ResolveSkillsInstallTarget with unavailable home error = %v, want ErrSkillsHomeUnavailable", err)
	}

	explicitTarget := "/explicit/skills"
	if _, err := ResolveSkillsInstallTarget("codex", &explicitTarget); err != nil {
		t.Fatalf("ResolveSkillsInstallTarget explicit target with unavailable home: %v", err)
	}

	emptyTarget := ""
	got, err := ResolveSkillsInstallTarget("codex", &emptyTarget)
	if err != nil {
		t.Fatalf("ResolveSkillsInstallTarget explicit empty target: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("explicit empty target = %q, want absolute path", got)
	}
}
