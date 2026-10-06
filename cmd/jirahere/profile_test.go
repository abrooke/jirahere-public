package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func newProfileFlagSet() (*flag.FlagSet, func() (string, error)) {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.String("other", "", "an unrelated flag")
	return fs, registerProfileFlag(fs)
}

func TestRegisterProfileFlag_Resolve(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr string
	}{
		{name: "absent", args: nil, want: ""},
		{name: "absent with other flag", args: []string{"--other", "x"}, want: ""},
		{name: "valid", args: []string{"--profile", "work"}, want: "work"},
		{name: "valid equals form", args: []string{"--profile=work-2.x_y"}, want: "work-2.x_y"},
		{name: "valid single dash", args: []string{"-profile", "work"}, want: "work"},
		{name: "valid default is ordinary", args: []string{"--profile", "default"}, want: "default"},
		{name: "valid 64 chars", args: []string{"--profile", strings.Repeat("a", 64)}, want: strings.Repeat("a", 64)},

		{
			name:    "given but empty",
			args:    []string{"--profile", ""},
			wantErr: "jirahere: --profile was given but is empty; a profile name must not be empty.",
		},
		{
			name:    "given but empty equals form",
			args:    []string{"--profile="},
			wantErr: "jirahere: --profile was given but is empty; a profile name must not be empty.",
		},
		{
			name:    "given but empty single dash equals form",
			args:    []string{"-profile="},
			wantErr: "jirahere: --profile was given but is empty; a profile name must not be empty.",
		},
		{
			name:    "too long",
			args:    []string{"--profile", strings.Repeat("a", 65)},
			wantErr: "jirahere: invalid --profile: profile name is 65 bytes; the maximum is 64.",
		},
		{
			name:    "dotdot",
			args:    []string{"--profile", ".."},
			wantErr: `jirahere: invalid --profile: profile name must not be "..".`,
		},
		{
			name:    "leading dot",
			args:    []string{"--profile", ".hidden"},
			wantErr: `jirahere: invalid --profile: profile name must not start with '.'.`,
		},
		{
			name:    "leading dash",
			args:    []string{"--profile=-x"},
			wantErr: `jirahere: invalid --profile: profile name must not start with '-'.`,
		},
		{
			name:    "leading underscore",
			args:    []string{"--profile", "_x"},
			wantErr: `jirahere: invalid --profile: profile name must not start with '_'.`,
		},
		{
			name:    "forward slash",
			args:    []string{"--profile", "a/b"},
			wantErr: `jirahere: invalid --profile: profile name must not contain a path separator (/ or \).`,
		},
		{
			name:    "backslash",
			args:    []string{"--profile", `a\b`},
			wantErr: `jirahere: invalid --profile: profile name must not contain a path separator (/ or \).`,
		},
		{
			name:    "control character",
			args:    []string{"--profile", "a\x1bb"},
			wantErr: "jirahere: invalid --profile: profile name must not contain NUL or control characters.",
		},
		{
			name:    "whitespace",
			args:    []string{"--profile", "a b"},
			wantErr: "jirahere: invalid --profile: profile name must not contain whitespace.",
		},
		{
			name:    "non-ASCII",
			args:    []string{"--profile", "café"},
			wantErr: "jirahere: invalid --profile: profile name must be ASCII.",
		},
		{
			name:    "disallowed punctuation",
			args:    []string{"--profile", "a$b"},
			wantErr: "jirahere: invalid --profile: profile name may contain only letters, digits, '.', '_' and '-'.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, resolve := newProfileFlagSet()
			if err := parseFlagSet(fs, tt.args); err != nil {
				t.Fatalf("parseFlagSet(%q) error = %v", tt.args, err)
			}
			var got string
			var err error
			stderr := captureStderr(t, func() { got, err = resolve() })

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("resolve() error = %v, want nil", err)
				}
				if got != tt.want {
					t.Errorf("resolve() = %q, want %q", got, tt.want)
				}
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
				return
			}
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("resolve() error = %v (%T), want *errSilent", err, err)
			}
			if got != "" {
				t.Errorf("resolve() name = %q on error, want empty", got)
			}
			if want := tt.wantErr + "\n"; stderr != want {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}

			if v := strings.TrimPrefix(tt.args[len(tt.args)-1], "--profile="); len(v) >= 3 && strings.Contains(stderr, v) {
				t.Errorf("stderr %q echoes the rejected name", stderr)
			}
		})
	}
}

func TestRegisterProfileFlag_Registration(t *testing.T) {
	fs, _ := newProfileFlagSet()
	f := fs.Lookup("profile")
	if f == nil {
		t.Fatal("--profile not registered")
	}
	if f.DefValue != "" {
		t.Errorf("DefValue = %q, want empty", f.DefValue)
	}
	if f.Usage != profileFlagUsage {
		t.Errorf("Usage = %q, want profileFlagUsage", f.Usage)
	}
}

func TestRegisterProfileFlag_HelpListing(t *testing.T) {
	fs, _ := newProfileFlagSet()
	var buf bytes.Buffer
	if err := writeFlagUsage(&buf, fs); err != nil {
		t.Fatalf("writeFlagUsage: %v", err)
	}

	want := "  --profile profile\n        " + strings.ReplaceAll(profileFlagUsage, "`", "") + "\n"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("help listing missing --profile entry.\ngot:\n%s\nwant substring:\n%s", buf.String(), want)
	}

	if strings.Contains(buf.String(), "(default") {
		t.Errorf("help listing has a default annotation:\n%s", buf.String())
	}
}

func TestLoginRemediation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"default", "", "jirahere login"},
		{"named", "work", "jirahere login --profile work"},
		{"named with punctuation", "a.b_c-d", "jirahere login --profile a.b_c-d"},
		{"named literally default", "default", "jirahere login --profile default"},
		{"control characters stripped", "a\x1b[31mb\r\nc", "jirahere login --profile a[31mbc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := loginRemediation(tt.in); got != tt.want {
				t.Errorf("loginRemediation(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNotLoggedInMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{

		{"default", "", `jirahere: not logged in. Run "jirahere login" first.`},
		{"named", "work", `jirahere: not logged in (profile "work"). Run "jirahere login --profile work" first.`},
		{"named literally default", "default", `jirahere: not logged in (profile "default"). Run "jirahere login --profile default" first.`},
		{"control characters stripped", "a\r\nb", `jirahere: not logged in (profile "ab"). Run "jirahere login --profile ab" first.`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notLoggedInMessage(tt.in)
			if got != tt.want {
				t.Errorf("notLoggedInMessage(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("message %q is not single-line", got)
			}
		})
	}
}

func TestNotLoggedInMessage_DefaultMatchesFailfCallSites(t *testing.T) {
	const callSite = `jirahere: not logged in. Run "jirahere login" first.`
	var err error
	stderr := captureStderr(t, func() { err = failf("%s", notLoggedInMessage("")) })
	if stderr != callSite+"\n" {
		t.Errorf("stderr = %q, want %q", stderr, callSite+"\n")
	}
	if err == nil || err.Error() != callSite {
		t.Errorf("err = %v, want %q", err, callSite)
	}
}
