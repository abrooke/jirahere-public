package command

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveAgentInstallTarget(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "claude-config")
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("AGENT_TARGET_PART", "from-env")

	tests := []struct {
		name      string
		app       string
		targetDir string
		explicit  bool
		want      string
	}{
		{"claude config directory", "claude", "", false, filepath.Join(configDir, "agents", "jirasistant.md")},
		{"codex home is flat", "codex", "", false, filepath.Join(codexHome, "jirasistant.config.toml")},
		{"claude dir expands tilde and env, filename appended directly", "claude", "~/$AGENT_TARGET_PART", true, filepath.Join(home, "from-env", "jirasistant.md")},
		{"codex dir appended directly", "codex", "/explicit/root", true, filepath.Join("/explicit/root", "jirasistant.config.toml")},
		{"bare tilde dir", "claude", "~", true, filepath.Join(home, "jirasistant.md")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetDir *string
			if tt.explicit {
				targetDir = &tt.targetDir
			}
			got, err := ResolveAgentInstallTarget(tt.app, targetDir)
			if err != nil {
				t.Fatalf("ResolveAgentInstallTarget(%q, %q): %v", tt.app, tt.targetDir, err)
			}
			if got != tt.want {
				t.Errorf("target = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveAgentInstallTarget_DefaultsUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")
	unsetenvForTest(t, "CODEX_HOME")

	for _, tt := range []struct {
		app  string
		want string
	}{
		{"claude", filepath.Join(home, ".claude", "agents", "jirasistant.md")},
		{"codex", filepath.Join(home, ".codex", "jirasistant.config.toml")},
	} {
		got, err := ResolveAgentInstallTarget(tt.app, nil)
		if err != nil {
			t.Fatalf("ResolveAgentInstallTarget(%q, default): %v", tt.app, err)
		}
		if got != tt.want {
			t.Errorf("ResolveAgentInstallTarget(%q, default) = %q, want %q", tt.app, got, tt.want)
		}
	}
}

func TestResolveAgentInstallTarget_RelativeDirIsAbsolutized(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	relative := "relative/root"
	got, err := ResolveAgentInstallTarget("codex", &relative)
	if err != nil {
		t.Fatalf("ResolveAgentInstallTarget relative dir: %v", err)
	}
	if !filepath.IsAbs(got) || filepath.Base(got) != "jirasistant.config.toml" || filepath.Base(filepath.Dir(got)) != "root" {
		t.Errorf("target = %q, want absolute path ending relative/root/jirasistant.config.toml", got)
	}
}

func TestResolveAgentInstallTarget_HomeUnavailable(t *testing.T) {
	t.Setenv("HOME", "")
	unsetenvForTest(t, "CLAUDE_CONFIG_DIR")
	unsetenvForTest(t, "CODEX_HOME")

	for _, app := range []string{"claude", "codex"} {
		got, err := ResolveAgentInstallTarget(app, nil)
		if !errors.Is(err, ErrAgentHomeUnavailable) {
			t.Fatalf("ResolveAgentInstallTarget(%q) with unavailable home error = %v, want ErrAgentHomeUnavailable", app, err)
		}
		if got != "" {
			t.Errorf("target = %q, want empty on error", got)
		}
	}

	tilde := "~/agents"
	if _, err := ResolveAgentInstallTarget("claude", &tilde); !errors.Is(err, ErrAgentHomeUnavailable) {
		t.Errorf("tilde --dir with unavailable home error = %v, want ErrAgentHomeUnavailable", err)
	}

	explicit := "/explicit/root"
	if _, err := ResolveAgentInstallTarget("codex", &explicit); err != nil {
		t.Errorf("explicit --dir with unavailable home: %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if _, err := ResolveAgentInstallTarget("claude", nil); err != nil {
		t.Errorf("CLAUDE_CONFIG_DIR set with unavailable home: %v", err)
	}
}

func TestResolveAgentInstallTarget_RejectsTerminalUnsafePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	unsafe := filepath.Join(t.TempDir(), "agents") + "\x1b[31m\r\nforged\u202e"
	got, err := ResolveAgentInstallTarget("codex", &unsafe)
	if !errors.Is(err, ErrAgentTargetUnsafe) {
		t.Fatalf("error = %v, want ErrAgentTargetUnsafe", err)
	}
	if got != "" {
		t.Errorf("target = %q, want empty on unsafe path", got)
	}
}

func TestResolveAgentInstallTarget_UnsupportedApp(t *testing.T) {
	if _, err := ResolveAgentInstallTarget("cursor", nil); err == nil {
		t.Error("error = nil, want unsupported app error")
	}
}
