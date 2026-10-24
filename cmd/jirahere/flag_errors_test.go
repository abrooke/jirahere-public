package main

import (
	"errors"
	"flag"
	"regexp"
	"testing"
	"time"
)

var bareDashFlag = regexp.MustCompile(`(^|[^-\w])-[A-Za-z]`)

func TestParseFlagSet_ErrorsNameFlagWithTwoDashes(t *testing.T) {
	newFS := func() *flag.FlagSet {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("profile", "", "")
		fs.String("label", "", "")
		fs.String("y", "", "")
		fs.Bool("json", false, "")
		fs.Int("count", 0, "")
		fs.Duration("timeout", time.Second, "")
		return fs
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing value, profile", []string{"--profile"}, "jirahere: flag needs an argument: --profile"},
		{"missing value, single-dash spelling", []string{"-profile"}, "jirahere: flag needs an argument: --profile"},
		{"missing value, other string flag", []string{"--label"}, "jirahere: flag needs an argument: --label"},
		{"unknown flag", []string{"--bogus"}, "jirahere: flag provided but not defined: --bogus"},
		{"unknown flag, single dash", []string{"-bogus"}, "jirahere: flag provided but not defined: --bogus"},
		{"bad bool value", []string{"--json=maybe"}, `jirahere: invalid boolean value "maybe" for --json: parse error`},
		{"bad int value", []string{"--count", "abc"}, `jirahere: invalid value "abc" for flag --count: parse error`},
		{"bad duration value", []string{"--timeout=soon"}, `jirahere: invalid value "soon" for flag --timeout: parse error`},
		{"value containing a dash", []string{"--count", "-x"}, `jirahere: invalid value "-x" for flag --count: parse error`},
		{"value that looks like the shape", []string{"--count=a for flag -y: b"}, `jirahere: invalid value "a for flag -y: b" for flag --count: parse error`},

		{"passthrough, three dashes", []string{"---x"}, "jirahere: bad flag syntax: ---x"},
		{"passthrough, empty name", []string{"--=x"}, "jirahere: bad flag syntax: --=x"},

		{"unknown flag with ESC stripped", []string{"--bo\x1b[31mgus"}, "jirahere: flag provided but not defined: --bo[31mgus"},
		{"unknown flag with newline stripped", []string{"--bo\ngus"}, "jirahere: flag provided but not defined: --bogus"},
		{"unknown flag with CR stripped", []string{"--profile\r"}, "jirahere: flag provided but not defined: --profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseFlagSet(newFS(), tt.args)
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %v (%T), want *errSilent", err, err)
			}
			if got := err.Error(); got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseFlagSet_ErrorsHaveNoBareDashFlag(t *testing.T) {
	quoted := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	for _, args := range [][]string{
		{"--profile"}, {"--label"}, {"--bogus"}, {"-bogus"},
		{"--json=maybe"}, {"--count", "abc"}, {"--count", "-x"},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("profile", "", "")
		fs.String("label", "", "")
		fs.Bool("json", false, "")
		fs.Int("count", 0, "")
		err := parseFlagSet(fs, args)
		if err == nil {
			t.Fatalf("parseFlagSet(%q) = nil, want error", args)
		}
		msg := quoted.ReplaceAllString(err.Error(), `""`)
		if bareDashFlag.MatchString(msg) {
			t.Errorf("parseFlagSet(%q) message %q has a bare -name", args, err.Error())
		}
	}
}
