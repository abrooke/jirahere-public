package command

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAgentInstallTarget_CodexSpecializationStems(t *testing.T) {
	root := "/explicit/root"
	tests := []struct {
		name  string
		agent string
		stems []string
		want  string
	}{
		{"one stem", "jirasistant", []string{"myspec"}, "jirasistant-myspec.config.toml"},
		{"req agent", "jirasistant-req", []string{"myspec"}, "jirasistant-req-myspec.config.toml"},
		{"stems in flag order", "jirasistant", []string{"a", "b"}, "jirasistant-a-b.config.toml"},
		{"no stems unchanged", "jirasistant", nil, "jirasistant.config.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAgentInstallTarget(tt.agent, "codex", &root, tt.stems...)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(root, tt.want); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestResolveAgentInstallTarget_CodexStemValidation(t *testing.T) {
	root := "/explicit/root"

	okLen := strings.Repeat("a", 231)
	tests := []struct {
		name  string
		stems []string
		ok    bool
	}{
		{"max length ok", []string{okLen}, true},
		{"one byte over", []string{okLen + "a"}, false},
		{"joined stems exceed", []string{strings.Repeat("a", 115), strings.Repeat("b", 116)}, false},
		{"empty", []string{""}, false},
		{"space", []string{"a b"}, false},
		{"slash", []string{"a/b"}, false},
		{"dotdot slash", []string{"../x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAgentInstallTarget("jirasistant", "codex", &root, tt.stems...)
			if tt.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrAgentSpecializationStem) || got != "" {
				t.Fatalf("got %q err=%v, want ErrAgentSpecializationStem", got, err)
			}
		})
	}
}
