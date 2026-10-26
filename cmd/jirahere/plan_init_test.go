package main

import (
	"errors"
	"flag"
	"io"
	"net/http"
	"strings"
	"testing"
)

func planInitServer(t *testing.T) (posted *string, requests *int) {
	t.Helper()
	posted = new(string)
	requests = new(int)
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		*requests++
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"6","name":"Epic"}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			*posted = string(body)
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-77"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return posted, requests
}

func TestRunPlanInit_Exists(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{
		"PROJ-1": "Other", "PROJ-2": "_Plan Q3'26",
	}, "PROJ-1", "PROJ-2")})
	_, requests := planInitServer(t)

	var err error
	out := captureStdout(t, func() { err = runPlan([]string{"init"}) })
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != "Plan epic exists: PROJ-2\n" {
		t.Errorf("stdout = %q", out)
	}
	if *requests != 0 {
		t.Errorf("Jira requests = %d, want 0", *requests)
	}
}

func TestRunPlanInit_IgnoresPlanMention(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Fix _Plan parsing"}, "PROJ-1")})
	posted, _ := planInitServer(t)

	var err error
	out := captureStdout(t, func() { err = runPlan([]string{"init"}) })
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "PROJ-77") || strings.Contains(out, "exists") {
		t.Errorf("stdout = %q, want a created epic", out)
	}
	if !strings.Contains(*posted, `"summary":"_Plan Q3'26"`) {
		t.Errorf("POST body %s missing plan epic summary", *posted)
	}
}

func TestRunPlanInit_BypassesCache(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	f := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Other"}, "PROJ-1")}
	stubQuarterSearcher(t, f)
	planInitServer(t)
	for i := 0; i < 2; i++ {
		_ = captureStdout(t, func() { _ = runPlan([]string{"init", "--dry-run"}) })
	}
	if f.calls != 2 {
		t.Errorf("SearchQuarterIssues calls = %d, want 2 (no cache)", f.calls)
	}
}

func TestRunPlanInit_Creates(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Other"}, "PROJ-1")})
	posted, _ := planInitServer(t)

	var err error
	var stderr string
	out := captureStdout(t, func() { stderr = captureStderr(t, func() { err = runPlan([]string{"init"}) }) })
	if err != nil || stderr != "" {
		t.Fatalf("err = %v, stderr = %q", err, stderr)
	}
	want := "Created a new Epic in PROJ.\n" +
		"PROJ-77\n" +
		"Labels: Q3'26. Q3'26 was auto-added for the current quarter.\n" +
		"Done.\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	for _, frag := range []string{`"summary":"_Plan Q3'26"`, `"labels":["Q3'26"]`, `"id":"6"`} {
		if !strings.Contains(*posted, frag) {
			t.Errorf("POST body %s missing %s", *posted, frag)
		}
	}
}

func TestRunPlanInit_Ambiguous(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{
		"PROJ-1": "A _Plan Q3'26", "PROJ-2": "B _Plan Q3'26", "PROJ-3": "Other",
	}, "PROJ-1", "PROJ-2", "PROJ-3")})
	_, requests := planInitServer(t)

	var err error
	var stderr string
	out := captureStdout(t, func() { stderr = captureStderr(t, func() { err = runPlan([]string{"init"}) }) })
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v, want *errSilent", err)
	}
	if out != "" || *requests != 0 {
		t.Errorf("stdout = %q, requests = %d, want none", out, *requests)
	}
	if !strings.Contains(stderr, "PROJ-1") || !strings.Contains(stderr, "PROJ-2") || strings.Contains(stderr, "PROJ-3") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRunPlanInit_DryRun(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Other"}, "PROJ-1")})
	_, requests := planInitServer(t)

	var err error
	out := captureStdout(t, func() { err = runPlan([]string{"init", "--dry-run"}) })
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if *requests != 0 {
		t.Errorf("requests = %d, want 0", *requests)
	}
	if !strings.Contains(out, `"_Plan Q3'26"`) || !strings.Contains(out, "Dry run") || strings.Contains(out, "PROJ-77") {
		t.Errorf("stdout = %q", out)
	}
}

func TestRunPlanInit_DryRunExists(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Plan Q3'26"}, "PROJ-2")})
	var err error
	out := captureStdout(t, func() { err = runPlanInit([]string{"--dry-run"}) })
	if err != nil || out != "Plan epic exists: PROJ-2\n" {
		t.Errorf("err = %v, out = %q", err, out)
	}
}

func TestRunPlanInit_MissingSettings(t *testing.T) {
	for name, tc := range map[string]struct{ settings, setting string }{
		"project": {`{"defaults":{"current_quarter":"Q3'26"}}`, "defaults.project"},
		"quarter": {`{"defaults":{"project":"PROJ"}}`, "defaults.current_quarter"},
	} {
		t.Run(name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, tc.settings)
			f := &fakeQuarterSearcher{}
			stubQuarterSearcher(t, f)
			_, requests := planInitServer(t)
			var err error
			var stderr string
			out := captureStdout(t, func() { stderr = captureStderr(t, func() { err = runPlanInit(nil) }) })
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %v, want *errSilent", err)
			}
			if !strings.Contains(stderr, tc.setting) || out != "" || f.calls != 0 || *requests != 0 {
				t.Errorf("stderr = %q, out = %q, calls = %d, requests = %d", stderr, out, f.calls, *requests)
			}
		})
	}
}

func TestRunPlanInit_UsageAndHelp(t *testing.T) {
	var err error
	_ = captureStdout(t, func() { _ = captureStderr(t, func() { err = runPlanInit([]string{"extra"}) }) })
	var u *errUsage
	if !errors.As(err, &u) {
		t.Errorf("err = %v, want *errUsage", err)
	}
	out := captureStdout(t, func() { err = runPlanInit([]string{"--help"}) })
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(out, "dry-run") {
		t.Errorf("err = %v, out = %q", err, out)
	}
}

func TestRunPlanInit_SearchError(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{err: errors.New("search boom")})
	_, requests := planInitServer(t)

	var err error
	var stderr string
	out := captureStdout(t, func() { stderr = captureStderr(t, func() { err = runPlanInit(nil) }) })
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v, want *errSilent", err)
	}
	if out != "" || *requests != 0 {
		t.Errorf("stdout = %q, requests = %d, want none", out, *requests)
	}
	if want := "jirahere: plan init: could not list the quarter's work items: Jira call failed unexpectedly.\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestRunPlanInit_CreateFails(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Other"}, "PROJ-1")})
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"6","name":"Epic"}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorMessages":["nope"]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	var err error
	var stderr string
	out := captureStdout(t, func() { stderr = captureStderr(t, func() { err = runPlanInit(nil) }) })
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v, want *errSilent", err)
	}
	if out != "" || !strings.Contains(stderr, "create failed") || !strings.Contains(stderr, "Nothing was created") {
		t.Errorf("stdout = %q, stderr = %q", out, stderr)
	}
}
