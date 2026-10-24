package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

const upsertEpicKey = "PROJ-2"

type upsertJira struct {
	mu     sync.Mutex
	issues map[string]string

	fail map[string]int

	createmeta string
	hits       map[string]int
	gets       []string
	writes     []upsertWrite
}

type upsertWrite struct {
	Method, Path string
	Body         map[string]any
}

func (j *upsertJira) writesTo(method string) []upsertWrite {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []upsertWrite
	for _, w := range j.writes {
		if w.Method == method {
			out = append(out, w)
		}
	}
	return out
}

func (j *upsertJira) fieldsPutTo(t *testing.T, key string) map[string]any {
	t.Helper()
	merged := map[string]any{}
	for _, w := range j.writesTo(http.MethodPut) {
		if w.Path != "/rest/api/3/issue/"+key {
			continue
		}
		f, _ := w.Body["fields"].(map[string]any)
		for k, v := range f {
			if old, dup := merged[k]; dup && !reflect.DeepEqual(old, v) {
				t.Errorf("field %q written twice with different values: %v then %v", k, old, v)
			}
			merged[k] = v
		}
		if len(w.Body) != 1 {
			t.Errorf("PUT %s body keys = %v, want only \"fields\"", key, w.Body)
		}
	}
	return merged
}

func newUpsertJira(t *testing.T, issues map[string]string) *upsertJira {
	t.Helper()
	j := &upsertJira{issues: issues}
	createTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		j.mu.Lock()
		defer j.mu.Unlock()
		if r.Method == http.MethodGet {
			j.gets = append(j.gets, r.URL.Path)
		}
		rk := r.Method + " " + r.URL.Path
		if j.hits == nil {
			j.hits = map[string]int{}
		}
		j.hits[rk]++
		st, ok := j.fail[fmt.Sprintf("%s#%d", rk, j.hits[rk])]
		if !ok {
			st, ok = j.fail[rk]
		}
		if ok {
			if r.Method != http.MethodGet {
				j.writes = append(j.writes, upsertWrite{r.Method, r.URL.Path, nil})
			}
			w.WriteHeader(st)
			return
		}
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta" && r.Method == http.MethodGet && j.createmeta != "":
			_, _ = w.Write([]byte(j.createmeta))
		case r.URL.Path == "/rest/api/3/issue/createmeta" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task","subtask":false},{"id":"6","name":"Epic","subtask":false}]}]}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") && r.Method == http.MethodGet:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			parent, ok := j.issues[key]
			if !ok {
				t.Errorf("GET of unknown issue %s", key)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			out := `{"id":"1","key":"` + key + `","fields":{"summary":"x","issuetype":{"name":"Task"}`
			if parent != "" {
				out += `,"parent":{"key":"` + parent + `","fields":{"summary":"p"}}`
			}
			_, _ = w.Write([]byte(out + `}}`))
		case r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete:
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Errorf("%s %s body is not JSON: %v", r.Method, r.URL.Path, err)
				}
			}
			j.writes = append(j.writes, upsertWrite{r.Method, r.URL.Path, body})
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"id":"10010","key":"PROJ-101"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return j
}

func upsertItem(key, summary string) quarter.QuarterIssue {
	return quarter.QuarterIssue{
		Key:     key,
		Summary: summary,
		Status:  jira.Status{Name: "To Do"},
		Created: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Updated: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	}
}

func upsertBase(extra ...quarter.QuarterIssue) []quarter.QuarterIssue {
	items := []quarter.QuarterIssue{
		upsertItem(upsertEpicKey, "FY26 _Plan Q3'26"),
		upsertItem("PROJ-10", "Unrelated work"),
		upsertItem("PROJ-11", "Plan Day 20261016"),
		upsertItem("PROJ-12", "Plan Week 20261018"),
		upsertItem("PROJ-13", "plan day 20261017"),
		upsertItem("PROJ-14", "Plan Day 20261017 extra"),
	}
	return append(items, extra...)
}

func upsertSetup(t *testing.T, inv []quarter.QuarterIssue, issues map[string]string) (*upsertJira, *fakeQuarterSearcher) {
	t.Helper()
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	f := &fakeQuarterSearcher{issues: inv}
	stubQuarterSearcher(t, f)
	if issues == nil {
		issues = map[string]string{}
	}
	issues[upsertEpicKey] = ""
	return newUpsertJira(t, issues), f
}

func upsertBodyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func runPlanSaveCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runPlan(append([]string{"save"}, args...)) })
	})
	return stdout, stderr, err
}

func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

const upsertBody = "line one\n\nline two\n"

func wantADF(t *testing.T) any {
	t.Helper()
	return asJSON(t, adf.FromPlainText("line one\n\nline two"))
}

func TestRunPlanSave_Upsert_CreatesWhenNoneExists(t *testing.T) {
	j, _ := upsertSetup(t, upsertBase(), nil)
	body := upsertBodyFile(t, upsertBody)

	stdout, stderr, err := runPlanSaveCapture(t, "day", "--body-file", body, "--date", "2026-10-17")
	if err != nil {
		t.Fatalf("err = %v (stderr %q)", err, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if !hasOwnLine(stdout, "PROJ-101") || strings.Contains(stdout, "Created") {
		t.Errorf("stdout = %q, want the bare new key on its own line", stdout)
	}
	if !strings.HasSuffix(stdout, "Done.\n") {
		t.Errorf("stdout = %q, want closing \"Done.\"", stdout)
	}

	posts := j.writesTo(http.MethodPost)
	if len(posts) != 1 || posts[0].Path != "/rest/api/3/issue" {
		t.Fatalf("POSTs = %+v, want exactly one create at /rest/api/3/issue", posts)
	}
	fields, _ := posts[0].Body["fields"].(map[string]any)
	want := map[string]any{
		"project":     map[string]any{"id": "10000"},
		"issuetype":   map[string]any{"id": "5"},
		"summary":     "Plan Day 20261017",
		"description": wantADF(t),

		"labels": []any{"Q3'26"},
	}
	if !reflect.DeepEqual(asJSON(t, fields), asJSON(t, want)) {

		delete(fields, "parent")
		if !reflect.DeepEqual(asJSON(t, fields), asJSON(t, want)) {
			t.Errorf("create fields = %v, want %v", fields, want)
		}
	}

	var parent any
	if p, ok := posts[0].Body["fields"].(map[string]any)["parent"]; ok {
		parent = p
	} else {
		parent = j.fieldsPutTo(t, "PROJ-101")["parent"]
	}
	if !reflect.DeepEqual(asJSON(t, parent), asJSON(t, map[string]any{"key": upsertEpicKey})) {
		t.Errorf("new item's parent = %v, want {key: %s}", parent, upsertEpicKey)
	}
	if puts := j.writesTo(http.MethodPut); len(puts) > 1 {
		t.Errorf("PUTs = %+v, want at most the single parent link", puts)
	}
}

func TestRunPlanSave_Upsert_SummaryRule(t *testing.T) {
	tests := []struct {
		kind, date, want string
	}{
		{"day", "2026-10-17", "Plan Day 20261017"},
		{"week", "2026-10-17", "Plan Week 20261017"},
		{"month", "2026-10-17", "Plan Month 20261017"},
		{"day", "2026-01-05", "Plan Day 20260105"},
		{"week", "2026-12-31", "Plan Week 20261231"},
		{"month", "2028-02-29", "Plan Month 20280229"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			j, _ := upsertSetup(t, upsertBase(), nil)
			_, stderr, err := runPlanSaveCapture(t, tc.kind, "--body-file", upsertBodyFile(t, upsertBody), "--date", tc.date)
			if err != nil {
				t.Fatalf("err = %v (stderr %q)", err, stderr)
			}
			posts := j.writesTo(http.MethodPost)
			if len(posts) != 1 {
				t.Fatalf("POSTs = %d, want 1", len(posts))
			}
			fields, _ := posts[0].Body["fields"].(map[string]any)
			if fields["summary"] != tc.want {
				t.Errorf("summary = %v, want %q", fields["summary"], tc.want)
			}
		})
	}
}

func TestRunPlanSave_Upsert_NoDateUsesToday(t *testing.T) {
	j, _ := upsertSetup(t, upsertBase(), nil)
	_, stderr, err := runPlanSaveCapture(t, "day", "--body-file", upsertBodyFile(t, upsertBody))
	if err != nil {
		t.Fatalf("err = %v (stderr %q)", err, stderr)
	}
	posts := j.writesTo(http.MethodPost)
	if len(posts) != 1 {
		t.Fatalf("POSTs = %d, want 1", len(posts))
	}
	fields, _ := posts[0].Body["fields"].(map[string]any)
	if want := "Plan Day " + time.Now().Format("20060102"); fields["summary"] != want {
		t.Errorf("summary = %v, want %q", fields["summary"], want)
	}
}

func TestRunPlanSave_Upsert_UpdatesExistingInPlace(t *testing.T) {

	inv := upsertBase(upsertItem("PROJ-50", "Plan Day 20261017"))
	j, _ := upsertSetup(t, inv, map[string]string{"PROJ-50": upsertEpicKey})

	stdout, stderr, err := runPlanSaveCapture(t, "day", "--body-file", upsertBodyFile(t, upsertBody), "--date", "2026-10-17")
	if err != nil {
		t.Fatalf("err = %v (stderr %q)", err, stderr)
	}
	if stdout != "Plan saved: PROJ-50 (updated)\n" || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want the updated line only", stdout, stderr)
	}
	if posts := j.writesTo(http.MethodPost); len(posts) != 0 {
		t.Errorf("POSTs = %+v, want no create call", posts)
	}
	if n := len(j.writes); n != 1 {
		t.Errorf("writes = %+v, want exactly one (the description PUT)", j.writes)
	}
	got := j.fieldsPutTo(t, "PROJ-50")
	if !reflect.DeepEqual(asJSON(t, got), asJSON(t, map[string]any{"description": wantADF(t)})) {
		t.Errorf("PUT fields = %v, want only the description", got)
	}
}

func TestRunPlanSave_Upsert_OrphanParentRepaired(t *testing.T) {

	inv := upsertBase(upsertItem("PROJ-50", "Plan Week 20261017 "), upsertItem("PROJ-51", "Plan Month 20261017"))
	j, _ := upsertSetup(t, inv, map[string]string{"PROJ-51": ""})

	stdout, stderr, err := runPlanSaveCapture(t, "month", "--body-file", upsertBodyFile(t, upsertBody), "--date", "2026-10-17")
	if err != nil {
		t.Fatalf("err = %v (stderr %q)", err, stderr)
	}
	if stdout != "Plan saved: PROJ-51 (updated)\n" || stderr != "" {
		t.Errorf("stdout/stderr = %q/%q, want the updated line only", stdout, stderr)
	}
	if posts := j.writesTo(http.MethodPost); len(posts) != 0 {
		t.Errorf("POSTs = %+v, want no create call", posts)
	}
	got := j.fieldsPutTo(t, "PROJ-51")
	want := map[string]any{"description": wantADF(t), "parent": map[string]any{"key": upsertEpicKey}}
	if !reflect.DeepEqual(asJSON(t, got), asJSON(t, want)) {
		t.Errorf("PUT fields = %v, want description + parent=%s and nothing else", got, upsertEpicKey)
	}
	for _, w := range j.writes {
		if w.Path != "/rest/api/3/issue/PROJ-51" {
			t.Errorf("write to %s %s, want only PROJ-51", w.Method, w.Path)
		}
	}
}

func TestRunPlanSave_Upsert_ExistingOtherParentLeftAlone(t *testing.T) {
	inv := upsertBase(upsertItem("PROJ-50", "Plan Day 20261017"))
	j, _ := upsertSetup(t, inv, map[string]string{"PROJ-50": "PROJ-99"})

	stdout, _, err := runPlanSaveCapture(t, "day", "--body-file", upsertBodyFile(t, upsertBody), "--date", "2026-10-17")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if stdout != "Plan saved: PROJ-50 (updated)\n" {
		t.Errorf("stdout = %q", stdout)
	}
	got := j.fieldsPutTo(t, "PROJ-50")
	if _, has := got["parent"]; has {
		t.Errorf("PUT fields = %v: a parent that is already set must not be touched", got)
	}
	if _, has := got["description"]; !has || len(got) != 1 {
		t.Errorf("PUT fields = %v, want only the description", got)
	}
}

func TestRunPlanSave_Upsert_AmbiguousWritesNothing(t *testing.T) {
	inv := upsertBase(upsertItem("PROJ-50", "Plan Day 20261017"), upsertItem("PROJ-60", "Plan Day 20261017"), upsertItem("PROJ-70", "Plan Day 20261017"))
	j, _ := upsertSetup(t, inv, map[string]string{"PROJ-50": "", "PROJ-60": "", "PROJ-70": ""})

	stdout, stderr, err := runPlanSaveCapture(t, "day", "--body-file", upsertBodyFile(t, upsertBody), "--date", "2026-10-17")
	if err == nil {
		t.Fatal("err = nil, want an ambiguity failure")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	for _, key := range []string{"PROJ-50", "PROJ-60", "PROJ-70"} {
		if !strings.Contains(stderr, key) {
			t.Errorf("stderr = %q, want it to name %s", stderr, key)
		}
	}
	for _, key := range []string{"PROJ-10", "PROJ-11", "PROJ-12", "PROJ-13", "PROJ-14", upsertEpicKey} {
		if strings.Contains(stderr, key) {
			t.Errorf("stderr = %q, names non-matching key %s", stderr, key)
		}
	}
	if !strings.Contains(stderr, "Plan Day 20261017") {
		t.Errorf("stderr = %q, want the summary named", stderr)
	}
	if len(j.writes) != 0 {
		t.Errorf("writes = %+v, want none", j.writes)
	}
	if strings.Count(strings.TrimRight(stderr, "\n"), "\n") != 0 {
		t.Errorf("stderr = %q, want a single line", stderr)
	}
}

func TestRunPlanSave_Upsert_UsesFreshInventory(t *testing.T) {
	j, f := upsertSetup(t, upsertBase(), map[string]string{"PROJ-50": upsertEpicKey})

	if _, err := quarter.Inventory(context.Background(), f, ""); err != nil {
		t.Fatal(err)
	}
	warm := f.calls

	f.issues = upsertBase(upsertItem("PROJ-50", "Plan Day 20261017"))

	stdout, stderr, err := runPlanSaveCapture(t, "day", "--body-file", upsertBodyFile(t, upsertBody), "--date", "2026-10-17")
	if err != nil {
		t.Fatalf("err = %v (stderr %q)", err, stderr)
	}
	if f.calls <= warm {
		t.Errorf("SearchQuarterIssues calls = %d (warm %d), want a fresh full fetch", f.calls, warm)
	}
	if stdout != "Plan saved: PROJ-50 (updated)\n" {
		t.Errorf("stdout = %q, want the update of the newly visible item", stdout)
	}
	if posts := j.writesTo(http.MethodPost); len(posts) != 0 {
		t.Errorf("POSTs = %+v, want no duplicate create", posts)
	}
}
