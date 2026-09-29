package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/command"
)

func TestMainExitCodes_UsageErrorsExitTwo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"clone", "PROJ-1", "--bogus"}, "jirahere: flag provided but not defined: --bogus\n"},
		{"unknown flag on login", []string{"login", "--nope"}, "jirahere: flag provided but not defined: --nope\n"},
		{"bad flag value", []string{"clone", "PROJ-1", "--part=maybe"}, `jirahere: invalid boolean value "maybe" for --part: parse error` + "\n"},
		{"flag missing its value", []string{"clone", "PROJ-1", "--parent"}, "jirahere: flag needs an argument: --parent\n"},
		{"repeated non-repeatable flag", []string{"clone", "PROJ-1", "--part", "--part"}, "jirahere: --part may not be given more than once.\n"},
		{"move repeated flag", []string{"move", "PROJ-1", "--parent", "A-1", "--parent", "A-2"}, "jirahere: --parent may not be given more than once.\n"},
		{"missing positional", []string{"clone"}, ""},
		{"extra positional", []string{"move", "A-1", "B-2", "C-3"}, ""},
		{"rehome-children missing positional", []string{"rehome-children", "--to", "X-1"}, ""},
		{"mutually exclusive flags", []string{"clone", "PROJ-1", "--part", "--summary-old", "a", "--summary-new", "b"}, "jirahere: --part may not be combined with --summary-old or --summary-new.\n"},
		{"unknown subcommand", []string{"frob"}, "jirahere: command \"frob\" not implemented yet\n"},
		{"no args", nil, ""},
		{"--profile invalid name on login", []string{"login", "--profile", "bad/name"}, ""},
		{"--profile invalid name on logout", []string{"logout", "--profile", "bad/name"}, ""},
		{"--profile empty on logout", []string{"logout", "--profile", ""}, "jirahere: --profile was given but is empty; a profile name must not be empty.\n"},
		{"--profile empty on login", []string{"login", "--profile", ""}, "jirahere: --profile was given but is empty; a profile name must not be empty.\n"},
		{"bare --profile on logout", []string{"logout", "--profile"}, "jirahere: flag needs an argument: --profile\n"},
		{"bare --profile on login", []string{"login", "--profile"}, "jirahere: flag needs an argument: --profile\n"},
		{"create no project and no default", []string{"create", "--summary", "S"}, "jirahere: no project given and no default project configured; pass --project or set defaults.project\n"},
		{"context get no key", []string{"context", "get"}, ""},
		{"configure set no flags", []string{"configure", "set"}, ""},
		{"login non-interactive missing --site", []string{"login", "--api-token"}, ""},
		{"login non-interactive missing --email", []string{"login", "--api-token", "--site", "example.atlassian.net"}, ""},
		{"login malformed --site", []string{"login", "--api-token", "--site", "bad/site", "--email", "a@b.c"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != 2 {
				t.Errorf("args=%v: exit = %d, want 2 (stderr=%q)", tt.args, exitCode, stderr)
			}
			if stdout != "" {
				t.Errorf("args=%v: stdout = %q, want empty", tt.args, stdout)
			}
			if stderr == "" {
				t.Errorf("args=%v: stderr empty, want the usage message", tt.args)
			}
			if tt.want != "" && stderr != tt.want {
				t.Errorf("args=%v: stderr = %q, want %q", tt.args, stderr, tt.want)
			}

			if n := strings.Count(stderr, "jirahere: "); n > 1 {
				t.Errorf("args=%v: stderr = %q carries %d jirahere: prefixes, want at most 1", tt.args, stderr, n)
			}
		})
	}
}

func TestMainExitCodes_RuntimeFailuresExitOne(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	for _, tt := range []struct {
		name string
		args []string
	}{
		{"clone not logged in", []string{"clone", "PROJ-1"}},
		{"comment list not logged in", []string{"comment", "list", "PROJ-1"}},
		{"status set not logged in", []string{"status", "set", "PROJ-1", "--status", "Done"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != 1 {
				t.Errorf("args=%v: exit = %d, want 1 (stderr=%q)", tt.args, exitCode, stderr)
			}
			if stdout != "" {
				t.Errorf("args=%v: stdout = %q, want empty", tt.args, stdout)
			}
			if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; stderr != want {
				t.Errorf("args=%v: stderr = %q, want %q", tt.args, stderr, want)
			}
		})
	}
}

func TestMainExitCodes_HelpExitsZero(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"clone", "--help"},
		{"login", "--help"},
		{"logout", "--help"},
	} {
		stdout, stderr, exitCode := runJirahereProcess(t, args...)
		if exitCode != 0 || stderr != "" || stdout == "" {
			t.Errorf("args=%v: stdout=%q stderr=%q exit=%d, want help on stdout and exit 0", args, stdout, stderr, exitCode)
		}
	}
}

func TestMainExitCodes_QuarterListUnsetDefaultExitsOne(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	stdout, stderr, exitCode := runJirahereProcess(t, "quarter", "list", "--previous")
	if exitCode != 1 {
		t.Errorf("exit = %d, want 1 (stderr=%q)", exitCode, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "defaults.previous_quarter") {
		t.Errorf("stderr = %q, want it to name defaults.previous_quarter", stderr)
	}
}

func TestMappers_UsageVersusRuntimeClass(t *testing.T) {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devnull.Close() }()
	orig := os.Stderr
	os.Stderr = devnull
	defer func() { os.Stderr = orig }()

	boom := errors.New("boom")
	for _, tt := range []struct {
		name  string
		mapFn func(error) error
		in    error
		usage bool
	}{
		{"clone empty --label-new", mapCloneError, command.ErrLabelNewEmpty, true},
		{"clone whitespace --label-new", mapCloneError, &command.ErrInvalidLabel{Label: "a b"}, true},
		{"clone source 404", mapCloneError, &command.IssueNotFoundError{Subject: "clone source", Key: "PROJ-1"}, false},
		{"clone parent 404", mapCloneError, &command.IssueNotFoundError{Subject: "parent", Key: "PROJ-2"}, false},
		{"clone unreachable", mapCloneError, &command.ErrPreflightUnreachable{Subject: "the clone", Key: "PROJ-1", Err: boom}, false},
		{"clone create failed", mapCloneError, &command.ErrCreateFailed{Subject: "the clone", SourceKey: "PROJ-1", Err: boom}, false},
		{"clone Cloners link missing", mapCloneError, command.ErrClonersLinkTypeMissing, false},
		{"clone default", mapCloneError, boom, false},

		{"move empty --label-new", mapMovePreflightError, command.ErrMoveLabelNewEmpty, true},
		{"move whitespace --label-new", mapMovePreflightError, &command.ErrMoveInvalidLabel{Label: "a b"}, true},
		{"move source 404", mapMovePreflightError, &command.IssueNotFoundError{Subject: "move source", Key: "PROJ-1"}, false},
		{"move unreachable", mapMovePreflightError, &command.ErrMovePreflightUnreachable{Subject: "the move", Key: "PROJ-1", Err: boom}, false},
		{"move default", mapMovePreflightError, boom, false},

		{"rehome summary pair incomplete", mapRehomeChildrenPreflightError, command.ErrRehomeSummaryPairIncomplete, true},
		{"rehome label pair incomplete", mapRehomeChildrenPreflightError, command.ErrRehomeLabelPairIncomplete, true},
		{"rehome empty --skip-status entry", mapRehomeChildrenPreflightError, &command.ErrRehomeSkipStatusInvalid{Raw: "Done,"}, true},
		{"rehome parent 404", mapRehomeChildrenPreflightError, &command.IssueNotFoundError{Subject: "rehome-children old parent", Key: "PROJ-1"}, false},
		{"rehome unreachable", mapRehomeChildrenPreflightError, &command.ErrRehomeChildrenPreflightUnreachable{Subject: "rehome-children", Key: "PROJ-1", Err: boom}, false},
		{"rehome default", mapRehomeChildrenPreflightError, boom, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.mapFn(tt.in)
			var silent *errSilent
			if !errors.As(got, &silent) {
				t.Fatalf("mapped error %T is not an *errSilent (message would be re-printed by main)", got)
			}
			var usage *errUsage
			if isUsage := errors.As(got, &usage); isUsage != tt.usage {
				t.Errorf("errors.As(*errUsage) = %v, want %v", isUsage, tt.usage)
			}
		})
	}
}
