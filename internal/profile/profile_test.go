package profile

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{

		{"one char", "a", ""},
		{"one digit", "0", ""},
		{"64 chars", strings.Repeat("a", 64), ""},
		{"dot inside", "a.b", ""},
		{"underscore inside", "a_b", ""},
		{"dash inside", "a-b", ""},
		{"mixed case", "Work-2.x_y", ""},
		{"digit first", "1abc", ""},
		{"double dot inside", "a..b", ""},
		{"trailing dot", "a.", ""},
		{"default is ordinary", "default", ""},

		{"empty", "", "empty"},
		{"65 bytes", strings.Repeat("a", 65), "maximum"},
		{"leading dot", ".hidden", "start with"},
		{"leading dash", "-x", "start with"},
		{"leading underscore", "_x", "start with"},
		{"single dot", ".", "start with"},
		{"dotdot", "..", `".."`},
		{"forward slash", "a/b", "path separator"},
		{"backslash", `a\b`, "path separator"},
		{"leading slash", "/etc", "path separator"},
		{"traversal", "a/../b", "path separator"},
		{"dotdot slash", "../x", `start with`},
		{"NUL", "a\x00b", "control"},
		{"control", "a\x01b", "control"},
		{"newline", "a\nb", "control"},
		{"tab", "a\tb", "control"},
		{"DEL", "a\x7fb", "control"},
		{"space inside", "a b", "whitespace"},
		{"leading space", " a", "whitespace"},
		{"trailing space", "a ", "whitespace"},
		{"non-ASCII", "café", "ASCII"},
		{"non-ASCII leading", "é", "ASCII"},
		{"unicode space", "a b", "ASCII"},
		{"other punctuation", "a:b", "only letters"},
		{"star", "a*", "only letters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%q) = %v, want nil", tt.in, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%q) = nil, want error containing %q", tt.in, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate(%q) = %q, want it to contain %q", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestValidateErrorDoesNotEchoName(t *testing.T) {
	for _, in := range []string{"a/../SECRET", "SECRET\x00", "SECRET é", ".SECRET", "SECRET SECRET"} {
		err := Validate(in)
		if err == nil {
			t.Fatalf("Validate(%q) = nil, want error", in)
		}
		if strings.Contains(err.Error(), "SECRET") {
			t.Errorf("error %q echoes the raw name", err)
		}
	}
}
