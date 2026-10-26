package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

func singleLine(s string) bool { return strings.Count(strings.TrimRight(s, "\n"), "\n") == 0 }

func runSaveDay(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	return runPlanSaveCapture(t, "day", "--body-file", upsertBodyFile(t, upsertBody), "--date", "2026-10-17")
}

func assertPlainFailure(t *testing.T, err error, stderr string) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want failure")
	}
	var us *errUsage
	if errors.As(err, &us) {
		t.Errorf("got usage error (exit 2), want plain failure (exit 1): %v", err)
	}
	if !strings.HasPrefix(stderr, "jirahere: ") || !singleLine(stderr) {
		t.Errorf("stderr = %q, want one jirahere: line", stderr)
	}
}

func TestRunPlanSave_Failure_NoPlanEpicNamesInitAndWritesNothing(t *testing.T) {
	inv := []quarter.QuarterIssue{upsertItem("PROJ-10", "Unrelated work")}
	j, _ := upsertSetup(t, inv, nil)
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "jirahere plan init") {
		t.Errorf("stderr = %q, want the plan init remedy", stderr)
	}
	if stdout != "" || len(j.writes) != 0 {
		t.Errorf("stdout = %q writes = %+v, want nothing", stdout, j.writes)
	}
}

func TestRunPlanSave_Failure_AmbiguousEpicNamesEveryKeyAndWritesNothing(t *testing.T) {
	inv := upsertBase(upsertItem("PROJ-3", "Other _Plan Q3'26 epic"))
	j, _ := upsertSetup(t, inv, nil)
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	for _, k := range []string{upsertEpicKey, "PROJ-3"} {
		if !strings.Contains(stderr, k) {
			t.Errorf("stderr = %q, want %s named", stderr, k)
		}
	}
	if stdout != "" || len(j.writes) != 0 {
		t.Errorf("stdout = %q writes = %+v, want nothing", stdout, j.writes)
	}
}

func TestRunPlanSave_Failure_InventoryJiraErrorWritesNothing(t *testing.T) {
	j, f := upsertSetup(t, upsertBase(), nil)
	f.err = &jira.StatusError{StatusCode: 503}
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "plan save") || stdout != "" || len(j.writes) != 0 {
		t.Errorf("stdout = %q stderr = %q writes = %+v", stdout, stderr, j.writes)
	}
}

func TestRunPlanSave_Failure_NotLoggedInWritesNothing(t *testing.T) {

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettingsFile(t, planSettings)
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	orig := http.DefaultTransport
	http.DefaultTransport = createServerTransport{srv: srv, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })
	fq := &fakeQuarterSearcher{}
	stubQuarterSearcher(t, fq)

	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if n := reqs.Load(); n != 0 || fq.calls != 0 {
		t.Errorf("jira requests = %d, inventory searches = %d, want none", n, fq.calls)
	}
}

func TestRunPlanSave_Failure_UpdateReadFailsWritesNothing(t *testing.T) {
	inv := upsertBase(upsertItem("PROJ-50", "Plan Day 20261017"))
	j, _ := upsertSetup(t, inv, map[string]string{"PROJ-50": upsertEpicKey})
	j.fail = map[string]int{"GET /rest/api/3/issue/PROJ-50": http.StatusInternalServerError}
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "PROJ-50") || stdout != "" || len(j.writes) != 0 {
		t.Errorf("stdout = %q stderr = %q writes = %+v", stdout, stderr, j.writes)
	}
}

func TestRunPlanSave_Failure_CreateCallFailsNothingCreated(t *testing.T) {
	j, _ := upsertSetup(t, upsertBase(), nil)
	j.fail = map[string]int{"POST /rest/api/3/issue": http.StatusInternalServerError}
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "Nothing was created") {
		t.Errorf("stderr = %q, want \"Nothing was created\"", stderr)
	}
	if len(j.writes) != 1 {
		t.Errorf("writes = %+v, want only the failed POST", j.writes)
	}
}

func TestRunPlanSave_Failure_CreatePreflightFailsNothingCreated(t *testing.T) {
	j, _ := upsertSetup(t, upsertBase(), nil)
	j.fail = map[string]int{"GET /rest/api/3/issue/createmeta": http.StatusInternalServerError}
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "could not resolve project") {
		t.Errorf("stderr = %q, want the project-resolution failure", stderr)
	}
	if stdout != "" || len(j.writes) != 0 {
		t.Errorf("stdout = %q writes = %+v, want nothing created and no POST", stdout, j.writes)
	}
}

func TestRunPlanSave_Failure_IssueTypeUnavailableStaysSingleLine(t *testing.T) {
	j, _ := upsertSetup(t, upsertBase(), nil)
	j.createmeta = `{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"6","name":"Epic\nINJECTED","subtask":false}]}]}`
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "not available") {
		t.Errorf("stderr = %q, want the issue-type-unavailable failure", stderr)
	}
	if stdout != "" || len(j.writes) != 0 {
		t.Errorf("stdout = %q writes = %+v, want nothing", stdout, j.writes)
	}
}

func TestRunPlanSave_Failure_ParentSetAfterCreateThenRerunRepairs(t *testing.T) {
	inv := upsertBase()
	j, f := upsertSetup(t, inv, nil)
	j.fail = map[string]int{"PUT /rest/api/3/issue/PROJ-101": http.StatusInternalServerError}

	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if stdout != "PROJ-101\n" {
		t.Errorf("stdout = %q, want the bare key PROJ-101 only", stdout)
	}
	if strings.Contains(stdout, "Done.") {
		t.Errorf("stdout = %q, must not print Done. after a partial failure", stdout)
	}
	if !strings.Contains(stderr, "PROJ-101") || !strings.Contains(stderr, "parent") {
		t.Errorf("stderr = %q, want the failed parent step naming the key", stderr)
	}
	if n := len(j.writesTo(http.MethodPost)); n != 1 {
		t.Fatalf("POSTs = %d, want 1", n)
	}

	f.issues = append(inv, upsertItem("PROJ-101", "Plan Day 20261017"))
	j.mu.Lock()
	j.fail = nil
	j.issues["PROJ-101"] = ""
	j.writes = nil
	j.mu.Unlock()
	stdout, stderr, err = runSaveDay(t)
	if err != nil {
		t.Fatalf("re-run err = %v (stderr %q)", err, stderr)
	}
	if stdout != "Plan saved: PROJ-101 (updated)\n" {
		t.Errorf("re-run stdout = %q", stdout)
	}
	if n := len(j.writesTo(http.MethodPost)); n != 0 {
		t.Errorf("re-run POSTs = %d, want no second create", n)
	}
	got := j.fieldsPutTo(t, "PROJ-101")
	if p, _ := got["parent"].(map[string]any); p["key"] != upsertEpicKey {
		t.Errorf("re-run PUT fields = %v, want parent repaired to %s", got, upsertEpicKey)
	}
}

func TestRunPlanSave_Failure_UpdateDescriptionFailsNamesKeyNoFurtherSteps(t *testing.T) {

	inv := upsertBase(upsertItem("PROJ-51", "Plan Day 20261017"))
	j, _ := upsertSetup(t, inv, map[string]string{"PROJ-51": ""})
	j.fail = map[string]int{"PUT /rest/api/3/issue/PROJ-51": http.StatusInternalServerError}
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "PROJ-51") || !strings.Contains(stderr, "description") {
		t.Errorf("stderr = %q, want the description step and key", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (no Done.)", stdout)
	}
	if n := len(j.writes); n != 1 {
		t.Errorf("writes = %+v, want only the failed description PUT", j.writes)
	}
}

func TestRunPlanSave_Failure_UpdateParentSetFailsNamesKeyAndStep(t *testing.T) {

	inv := upsertBase(upsertItem("PROJ-52", "Plan Day 20261017"))
	j, _ := upsertSetup(t, inv, map[string]string{"PROJ-52": ""})
	j.fail = map[string]int{"PUT /rest/api/3/issue/PROJ-52#2": http.StatusInternalServerError}
	stdout, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "PROJ-52") || !strings.Contains(stderr, "parent") {
		t.Errorf("stderr = %q, want the parent step and key", stderr)
	}
	if strings.Contains(stdout, "Done.") || stdout != "" {
		t.Errorf("stdout = %q, want empty (no Done.)", stdout)
	}
	if n := len(j.writesTo(http.MethodPut)); n != 2 {
		t.Errorf("PUTs = %d, want the description then the failed parent set", n)
	}
}

func TestRunPlanSave_Failure_UpdateNeverPrintsDone(t *testing.T) {
	inv := upsertBase(upsertItem("PROJ-50", "Plan Day 20261017"))
	upsertSetup(t, inv, map[string]string{"PROJ-50": upsertEpicKey})
	stdout, _, err := runSaveDay(t)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "Done.") {
		t.Errorf("stdout = %q, update branch must not print Done.", stdout)
	}
}

func TestRunPlanSave_Failure_SettingsLoadErrorIsSingleLine(t *testing.T) {

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg\nforged: line\x1b[31m"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir, err := auth.ConfigDir("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "settings.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runSaveDay(t)
	assertPlainFailure(t, err, stderr)
	if !strings.Contains(stderr, "could not load settings") || strings.Contains(stderr, "\x1b") {
		t.Errorf("stderr = %q, want sanitized settings-load error", stderr)
	}
}

func TestParsePlanSaveArgs_DateErrorIsSingleLine(t *testing.T) {
	_, err := parsePlanSaveArgs([]string{"day", "--body-file", "-", "--date", "2026-10-17\nforged\x1b[31m"}, strings.NewReader("x"))
	if err == nil {
		t.Fatal("err = nil, want --date error")
	}
	if msg := err.Error(); !strings.Contains(msg, "--date") || strings.ContainsAny(msg, "\n\r\x1b") {
		t.Errorf("err = %q, want single-line --date error", msg)
	}
}
