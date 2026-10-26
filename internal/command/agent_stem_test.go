package command

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAgentInstallTarget_SpecializationStems(t *testing.T) {
	root := "/explicit/root"
	tests := []struct {
		name  string
		agent string
		stems []string
		want  string
	}{
		{"one stem", "jirasistant", []string{"myspec"}, "jirasistant-myspec.md"},
		{"req agent", "jirasistant-req", []string{"myspec"}, "jirasistant-req-myspec.md"},
		{"stems in flag order", "jirasistant", []string{"a", "b"}, "jirasistant-a-b.md"},
		{"allowed punctuation", "jirasistant", []string{"My_spec.v2-x"}, "jirasistant-My_spec.v2-x.md"},
		{"no stems unchanged", "jirasistant", nil, "jirasistant.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAgentInstallTarget(tt.agent, "claude", &root, tt.stems...)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(root, tt.want); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestResolveAgentInstallTarget_StemValidation(t *testing.T) {
	root := "/explicit/root"

	okLen := strings.Repeat("a", 240)
	tests := []struct {
		name  string
		stems []string
		ok    bool
	}{
		{"max length ok", []string{okLen}, true},
		{"one byte over", []string{okLen + "a"}, false},
		{"joined stems exceed", []string{strings.Repeat("a", 120), strings.Repeat("b", 120)}, false},
		{"joined stems at limit", []string{strings.Repeat("a", 120), strings.Repeat("b", 119)}, true},
		{"empty", []string{""}, false},
		{"empty among valid", []string{"a", ""}, false},
		{"slash", []string{"a/b"}, false},
		{"backslash", []string{`a\b`}, false},
		{"dotdot slash", []string{"../x"}, false},
		{"space", []string{"a b"}, false},
		{"nul", []string{"a\x00b"}, false},
		{"newline", []string{"a\nb"}, false},
		{"non-ascii", []string{"é"}, false},
		{"tilde", []string{"~"}, false},
		{"dollar", []string{"$HOME"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAgentInstallTarget("jirasistant", "claude", &root, tt.stems...)
			if tt.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(filepath.Base(got)) > 255 {
					t.Errorf("filename %d bytes", len(filepath.Base(got)))
				}
				return
			}
			if !errors.Is(err, ErrAgentSpecializationStem) {
				t.Fatalf("err = %v, want ErrAgentSpecializationStem", err)
			}
			if got != "" {
				t.Errorf("returned path %q with error", got)
			}
		})
	}
}
