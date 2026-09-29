package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

var quarterListTZ = time.FixedZone("MST", -7*60*60)

func quarterListSample() []quarter.QuarterIssue {
	return []quarter.QuarterIssue{
		{
			Key:     "PROJ-2",
			Summary: "Ship the thing",
			Status:  jira.Status{Name: "In Progress"},
			Created: time.Date(2026, 7, 1, 10, 0, 0, 0, quarterListTZ),
			Updated: time.Date(2026, 8, 15, 12, 30, 0, 0, quarterListTZ),
		},
		{
			Key:     "PROJ-9",
			Summary: "Second item",
			Status:  jira.Status{Name: "Done"},
			Created: time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC),
			Updated: time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC),
		},
	}
}

type fakeQuarterSearcher struct {
	calls       int
	issues      []quarter.QuarterIssue
	err         error
	lastProject string
	lastQuarter string
}

func (f *fakeQuarterSearcher) SearchQuarterIssues(_ context.Context, project, quarterLabel string) ([]quarter.QuarterIssue, error) {
	f.calls++
	f.lastProject = project
	f.lastQuarter = quarterLabel
	return f.issues, f.err
}

func (f *fakeQuarterSearcher) LatestQuarterUpdate(_ context.Context, _, _ string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func stubQuarterSearcher(t *testing.T, f quarter.Searcher) {
	t.Helper()
	orig := newQuarterSearcher
	newQuarterSearcher = func(auth.Credentials) quarter.Searcher { return f }
	t.Cleanup(func() { newQuarterSearcher = orig })
}

func quarterListLogin(t *testing.T) (cacheDir string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: "test@example.com", Token: "test-token"},
	}); err != nil {
		t.Fatalf("auth.Save: %v", err)
	}
	return filepath.Join(cacheHome, "jirahere")
}

func TestRunQuarter_BadOrAbsentSubcommand(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"absent", nil},
		{"bad", []string{"bogus"}},
		{"list plus surplus positional", []string{"list", "extra"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runQuarter(tt.args) })
			if err == nil || !strings.Contains(err.Error(), quarterListUsage) {
				t.Fatalf("err = %v, want %q", err, quarterListUsage)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
		})
	}
}

func TestRunQuarter_HelpToken(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"list", "--help"}} {
		var err error
		out := captureStdout(t, func() { err = runQuarter(args) })
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("runQuarter(%v) err = %v, want flag.ErrHelp", args, err)
		}
		if !strings.Contains(out, "usage: jirahere quarter list") {
			t.Errorf("runQuarter(%v) help stdout = %q", args, out)
		}
	}
}

func TestRunQuarterList_NotLoggedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: quarterListSample()})

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runQuarterList(nil) })
	})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v, want not-logged-in failure", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one body-free error line", stderr)
	}
}

func TestRunQuarterList_ColdPathPopulatesCacheAndPrints(t *testing.T) {
	cacheDir := quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	fake := &fakeQuarterSearcher{issues: quarterListSample()}
	stubQuarterSearcher(t, fake)

	var err error
	out := captureStdout(t, func() { err = runQuarterList(nil) })
	if err != nil {
		t.Fatalf("runQuarterList: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("SearchQuarterIssues calls = %d, want 1 (cold path)", fake.calls)
	}

	wantLines := []string{
		"PROJ-2\tShip the thing\t2026-07-01T10:00:00-07:00\t2026-08-15T12:30:00-07:00\tIn Progress",
		"PROJ-9\tSecond item\t2026-07-05T09:00:00Z\t2026-07-06T09:00:00Z\tDone",
	}
	if out != strings.Join(wantLines, "\n")+"\n" {
		t.Errorf("stdout = %q, want %q", out, strings.Join(wantLines, "\n")+"\n")
	}

	entries, rderr := os.ReadDir(cacheDir)
	if rderr != nil {
		t.Fatalf("ReadDir(%s): %v", cacheDir, rderr)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "quarter-inventory-") && strings.HasSuffix(e.Name(), ".json") {
			found = true
		}
	}
	if !found {
		t.Errorf("cache dir %s has no quarter-inventory-*.json file: %v", cacheDir, entries)
	}
}

func TestRunQuarterList_WarmCacheDoesNotReachSearch(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	fake := &fakeQuarterSearcher{issues: quarterListSample()}
	stubQuarterSearcher(t, fake)

	var err error
	_ = captureStdout(t, func() { err = runQuarterList(nil) })
	if err != nil {
		t.Fatalf("cold runQuarterList: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("cold path calls = %d, want 1", fake.calls)
	}

	fake.calls = 0
	fake.err = errors.New("SearchQuarterIssues must not be called on a warm cache")
	out := captureStdout(t, func() { err = runQuarterList(nil) })
	if err != nil {
		t.Fatalf("warm runQuarterList: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("warm path calls = %d, want 0", fake.calls)
	}
	wantLines := []string{
		"PROJ-2\tShip the thing\t2026-07-01T10:00:00-07:00\t2026-08-15T12:30:00-07:00\tIn Progress",
		"PROJ-9\tSecond item\t2026-07-05T09:00:00Z\t2026-07-06T09:00:00Z\tDone",
	}
	if out != strings.Join(wantLines, "\n")+"\n" {
		t.Errorf("warm stdout = %q, want %q", out, strings.Join(wantLines, "\n")+"\n")
	}
}

func TestRunQuarterList_EmptyInventoryText(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: []quarter.QuarterIssue{}})

	var err error
	out := captureStdout(t, func() { err = runQuarterList(nil) })
	if err != nil {
		t.Fatalf("runQuarterList: %v", err)
	}
	if out != "no work items in FY26-Q1\n" {
		t.Errorf("stdout = %q, want %q", out, "no work items in FY26-Q1\n")
	}
}

func TestRunQuarterList_UnsetDefaults(t *testing.T) {
	for _, tt := range []struct {
		name     string
		settings string
		wantMsg  string
	}{
		{"project unset", `{"defaults":{"current_quarter":"FY26-Q1"}}`, "defaults.project is not set; add it to settings.json"},
		{"quarter unset", `{"defaults":{"project":"PROJ"}}`, "defaults.current_quarter is not set; add it to settings.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, tt.settings)
			fake := &fakeQuarterSearcher{issues: quarterListSample()}
			stubQuarterSearcher(t, fake)

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runQuarterList(nil) })
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("err = %v, want message containing %q", err, tt.wantMsg)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, tt.wantMsg) {
				t.Errorf("stderr = %q, want one line containing %q", stderr, tt.wantMsg)
			}
			if fake.calls != 0 {
				t.Errorf("SearchQuarterIssues calls = %d, want 0 (unset default aborts before search)", fake.calls)
			}
		})
	}
}

func TestRunQuarterList_JSONHappyPath(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: quarterListSample()})

	var err error
	out := captureStdout(t, func() { err = runQuarterList([]string{"--json"}) })
	if err != nil {
		t.Fatalf("runQuarterList --json: %v", err)
	}

	var doc struct {
		Items []struct {
			Key     string `json:"key"`
			Summary string `json:"summary"`
			Status  string `json:"status"`
			Created string `json:"created"`
			Updated string `json:"updated"`
		} `json:"items"`
	}
	if uerr := json.Unmarshal([]byte(out), &doc); uerr != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, out)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(doc.Items))
	}
	if doc.Items[0].Key != "PROJ-2" || doc.Items[1].Key != "PROJ-9" {
		t.Errorf("item order = [%q %q], want [PROJ-2 PROJ-9]", doc.Items[0].Key, doc.Items[1].Key)
	}
	if doc.Items[0].Summary != "Ship the thing" {
		t.Errorf("items[0].summary = %q", doc.Items[0].Summary)
	}
	if doc.Items[0].Status != "In Progress" || doc.Items[1].Status != "Done" {
		t.Errorf("item statuses = [%q %q], want [In Progress Done]", doc.Items[0].Status, doc.Items[1].Status)
	}
	if doc.Items[0].Created != "2026-07-01T10:00:00-07:00" || doc.Items[0].Updated != "2026-08-15T12:30:00-07:00" {
		t.Errorf("items[0] timestamps = (%q, %q), want RFC 3339 with preserved offset", doc.Items[0].Created, doc.Items[0].Updated)
	}
	if doc.Items[1].Created != "2026-07-05T09:00:00Z" {
		t.Errorf("items[1].created = %q, want 2026-07-05T09:00:00Z", doc.Items[1].Created)
	}

	dec := json.NewDecoder(strings.NewReader(out))
	var discard any
	if derr := dec.Decode(&discard); derr != nil {
		t.Fatalf("first decode: %v", derr)
	}
	if dec.More() {
		t.Errorf("stdout carries more than one JSON value: %q", out)
	}
}

func TestRunQuarterList_JSONEmptyInventory(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: []quarter.QuarterIssue{}})

	var err error
	out := captureStdout(t, func() { err = runQuarterList([]string{"--json"}) })
	if err != nil {
		t.Fatalf("runQuarterList --json: %v", err)
	}

	var raw map[string]json.RawMessage
	if uerr := json.Unmarshal([]byte(out), &raw); uerr != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, out)
	}
	if _, ok := raw["items"]; !ok || len(raw) != 1 {
		t.Fatalf("top-level keys = %v, want exactly {items}", keysOf(raw))
	}
	if strings.TrimSpace(string(raw["items"])) != "[]" {
		t.Errorf("items = %s, want []", raw["items"])
	}

	var doc quarterListDoc
	if uerr := json.Unmarshal([]byte(out), &doc); uerr != nil {
		t.Fatalf("unmarshal into quarterListDoc: %v", uerr)
	}
	if doc.Items == nil || len(doc.Items) != 0 {
		t.Errorf("doc.Items = %#v, want non-nil empty slice", doc.Items)
	}
}

func TestRunQuarterList_JSONUnsetDefaultStdoutEmpty(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"current_quarter":"FY26-Q1"}}`)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: quarterListSample()})

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runQuarterList([]string{"--json"}) })
	})
	if err == nil || !strings.Contains(err.Error(), "defaults.project is not set") {
		t.Fatalf("err = %v, want unset-project failure", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty under --json error path", out)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one body-free error line", stderr)
	}
	if strings.Contains(stderr, "{") {
		t.Errorf("stderr = %q, want a plain line, not JSON", stderr)
	}
}

func TestRunQuarterList_SanitizesJiraTextIdenticallyInBothForms(t *testing.T) {
	poisoned := "a\rb\nc\x1bd\u2028e\u202ef"
	sanitized := "abcdef"

	run := func(t *testing.T, jsonForm bool) string {
		quarterListLogin(t)
		writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
		stubQuarterSearcher(t, &fakeQuarterSearcher{issues: []quarter.QuarterIssue{{
			Key:     "PROJ-1" + poisoned,
			Summary: poisoned,
			Status:  jira.Status{Name: poisoned},
			Created: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
			Updated: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		}}})
		var err error
		var args []string
		if jsonForm {
			args = []string{"--json"}
		}
		out := captureStdout(t, func() { err = runQuarterList(args) })
		if err != nil {
			t.Fatalf("runQuarterList(json=%v): %v", jsonForm, err)
		}
		if !jsonForm {

			body := strings.TrimSuffix(out, "\n")
			for _, bad := range []rune{'\r', '\n', 0x1b, 0x2028, 0x202e} {
				if strings.ContainsRune(body, bad) {
					t.Errorf("text output retains poisoned rune %U: %q", bad, out)
				}
			}
		}
		return out
	}

	textOut := run(t, false)
	if textOut != "PROJ-1"+sanitized+"\t"+sanitized+"\t2026-07-01T10:00:00Z\t2026-07-01T10:00:00Z\t"+sanitized+"\n" {
		t.Errorf("text out = %q", textOut)
	}

	jsonOut := run(t, true)
	var doc quarterListDoc
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("json unmarshal: %v\n%q", err, jsonOut)
	}
	if doc.Items[0].Key != "PROJ-1"+sanitized || doc.Items[0].Summary != sanitized || doc.Items[0].Status != sanitized {
		t.Errorf("json item = {key:%q summary:%q status:%q}, want key %q summary/status %q", doc.Items[0].Key, doc.Items[0].Summary, doc.Items[0].Status, "PROJ-1"+sanitized, sanitized)
	}
}

func TestRunQuarterList_PreservesInventoryOrderNotResorted(t *testing.T) {
	scrambled := []quarter.QuarterIssue{
		{Key: "PROJ-3", Summary: "third-filed", Created: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), Updated: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{Key: "PROJ-1", Summary: "first-filed", Created: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Updated: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{Key: "PROJ-20", Summary: "twentieth-filed", Created: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), Updated: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)},
		{Key: "PROJ-2", Summary: "second-filed", Created: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), Updated: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)},
	}
	wantOrder := []string{"PROJ-3", "PROJ-1", "PROJ-20", "PROJ-2"}

	t.Run("text", func(t *testing.T) {
		quarterListLogin(t)
		writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
		stubQuarterSearcher(t, &fakeQuarterSearcher{issues: scrambled})

		var err error
		out := captureStdout(t, func() { err = runQuarterList(nil) })
		if err != nil {
			t.Fatalf("runQuarterList: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		if len(lines) != len(wantOrder) {
			t.Fatalf("got %d lines, want %d: %q", len(lines), len(wantOrder), out)
		}
		for i, want := range wantOrder {
			if got := strings.SplitN(lines[i], "\t", 2)[0]; got != want {
				t.Errorf("line %d key = %q, want %q (order re-sorted?)", i, got, want)
			}
		}
	})

	t.Run("json", func(t *testing.T) {
		quarterListLogin(t)
		writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
		stubQuarterSearcher(t, &fakeQuarterSearcher{issues: scrambled})

		var err error
		out := captureStdout(t, func() { err = runQuarterList([]string{"--json"}) })
		if err != nil {
			t.Fatalf("runQuarterList --json: %v", err)
		}
		var doc quarterListDoc
		if uerr := json.Unmarshal([]byte(out), &doc); uerr != nil {
			t.Fatalf("unmarshal: %v\n%q", uerr, out)
		}
		if len(doc.Items) != len(wantOrder) {
			t.Fatalf("got %d items, want %d", len(doc.Items), len(wantOrder))
		}
		for i, want := range wantOrder {
			if doc.Items[i].Key != want {
				t.Errorf("items[%d].key = %q, want %q (order re-sorted?)", i, doc.Items[i].Key, want)
			}
		}
	})
}

func TestRunQuarterList_InventoryErrorStdoutEmptyBothForms(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"text", nil},
		{"json", []string{"--json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
			stubQuarterSearcher(t, &fakeQuarterSearcher{err: errors.New("dial tcp: connection refused")})

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runQuarterList(tt.args) })
			})
			if err == nil || !strings.Contains(err.Error(), "quarter list failed") {
				t.Fatalf("err = %v, want a 'quarter list failed' failure", err)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty on the Inventory-error path", out)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
			if strings.Contains(stderr, "connection refused") {
				t.Errorf("stderr = %q leaks the raw transport error", stderr)
			}
		})
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

const quarterListFlagsSettings = `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1","previous_quarter":"FY25-Q4","next_quarter":"FY26-Q2"}}`

func TestRunQuarterList_PreviousNextLabelFlags_HappyPath(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		wantLabel string
	}{
		{"previous", []string{"--previous"}, "FY25-Q4"},
		{"next", []string{"--next"}, "FY26-Q2"},
		{"label", []string{"--label", "FY30-Q9"}, "FY30-Q9"},
	} {
		t.Run(tt.name+"/text", func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, quarterListFlagsSettings)
			fake := &fakeQuarterSearcher{issues: []quarter.QuarterIssue{}}
			stubQuarterSearcher(t, fake)

			var err error
			out := captureStdout(t, func() { err = runQuarterList(tt.args) })
			if err != nil {
				t.Fatalf("runQuarterList(%v): %v", tt.args, err)
			}
			if fake.calls != 1 {
				t.Fatalf("SearchQuarterIssues calls = %d, want 1", fake.calls)
			}
			if fake.lastQuarter != tt.wantLabel {
				t.Errorf("InventoryForQuarter quarter = %q, want %q", fake.lastQuarter, tt.wantLabel)
			}
			if fake.lastProject != "PROJ" {
				t.Errorf("InventoryForQuarter project = %q, want %q", fake.lastProject, "PROJ")
			}
			wantOut := "no work items in " + tt.wantLabel + "\n"
			if out != wantOut {
				t.Errorf("stdout = %q, want %q", out, wantOut)
			}
		})

		t.Run(tt.name+"/json", func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, quarterListFlagsSettings)
			fake := &fakeQuarterSearcher{issues: quarterListSample()}
			stubQuarterSearcher(t, fake)

			args := append(append([]string{}, tt.args...), "--json")
			var err error
			out := captureStdout(t, func() { err = runQuarterList(args) })
			if err != nil {
				t.Fatalf("runQuarterList(%v): %v", args, err)
			}
			if fake.lastQuarter != tt.wantLabel {
				t.Errorf("InventoryForQuarter quarter = %q, want %q", fake.lastQuarter, tt.wantLabel)
			}

			var raw map[string]json.RawMessage
			if uerr := json.Unmarshal([]byte(out), &raw); uerr != nil {
				t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, out)
			}
			if _, ok := raw["items"]; !ok || len(raw) != 1 {
				t.Fatalf("top-level keys = %v, want exactly {items}", keysOf(raw))
			}
			var doc quarterListDoc
			if uerr := json.Unmarshal([]byte(out), &doc); uerr != nil {
				t.Fatalf("unmarshal into quarterListDoc: %v", uerr)
			}
			if len(doc.Items) != 2 {
				t.Errorf("items = %d, want 2", len(doc.Items))
			}
			if len(doc.Items) == 2 && (doc.Items[0].Status != "In Progress" || doc.Items[1].Status != "Done") {
				t.Errorf("item statuses = [%q %q], want [In Progress Done]", doc.Items[0].Status, doc.Items[1].Status)
			}
		})
	}
}

func TestRunQuarterList_RendersStatusForEveryQuarterSource(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"default", nil},
		{"previous", []string{"--previous"}},
		{"next", []string{"--next"}},
		{"label", []string{"--label", "FY30-Q9"}},
	} {
		t.Run(tt.name+"/text", func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, quarterListFlagsSettings)
			stubQuarterSearcher(t, &fakeQuarterSearcher{issues: quarterListSample()})

			var err error
			out := captureStdout(t, func() { err = runQuarterList(tt.args) })
			if err != nil {
				t.Fatalf("runQuarterList(%v): %v", tt.args, err)
			}
			if !strings.Contains(out, "\tIn Progress\n") || !strings.Contains(out, "\tDone\n") {
				t.Errorf("stdout = %q, want both status columns", out)
			}
		})

		t.Run(tt.name+"/json", func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, quarterListFlagsSettings)
			stubQuarterSearcher(t, &fakeQuarterSearcher{issues: quarterListSample()})

			args := append(append([]string{}, tt.args...), "--json")
			var err error
			out := captureStdout(t, func() { err = runQuarterList(args) })
			if err != nil {
				t.Fatalf("runQuarterList(%v): %v", args, err)
			}
			var doc quarterListDoc
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("unmarshal: %v\\n%q", err, out)
			}
			if len(doc.Items) != 2 || doc.Items[0].Status != "In Progress" || doc.Items[1].Status != "Done" {
				t.Errorf("items = %#v, want statuses [In Progress Done]", doc.Items)
			}
		})
	}
}

func TestRunQuarterList_MultipleQuarterFlagsGiven(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"previous+next", []string{"--previous", "--next"}},
		{"previous+label", []string{"--previous", "--label", "FY30-Q9"}},
		{"next+label", []string{"--next", "--label", "FY30-Q9"}},
		{"all three", []string{"--previous", "--next", "--label", "FY30-Q9"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, quarterListFlagsSettings)
			fake := &fakeQuarterSearcher{issues: quarterListSample()}
			stubQuarterSearcher(t, fake)

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runQuarterList(tt.args) })
			})
			if err == nil || !strings.Contains(err.Error(), quarterListUsage) {
				t.Fatalf("err = %v, want usage error containing %q", err, quarterListUsage)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
			if fake.calls != 0 {
				t.Errorf("SearchQuarterIssues calls = %d, want 0 (usage error aborts before any Jira call)", fake.calls)
			}
		})
	}
}

func TestRunQuarterList_PreviousNextUnsetSetting(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		settings string
		wantMsg  string
	}{
		{
			"previous unset",
			[]string{"--previous"},
			`{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`,
			"defaults.previous_quarter is not set; add it to settings.json",
		},
		{
			"next unset",
			[]string{"--next"},
			`{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`,
			"defaults.next_quarter is not set; add it to settings.json",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, tt.settings)
			fake := &fakeQuarterSearcher{issues: quarterListSample()}
			stubQuarterSearcher(t, fake)

			var err error
			var stderr string
			out := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runQuarterList(tt.args) })
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("err = %v, want message containing %q", err, tt.wantMsg)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, tt.wantMsg) {
				t.Errorf("stderr = %q, want one line containing %q", stderr, tt.wantMsg)
			}
			if fake.calls != 0 {
				t.Errorf("SearchQuarterIssues calls = %d, want 0 (unset default aborts before any Jira call)", fake.calls)
			}
		})
	}
}

func TestRunQuarterList_EmptyLabelFlag(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, quarterListFlagsSettings)
	fake := &fakeQuarterSearcher{issues: quarterListSample()}
	stubQuarterSearcher(t, fake)

	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runQuarterList([]string{"--label", ""}) })
	})
	wantMsg := "jirahere: --label was given but is empty; an empty value is not allowed."
	if err == nil || !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("err = %v, want message containing %q", err, wantMsg)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one body-free error line", stderr)
	}
	if fake.calls != 0 {
		t.Errorf("SearchQuarterIssues calls = %d, want 0 (empty --label aborts before any Jira call)", fake.calls)
	}
}

func TestRunQuarterList_NoQuarterFlagsUnchanged(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, quarterListFlagsSettings)
	fake := &fakeQuarterSearcher{issues: []quarter.QuarterIssue{}}
	stubQuarterSearcher(t, fake)

	var err error
	out := captureStdout(t, func() { err = runQuarterList(nil) })
	if err != nil {
		t.Fatalf("runQuarterList: %v", err)
	}
	if fake.lastQuarter != "FY26-Q1" {
		t.Errorf("Inventory quarter = %q, want defaults.current_quarter %q", fake.lastQuarter, "FY26-Q1")
	}
	if out != "no work items in FY26-Q1\n" {
		t.Errorf("stdout = %q, want %q", out, "no work items in FY26-Q1\n")
	}
}

func TestRunQuarterList_ExplicitFalseQuarterFlagsUnchanged(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"previous=false", []string{"--previous=false"}},
		{"next=false", []string{"--next=false"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, quarterListFlagsSettings)
			fake := &fakeQuarterSearcher{issues: []quarter.QuarterIssue{}}
			stubQuarterSearcher(t, fake)

			var err error
			out := captureStdout(t, func() { err = runQuarterList(tt.args) })
			if err != nil {
				t.Fatalf("runQuarterList(%v): %v", tt.args, err)
			}
			if fake.lastQuarter != "FY26-Q1" {
				t.Errorf("Inventory quarter = %q, want defaults.current_quarter %q", fake.lastQuarter, "FY26-Q1")
			}
			if out != "no work items in FY26-Q1\n" {
				t.Errorf("stdout = %q, want %q", out, "no work items in FY26-Q1\n")
			}
		})
	}
}
