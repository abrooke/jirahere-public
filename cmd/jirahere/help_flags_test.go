package main

import (
	"errors"
	"flag"
	"strings"
	"testing"
)

const argvLeakToken = "--Zz$(id)"

const argvLeakMarker = "Zz"

const hostileFlagValue = "\x1b[31m9\r\nX"

func bareSingleDashListingLine(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "--") {
			return line, true
		}
	}
	return "", false
}

func TestHelp_ListsFlagsWithDoubleDash_NoBareSingleDash(t *testing.T) {
	tests := []struct {
		name      string
		run       func([]string) error
		args      []string
		wantFlags []string
	}{
		{
			name:      "create",
			run:       runCreate,
			args:      []string{"--help"},
			wantFlags: []string{"--summary", "--project", "--type", "--description", "--parent", "--label"},
		},
		{
			name:      "login",
			run:       runLogin,
			args:      []string{"--help"},
			wantFlags: []string{"--api-token", "--site", "--email", "--debug", "--profile"},
		},
		{
			name:      "logout",
			run:       runLogout,
			args:      []string{"--help"},
			wantFlags: []string{"--profile"},
		},
		{
			name:      "cache clear",
			run:       runCacheClear,
			args:      []string{"--help"},
			wantFlags: []string{"--profile"},
		},
		{
			name:      "clone (hand-written usage, valid positional)",
			run:       runClone,
			args:      []string{"PROJ-123", "--help"},
			wantFlags: []string{"--summary-old", "--summary-new", "--label-old", "--label-new", "--parent", "--part", "--profile"},
		},
		{
			name:      "context get (hand-written usage, valid positional)",
			run:       runContextGet,
			args:      []string{"PROJ-123", "--help"},
			wantFlags: []string{"--level", "--json", "--md", "--profile"},
		},
		{

			name:      "move (hand-written usage, valid positional)",
			run:       runMove,
			args:      []string{"PROJ-123", "--help"},
			wantFlags: []string{"--parent", "--summary-old", "--summary-new", "--label-old", "--label-new", "--profile"},
		},
		{

			name:      "rehome-children (hand-written usage, valid positional)",
			run:       runRehomeChildren,
			args:      []string{"PROJ-100", "--help"},
			wantFlags: []string{"--to", "--skip-status", "--summary-old", "--summary-new", "--label-old", "--label-new", "--profile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() { err = tt.run(tt.args) })
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}
			if stderrOut != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderrOut)
			}
			for _, want := range tt.wantFlags {
				if !strings.Contains(stdoutOut, want) {
					t.Errorf("stdout = %q, want it to list %q with the double-dash spelling", stdoutOut, want)
				}
			}
			if line, ok := bareSingleDashListingLine(stdoutOut); ok {
				t.Errorf("stdout has a bare single-dash flag listing line %q; the whole listing must use --flag", line)
			}
		})
	}
}

func TestHelp_HandWrittenUsageCommand_SingleUsageBlock(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantHeader string

		leakedTokens []string
	}{
		{
			name:         "clone",
			args:         []string{"clone", "PROJ-123", "--help"},
			wantHeader:   "usage: jirahere clone <source-key> [options]",
			leakedTokens: []string{"[--summary-old <s> --summary-new <s>]"},
		},
		{
			name:         "context get",
			args:         []string{"context", "get", "PROJ-123", "--help"},
			wantHeader:   "usage: jirahere context get <id> [options]",
			leakedTokens: []string{"[--level {0,1,2,3}]"},
		},
		{
			name:         "move",
			args:         []string{"move", "PROJ-123", "--help"},
			wantHeader:   "usage: jirahere move <source-key> [options]",
			leakedTokens: []string{"[--summary-old <s> --summary-new <s>]"},
		},
		{
			name:         "rehome-children",
			args:         []string{"rehome-children", "PROJ-100", "--help"},
			wantHeader:   "usage: jirahere rehome-children <old-parent-key> [options]",
			leakedTokens: []string{"[--skip-status <s1,s2,...>]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)

			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0 (an explicit --help is a successful request)", exitCode)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty (no \"flag: help requested.\" line)", stderr)
			}
			if !strings.Contains(stdout, tt.wantHeader) {
				t.Errorf("stdout = %q, want the generated header %q", stdout, tt.wantHeader)
			}
			if n := strings.Count(stdout, "usage: jirahere "); n != 1 {
				t.Errorf("stdout has %d usage headers, want exactly 1: %q", n, stdout)
			}
			for _, tok := range tt.leakedTokens {
				if strings.Contains(stdout, tok) {
					t.Errorf("stdout = %q, still contains hand-written synopsis fragment %q alongside the generated listing", stdout, tok)
				}
			}
		})
	}
}

func TestHelp_FlaglessCommandPrintsHeaderOnly(t *testing.T) {
	fs := flag.NewFlagSet("frobnicate", flag.ContinueOnError)
	var b strings.Builder
	if err := writeFlagUsage(&b, fs); err != nil {
		t.Fatalf("writeFlagUsage: %v", err)
	}
	if got, want := b.String(), "usage: jirahere frobnicate\n"; got != want {
		t.Errorf("output = %q, want exactly the header line %q", got, want)
	}
}

func TestWriteFlagUsage_NoSpecByteIdentical(t *testing.T) {
	flagless := flag.NewFlagSet("frobnicate", flag.ContinueOnError)
	var flaglessBuf strings.Builder
	if err := writeFlagUsage(&flaglessBuf, flagless); err != nil {
		t.Fatalf("writeFlagUsage: %v", err)
	}
	if got, want := flaglessBuf.String(), "usage: jirahere frobnicate\n"; got != want {
		t.Errorf("flagless, no spec: output = %q, want %q", got, want)
	}

	flagged := flag.NewFlagSet("frobnicate", flag.ContinueOnError)
	flagged.String("widget", "", "a widget")
	var flaggedBuf strings.Builder
	if err := writeFlagUsage(&flaggedBuf, flagged); err != nil {
		t.Fatalf("writeFlagUsage: %v", err)
	}
	if got, want := flaggedBuf.String(), "usage: jirahere frobnicate [options]\n\noptions:\n  --widget string\n        a widget\n"; got != want {
		t.Errorf("flagged, no spec: output = %q, want %q", got, want)
	}
}

func TestWriteFlagUsage_PositionalSpecRenderedInSynopsis(t *testing.T) {
	t.Run("flagged, one positional", func(t *testing.T) {
		fs := flag.NewFlagSet("clone", flag.ContinueOnError)
		fs.String("parent", "", "parent key")
		var b strings.Builder
		if err := writeFlagUsage(&b, fs, flagUsageSpec{Positionals: []string{"<source-key>"}}); err != nil {
			t.Fatalf("writeFlagUsage: %v", err)
		}
		if got, want := b.String(), "usage: jirahere clone <source-key> [options]\n\noptions:\n  --parent string\n        parent key\n"; got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})

	t.Run("flagged, two positionals including an optional one", func(t *testing.T) {
		fs := flag.NewFlagSet("agent install", flag.ContinueOnError)
		fs.String("dir", "", "target dir")
		var b strings.Builder
		if err := writeFlagUsage(&b, fs, flagUsageSpec{Positionals: []string{"<app>", "[<agent>]"}}); err != nil {
			t.Fatalf("writeFlagUsage: %v", err)
		}
		wantHeader := "usage: jirahere agent install <app> [<agent>] [options]\n"
		if !strings.HasPrefix(b.String(), wantHeader) {
			t.Errorf("output = %q, want it to start with %q", b.String(), wantHeader)
		}
	})

	t.Run("flagless, one positional", func(t *testing.T) {
		fs := flag.NewFlagSet("frobnicate", flag.ContinueOnError)
		var b strings.Builder
		if err := writeFlagUsage(&b, fs, flagUsageSpec{Positionals: []string{"<thing>"}}); err != nil {
			t.Fatalf("writeFlagUsage: %v", err)
		}
		if got, want := b.String(), "usage: jirahere frobnicate <thing>\n"; got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})
}

func TestHelp_PositionalCommandSynopsisNamesSamePositionalsAsStderrUsage(t *testing.T) {
	tests := []struct {
		name           string
		helpArgs       []string
		missingArgs    []string
		wantPositional string
	}{
		{
			name:           "clone",
			helpArgs:       []string{"clone", "--help"},
			missingArgs:    []string{"clone"},
			wantPositional: "<source-key>",
		},
		{
			name:           "move",
			helpArgs:       []string{"move", "--help"},
			missingArgs:    []string{"move"},
			wantPositional: "<source-key>",
		},
		{
			name:           "rehome-children",
			helpArgs:       []string{"rehome-children", "--help"},
			missingArgs:    []string{"rehome-children"},
			wantPositional: "<old-parent-key>",
		},
		{
			name:           "context get",
			helpArgs:       []string{"context", "get", "--help"},
			missingArgs:    []string{"context", "get"},
			wantPositional: "<id>",
		},
		{
			name:           "skills install",
			helpArgs:       []string{"skills", "install", "--help"},
			missingArgs:    []string{"skills", "install"},
			wantPositional: "<app> [<agent-name>]",
		},
		{
			name:           "agent install",
			helpArgs:       []string{"agent", "install", "--help"},
			missingArgs:    []string{"agent", "install"},
			wantPositional: "<app> [<agent>]",
		},
		{
			name:           "comment add",
			helpArgs:       []string{"comment", "add", "--help"},
			missingArgs:    []string{"comment", "add"},
			wantPositional: "<id>",
		},
		{
			name:           "comment list",
			helpArgs:       []string{"comment", "list", "--help"},
			missingArgs:    []string{"comment", "list"},
			wantPositional: "<id>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helpStdout, helpStderr, helpExit := runJirahereProcess(t, tt.helpArgs...)
			if helpExit != 0 {
				t.Errorf("--help exit code = %d, want 0", helpExit)
			}
			if helpStderr != "" {
				t.Errorf("--help stderr = %q, want empty", helpStderr)
			}
			if !strings.Contains(helpStdout, tt.wantPositional) {
				t.Errorf("--help stdout = %q, want it to name positional(s) %q", helpStdout, tt.wantPositional)
			}

			_, missingStderr, missingExit := runJirahereProcess(t, tt.missingArgs...)
			if missingExit != 2 {
				t.Errorf("missing-positional exit code = %d, want 2", missingExit)
			}
			if !strings.Contains(missingStderr, tt.wantPositional) {
				t.Errorf("missing-positional stderr = %q, want it to name positional(s) %q", missingStderr, tt.wantPositional)
			}
		})
	}
}

func TestHelp_ArgvNeverLeaksIntoListing(t *testing.T) {
	if !strings.Contains(argvLeakToken, argvLeakMarker) {
		t.Fatalf("test setup: argvLeakToken %q must contain marker %q", argvLeakToken, argvLeakMarker)
	}

	commands := []struct {
		name       string
		base       []string
		wantHeader string
		helpWins   bool
	}{
		{"create", []string{"create"}, "usage: jirahere create [options]", false},
		{"context get", []string{"context", "get", "PROJ-123"}, "usage: jirahere context get <id> [options]", true},
	}

	for _, c := range commands {
		t.Run(c.name+"/help-before-bogus", func(t *testing.T) {
			args := append(append([]string{}, c.base...), "--help", argvLeakToken)
			stdout, stderr, exitCode := runJirahereProcess(t, args...)

			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0", exitCode)
			}
			if !strings.Contains(stdout, c.wantHeader) {
				t.Errorf("stdout = %q, want the generated listing %q", stdout, c.wantHeader)
			}
			if strings.Contains(stdout, argvLeakMarker) {
				t.Errorf("stdout = %q, contains argv token marker %q -- the renderer echoed argv", stdout, argvLeakMarker)
			}
			if strings.Contains(stderr, argvLeakMarker) {
				t.Errorf("stderr = %q, contains argv token marker %q on the help path", stderr, argvLeakMarker)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty on a successful --help", stderr)
			}
		})

		t.Run(c.name+"/bogus-before-help", func(t *testing.T) {
			args := append(append([]string{}, c.base...), argvLeakToken, "--help")
			stdout, stderr, exitCode := runJirahereProcess(t, args...)

			if c.helpWins {
				if exitCode != 0 || stderr != "" || !strings.Contains(stdout, c.wantHeader) {
					t.Errorf("stdout=%q stderr=%q exit=%d, want help on stdout and exit 0", stdout, stderr, exitCode)
				}
				if strings.Contains(stdout, argvLeakMarker) {
					t.Errorf("stdout = %q, a usage listing carrying the argv token reached stdout", stdout)
				}
				return
			}

			if exitCode != 2 {
				t.Errorf("exit code = %d, want 2 (undefined flag is a genuine parse error)", exitCode)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty -- no usage listing is produced on the parse-error path", stdout)
			}
			if strings.Contains(stdout, argvLeakMarker) {
				t.Errorf("stdout = %q, a usage listing carrying the argv token reached stdout", stdout)
			}
			if strings.Contains(stdout, "usage: jirahere") {
				t.Errorf("stdout = %q, the renderer ran on a parse error", stdout)
			}
			if want := 1; strings.Count(stderr, "\n") != want {
				t.Errorf("stderr = %q, want exactly one sanitized failf line", stderr)
			}
			if !strings.HasPrefix(stderr, "jirahere: ") {
				t.Errorf("stderr = %q, want failf's \"jirahere: ...\" line", stderr)
			}
		})
	}
}

func TestHelp_DefinedFlagHostileValueNotEchoed(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantHeader string
		wantFlag   string
		wantExtra  []string
	}{
		{

			name:       "context get --level (non-zero registration default)",
			args:       []string{"context", "get", "PROJ-123", "--level", hostileFlagValue, "--help"},
			wantHeader: "usage: jirahere context get <id> [options]",
			wantFlag:   "--level string",
			wantExtra:  []string{`(default "0")`},
		},
		{

			name:       "create --summary (plain string flag)",
			args:       []string{"create", "--summary", hostileFlagValue, "--help"},
			wantHeader: "usage: jirahere create [options]",
			wantFlag:   "--summary string",
			wantExtra:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)

			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0 (--help stays a successful request even with an earlier flag set)", exitCode)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty (help path, not the parse-error path)", stderr)
			}
			if !strings.Contains(stdout, tt.wantHeader) {
				t.Errorf("stdout = %q, want the generated header %q", stdout, tt.wantHeader)
			}
			if !strings.Contains(stdout, tt.wantFlag) {
				t.Errorf("stdout = %q, want the flag's listing line %q", stdout, tt.wantFlag)
			}
			for _, want := range tt.wantExtra {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout = %q, want %q (the REGISTRATION default, i.e. f.DefValue not the live value)", stdout, want)
				}
			}

			if strings.Contains(stdout, hostileFlagValue) {
				t.Errorf("stdout = %q, contains the verbatim hostile flag value -- the renderer echoed a live flag value", stdout)
			}
			for _, b := range []string{"\x1b", "\r"} {
				if strings.Contains(stdout, b) {
					t.Errorf("stdout = %q, contains hostile byte %q from a parsed flag value", stdout, b)
				}
				if strings.Contains(stderr, b) {
					t.Errorf("stderr = %q, contains hostile byte %q", stderr, b)
				}
			}
		})
	}
}

func TestFlagDefaultNote_UsesRegistrationDefaultNotLiveValue(t *testing.T) {
	fs := flag.NewFlagSet("unit", flag.ContinueOnError)
	fs.String("level", "0", "context level")
	fs.String("summary", "", "summary")
	if err := fs.Set("level", hostileFlagValue); err != nil {
		t.Fatalf("fs.Set(level): %v", err)
	}
	if err := fs.Set("summary", hostileFlagValue); err != nil {
		t.Fatalf("fs.Set(summary): %v", err)
	}

	if got, want := flagDefaultNote(fs.Lookup("level")), ` (default "0")`; got != want {
		t.Errorf("flagDefaultNote(level) = %q, want %q (must read f.DefValue, not the live Set value)", got, want)
	}
	if got := flagDefaultNote(fs.Lookup("summary")); got != "" {
		t.Errorf("flagDefaultNote(summary) = %q, want \"\" (zero registration default, regardless of live value)", got)
	}
}
