package main

import (
	"errors"
	"flag"
	"strings"
	"testing"
)

const planSettings = `{"defaults":{"project":"PROJ","current_quarter":"Q3'26"}}`

func planExpectFailure(t *testing.T, args []string) string {
	t.Helper()
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runPlan(args) })
	})
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v, want *errSilent (exit 1)", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	return stderr
}

func TestRunPlan_Success_MatchesContextGetLevel0(t *testing.T) {
	for _, format := range []string{"md", "json"} {
		t.Run(format, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, planSettings)
			stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{
				"PROJ-1": "Unrelated work",
				"PROJ-2": "FY26 _Plan Q3'26",
			}, "PROJ-1", "PROJ-2")})
			requested := visionJiraServer(t, `{"issues":[]}`, map[string]string{"PROJ-2": visionActiveFields})

			args, ctxArgs := []string(nil), []string{"PROJ-2"}
			if format == "json" {
				args = []string{"--json"}
				ctxArgs = append(ctxArgs, "--json")
			}
			var err error
			out := captureStdout(t, func() { err = runPlan(args) })
			if err != nil {
				t.Fatalf("runPlan: %v", err)
			}
			if len(*requested) != 1 || (*requested)[0] != "PROJ-2" {
				t.Errorf("GetIssue keys = %v, want [PROJ-2]", *requested)
			}
			var cerr error
			want := captureStdout(t, func() { cerr = runContextGet(ctxArgs) })
			if cerr != nil {
				t.Fatalf("runContextGet: %v", cerr)
			}
			if out != want {
				t.Errorf("plan stdout differs from context get PROJ-2:\n got: %q\nwant: %q", out, want)
			}
		})
	}
}

func TestRunPlan_NoEpic(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Unrelated"}, "PROJ-1")})
	msg := planExpectFailure(t, nil)
	if !strings.Contains(msg, "jirahere plan init") || strings.Count(strings.TrimRight(msg, "\n"), "\n") != 0 {
		t.Errorf("stderr = %q, want one line naming `jirahere plan init`", msg)
	}
}

func TestRunPlan_NoEpic_IgnoresPlanMention(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Fix _Plan parsing"}, "PROJ-1")})
	msg := planExpectFailure(t, nil)
	if !strings.Contains(msg, `"_Plan Q3'26"`) || !strings.Contains(msg, "no plan epic found") {
		t.Errorf("stderr = %q, want not-found naming \"_Plan Q3'26\"", msg)
	}
}

func TestRunPlan_Ambiguous(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{
		"PROJ-1": "A _Plan Q3'26", "PROJ-2": "B _Plan Q3'26", "PROJ-3": "Other",
	}, "PROJ-1", "PROJ-2", "PROJ-3")})
	msg := planExpectFailure(t, nil)
	if !strings.Contains(msg, "PROJ-1") || !strings.Contains(msg, "PROJ-2") || strings.Contains(msg, "PROJ-3") ||
		strings.Count(strings.TrimRight(msg, "\n"), "\n") != 0 {
		t.Errorf("stderr = %q, want one line naming PROJ-1 and PROJ-2 only", msg)
	}
}

func TestRunPlan_FlagExclusionAndPositional(t *testing.T) {
	for _, args := range [][]string{{"--json", "--md"}, {"PROJ-1"}, {"--json", "extra"}} {
		var err error
		stdout := captureStdout(t, func() { _ = captureStderr(t, func() { err = runPlan(args) }) })
		var u *errUsage
		if !errors.As(err, &u) {
			t.Errorf("args %v: err = %v, want *errUsage", args, err)
		}
		if stdout != "" {
			t.Errorf("args %v: stdout = %q, want empty", args, stdout)
		}
	}
}

func TestRunPlan_Help(t *testing.T) {
	var err error
	out := captureStdout(t, func() { err = runPlan([]string{"--help"}) })
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(out, "json") {
		t.Errorf("err = %v, out = %q", err, out)
	}
}
