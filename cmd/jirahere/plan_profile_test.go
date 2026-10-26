package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/quarter"
)

const (
	planProfDefaultSettings = `{"defaults":{"project":"DEFPROJ","current_quarter":"DEF-Q"}}`
	planProfWorkSettings    = `{"defaults":{"project":"WORKPROJ","current_quarter":"WORK-Q"}}`
)

func planProfSetup(t *testing.T) (defaultCacheDir string) {
	t.Helper()
	defaultCacheDir = quarterListLogin(t)
	writeSettingsFile(t, planProfDefaultSettings)
	saveQuarterProfileLogin(t, "work", "work@example.com", "work-token")
	writeProfileSettings(t, "work", planProfWorkSettings)
	return defaultCacheDir
}

type planProfJira struct {
	mu          sync.Mutex
	auths       []string
	createmeta  []string
	posted      []string
	puts        []string
	requestPath []string
}

func (j *planProfJira) requests() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.auths)
}

func newPlanProfJira(t *testing.T) *planProfJira {
	t.Helper()
	j := &planProfJira{}
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j.mu.Lock()
		defer j.mu.Unlock()
		j.auths = append(j.auths, r.Header.Get("Authorization"))
		j.requestPath = append(j.requestPath, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			_, _ = w.Write([]byte(`{"issues":[]}`))
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			key := r.URL.Query().Get("projectKeys")
			j.createmeta = append(j.createmeta, key)
			id := "10000"
			if key == "WORKPROJ" {
				id = "20000"
			}
			_, _ = w.Write([]byte(`{"projects":[{"id":"` + id + `","key":"` + key + `","issuetypes":[{"id":"5","name":"Task","subtask":false},{"id":"6","name":"Epic","subtask":false}]}]}`))
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			j.posted = append(j.posted, string(raw))
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-77"}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") && r.Method == http.MethodGet:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":` + visionActiveFields + `}`))
		case r.Method == http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			j.puts = append(j.puts, string(raw))
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
	return j
}

func (j *planProfJira) assertAllAuth(t *testing.T, want string) {
	t.Helper()
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.auths) == 0 {
		t.Errorf("no live Jira requests were made, want at least one carrying the profile's login")
	}
	for i, got := range j.auths {
		if got != want {
			t.Errorf("live request %d (%s) Authorization = %q, want the work profile's login", i, j.requestPath[i], got)
		}
	}
}

func planProfCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runPlan(args) })
	})
	return stdout, stderr, err
}

func planProfInventory() []quarter.QuarterIssue {
	return visionInventory(map[string]string{"PROJ-2": "_Plan WORK-Q"}, "PROJ-2")
}

func planProfBodyFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte("the plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func planProfAssertWorkCache(t *testing.T, defaultCacheDir string) {
	t.Helper()
	if _, err := os.Stat(defaultCacheDir); !os.IsNotExist(err) {
		t.Errorf("default cache dir %s exists (err %v), want untouched", defaultCacheDir, err)
	}
	profCache, err := quarter.CacheDir("work")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(profCache)
	if err != nil || len(entries) != 1 {
		t.Errorf("profile cache dir %s entries = %v, err %v; want exactly one cache file", profCache, entries, err)
	}
}

func planProfAssertInventoryUse(t *testing.T, fake *fakeQuarterSearcher, seen []string, wantAuth string) {
	t.Helper()
	if fake.lastProject != "WORKPROJ" || fake.lastQuarter != "WORK-Q" {
		t.Errorf("searched (%q, %q), want (WORKPROJ, WORK-Q) from the profile's settings", fake.lastProject, fake.lastQuarter)
	}
	if len(seen) == 0 {
		t.Fatalf("inventory search never ran")
	}
	for _, got := range seen {
		if got != wantAuth {
			t.Errorf("inventory credentials = %q, want the work profile's login", got)
		}
	}
}

func TestRunPlan_Profile_Show_UsesProfileCredentialsSettingsAndCache(t *testing.T) {
	cacheDir := planProfSetup(t)
	fake := &fakeQuarterSearcher{issues: planProfInventory()}
	seen := stubQuarterSearcherRecordingAuth(t, fake)
	jira := newPlanProfJira(t)

	stdout, stderr, err := planProfCapture(t, "--profile", "work")
	if err != nil || stderr != "" {
		t.Fatalf("runPlan: %v (stderr %q)", err, stderr)
	}
	if !strings.HasPrefix(stdout, "# PROJ-2") {
		t.Errorf("stdout = %q, want the plan epic rendered", stdout)
	}
	want := basicAuth("work@example.com", "work-token")
	var got []string
	for _, c := range *seen {
		got = append(got, c.AuthHeader)
	}
	planProfAssertInventoryUse(t, fake, got, want)
	jira.assertAllAuth(t, want)
	planProfAssertWorkCache(t, cacheDir)
}

func TestRunPlanInit_Profile_UsesProfileCredentialsAndSettings(t *testing.T) {
	cacheDir := planProfSetup(t)
	fake := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Other"}, "PROJ-1")}
	seen := stubQuarterSearcherRecordingAuth(t, fake)
	jira := newPlanProfJira(t)

	stdout, stderr, err := planProfCapture(t, "init", "--profile", "work")
	if err != nil || stderr != "" {
		t.Fatalf("runPlan init: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Created a new Epic in WORKPROJ.") || !strings.Contains(stdout, "WORK-Q") {
		t.Errorf("stdout = %q, want an Epic created in WORKPROJ labelled WORK-Q", stdout)
	}
	want := basicAuth("work@example.com", "work-token")
	var got []string
	for _, c := range *seen {
		got = append(got, c.AuthHeader)
	}
	planProfAssertInventoryUse(t, fake, got, want)
	jira.assertAllAuth(t, want)
	if len(jira.createmeta) == 0 || jira.createmeta[0] != "WORKPROJ" {
		t.Errorf("createmeta asked for %v, want WORKPROJ", jira.createmeta)
	}
	if len(jira.posted) != 1 ||
		!strings.Contains(jira.posted[0], `"summary":"_Plan WORK-Q"`) ||
		!strings.Contains(jira.posted[0], `"labels":["WORK-Q"]`) ||
		!strings.Contains(jira.posted[0], `"20000"`) {
		t.Errorf("created body = %v, want _Plan WORK-Q / label WORK-Q in the WORKPROJ project (id 20000)", jira.posted)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("default cache dir %s exists (err %v), want untouched", cacheDir, err)
	}
}

func TestRunPlanInit_Profile_DryRunUsesProfileSettings(t *testing.T) {
	planProfSetup(t)
	fake := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-1": "Other"}, "PROJ-1")}
	stubQuarterSearcher(t, fake)
	jira := newPlanProfJira(t)

	stdout, stderr, err := planProfCapture(t, "init", "--dry-run", "--profile", "work")
	if err != nil || stderr != "" {
		t.Fatalf("err %v stderr %q", err, stderr)
	}
	if !strings.Contains(stdout, "in WORKPROJ") || !strings.Contains(stdout, `"_Plan WORK-Q"`) {
		t.Errorf("stdout = %q, want the dry run to name WORKPROJ and _Plan WORK-Q", stdout)
	}
	if fake.lastProject != "WORKPROJ" || fake.lastQuarter != "WORK-Q" || jira.requests() != 0 {
		t.Errorf("searched (%q, %q), live requests %d; want (WORKPROJ, WORK-Q) and 0", fake.lastProject, fake.lastQuarter, jira.requests())
	}
}

func TestRunPlanContext_Profile_UsesProfileCredentialsSettingsAndCache(t *testing.T) {
	cacheDir := planProfSetup(t)
	fake := &fakeQuarterSearcher{issues: planProfInventory()}
	seen := stubQuarterSearcherRecordingAuth(t, fake)
	jira := newPlanProfJira(t)

	stdout, stderr, err := planProfCapture(t, "context", "--day", "--json", "--profile", "work")
	if err != nil || stderr != "" {
		t.Fatalf("runPlan context: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, `"planEpic"`) || !strings.Contains(stdout, "PROJ-2") {
		t.Errorf("stdout = %q, want the bundle with the plan epic", stdout)
	}
	want := basicAuth("work@example.com", "work-token")
	var got []string
	for _, c := range *seen {
		got = append(got, c.AuthHeader)
	}
	planProfAssertInventoryUse(t, fake, got, want)
	jira.assertAllAuth(t, want)
	planProfAssertWorkCache(t, cacheDir)
}

func TestRunPlanContext_Profile_CacheIsolationAndRefresh(t *testing.T) {
	planProfSetup(t)
	newPlanProfJira(t)
	fake := &fakeQuarterSearcher{issues: planProfInventory()}
	stubQuarterSearcher(t, fake)

	for i := 0; i < 2; i++ {
		if _, stderr, err := planProfCapture(t, "context", "--day", "--profile", "work"); err != nil {
			t.Fatalf("work run %d: %v (stderr %q)", i, err, stderr)
		}
	}
	if fake.calls != 1 {
		t.Fatalf("searches after two work runs = %d, want 1 (warm work cache)", fake.calls)
	}

	if _, stderr, err := planProfCapture(t, "context", "--day"); err != nil {
		t.Fatalf("default run: %v (stderr %q)", err, stderr)
	}
	if fake.calls != 2 {
		t.Errorf("searches after default run = %d, want 2 (default does not read work's cache)", fake.calls)
	}
	if _, stderr, err := planProfCapture(t, "context", "--day", "--profile", "work", "--refresh"); err != nil {
		t.Fatalf("work refresh: %v (stderr %q)", err, stderr)
	}
	if fake.calls != 3 {
		t.Errorf("searches after work --refresh = %d, want 3", fake.calls)
	}
	if _, _, err := planProfCapture(t, "context", "--day", "--profile", "work"); err != nil || fake.calls != 3 {
		t.Errorf("work after refresh: err %v searches %d, want warm (3)", err, fake.calls)
	}
}

func TestRunPlanSave_Profile_UsesProfileCredentialsSettingsAndCache(t *testing.T) {

	orders := map[string][]string{
		"flag last":   {"day", "--body-file", "BODY", "--date", "2026-10-17", "--profile", "work"},
		"flag first":  {"--profile", "work", "day", "--body-file", "BODY", "--date", "2026-10-17"},
		"flag equals": {"day", "--profile=work", "--body-file", "BODY", "--date", "2026-10-17"},
	}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			cacheDir := planProfSetup(t)
			fake := &fakeQuarterSearcher{issues: planProfInventory()}
			seen := stubQuarterSearcherRecordingAuth(t, fake)
			jira := newPlanProfJira(t)
			body := planProfBodyFile(t)
			args := []string{"save"}
			for _, a := range order {
				if a == "BODY" {
					a = body
				}
				args = append(args, a)
			}

			stdout, stderr, err := planProfCapture(t, args...)
			if err != nil || stderr != "" {
				t.Fatalf("runPlan save: %v (stderr %q, stdout %q)", err, stderr, stdout)
			}
			want := basicAuth("work@example.com", "work-token")
			var got []string
			for _, c := range *seen {
				got = append(got, c.AuthHeader)
			}
			planProfAssertInventoryUse(t, fake, got, want)
			jira.assertAllAuth(t, want)
			if len(jira.posted) != 1 ||
				!strings.Contains(jira.posted[0], `"summary":"Plan Day 20261017"`) ||
				!strings.Contains(jira.posted[0], `"labels":["WORK-Q"]`) ||
				!strings.Contains(jira.posted[0], `"20000"`) {
				t.Errorf("created body = %v, want the plan item in WORKPROJ (id 20000) labelled WORK-Q", jira.posted)
			}
			planProfAssertWorkCache(t, cacheDir)
		})
	}
}

func TestRunPlan_NoProfile_UsesDefaultProfile(t *testing.T) {
	cases := map[string]func(t *testing.T) []string{
		"show":    func(*testing.T) []string { return nil },
		"init":    func(*testing.T) []string { return []string{"init", "--dry-run"} },
		"context": func(*testing.T) []string { return []string{"context", "--day"} },
		"save": func(t *testing.T) []string {
			return []string{"save", "day", "--body-file", planProfBodyFile(t), "--date", "2026-10-17"}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			planProfSetup(t)
			fake := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Plan DEF-Q"}, "PROJ-2")}
			if name == "init" {
				fake.issues = visionInventory(map[string]string{"PROJ-1": "Other"}, "PROJ-1")
			}
			seen := stubQuarterSearcherRecordingAuth(t, fake)
			jira := newPlanProfJira(t)

			if _, stderr, err := planProfCapture(t, mk(t)...); err != nil {
				t.Fatalf("err %v stderr %q", err, stderr)
			}
			if fake.lastProject != "DEFPROJ" || fake.lastQuarter != "DEF-Q" {
				t.Errorf("searched (%q, %q), want the default settings (DEFPROJ, DEF-Q)", fake.lastProject, fake.lastQuarter)
			}
			wantAuth := basicAuth("test@example.com", "test-token")
			for _, c := range *seen {
				if c.AuthHeader != wantAuth {
					t.Errorf("inventory credentials = %q, want the default login", c.AuthHeader)
				}
			}
			jira.mu.Lock()
			defer jira.mu.Unlock()
			for i, a := range jira.auths {
				if a != wantAuth {
					t.Errorf("live request %d Authorization = %q, want the default login", i, a)
				}
			}
		})
	}
}

func planProfUnknownCases(t *testing.T, name string) map[string][]string {
	body := planProfBodyFile(t)
	return map[string][]string{
		"show":         {"--profile", name},
		"init":         {"init", "--profile", name},
		"init dry-run": {"init", "--dry-run", "--profile", name},
		"context":      {"context", "--day", "--profile", name},
		"save":         {"save", "day", "--body-file", body, "--profile", name},
	}
}

func TestRunPlan_Profile_Unknown_FailsLikeVision(t *testing.T) {
	cases := planProfUnknownCases(t, "ghost")
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cacheDir := planProfSetup(t)
			fake := &fakeQuarterSearcher{issues: planProfInventory()}
			stubQuarterSearcher(t, fake)
			jira := newPlanProfJira(t)

			_, wantStderr, wantErr := runVisionCapture(t, "--profile", "ghost")
			var wantSilent *errSilent
			if !errors.As(wantErr, &wantSilent) {
				t.Fatalf("baseline vision err = %v, want *errSilent", wantErr)
			}
			if exp := "jirahere: not logged in (profile \"ghost\"). Run \"jirahere login --profile ghost\" first.\n"; wantStderr != exp {
				t.Fatalf("baseline vision stderr = %q, want %q", wantStderr, exp)
			}

			stdout, stderr, err := planProfCapture(t, args...)
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %v (%T), want *errSilent (exit 1) like vision", err, err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if stderr != wantStderr {
				t.Errorf("stderr = %q, want vision's %q", stderr, wantStderr)
			}
			if fake.calls != 0 || jira.requests() != 0 {
				t.Errorf("search calls = %d, live requests = %d, want 0 and 0", fake.calls, jira.requests())
			}
			if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
				t.Errorf("cache dir exists (err %v), want no cache I/O", err)
			}
			if profCache, err := quarter.CacheDir("ghost"); err == nil {
				if _, err := os.Stat(profCache); !os.IsNotExist(err) {
					t.Errorf("ghost cache dir exists (err %v), want none", err)
				}
			}
		})
	}
}

func TestRunPlan_Profile_NoLogin_FailsLikeVision(t *testing.T) {
	for name, args := range planProfUnknownCases(t, "empty") {
		t.Run(name, func(t *testing.T) {
			planProfSetup(t)
			writeProfileSettings(t, "empty", planProfWorkSettings)
			fake := &fakeQuarterSearcher{issues: planProfInventory()}
			stubQuarterSearcher(t, fake)
			jira := newPlanProfJira(t)

			_, wantStderr, _ := runVisionCapture(t, "--profile", "empty")
			stdout, stderr, err := planProfCapture(t, args...)
			var silent *errSilent
			if !errors.As(err, &silent) || stdout != "" || stderr != wantStderr {
				t.Errorf("err %v stdout %q stderr %q, want *errSilent, empty stdout, stderr %q", err, stdout, stderr, wantStderr)
			}
			if fake.calls != 0 || jira.requests() != 0 {
				t.Errorf("search calls = %d, live requests = %d, want 0 and 0", fake.calls, jira.requests())
			}
		})
	}
}

func TestRunPlan_Profile_UnsetQuarter_NoBorrowFromDefault(t *testing.T) {
	cases := map[string][]string{
		"show":    {"--profile", "work"},
		"init":    {"init", "--profile", "work"},
		"context": {"context", "--day", "--profile", "work"},
		"save":    nil,
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			planProfSetup(t)
			writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ"}}`)
			if name == "save" {
				args = []string{"save", "day", "--body-file", planProfBodyFile(t), "--profile", "work"}
			}
			fake := &fakeQuarterSearcher{issues: planProfInventory()}
			stubQuarterSearcher(t, fake)
			jira := newPlanProfJira(t)

			stdout, stderr, err := planProfCapture(t, args...)
			want := `jirahere: defaults.current_quarter is not set; add it to settings.json (run "jirahere configure set --profile work --current-quarter <label>").` + "\n"
			var silent *errSilent
			if !errors.As(err, &silent) || stdout != "" || stderr != want {
				t.Errorf("err %v stdout %q stderr %q, want *errSilent and stderr %q", err, stdout, stderr, want)
			}
			if fake.calls != 0 || jira.requests() != 0 {
				t.Errorf("search calls = %d, live requests = %d, want 0 and 0", fake.calls, jira.requests())
			}
		})
	}
}

func TestRunPlan_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
	names := map[string][]string{
		"empty":          {"--profile", ""},
		"missing value":  {"--profile"},
		"path traversal": {"--profile", "../evil"},
		"separator":      {"--profile", "a/b"},
		"whitespace":     {"--profile", "a b"},
		"control chars":  {"--profile", "a\x1b[31mb"},
	}

	bases := map[string]func(t *testing.T) []string{
		"show":    func(*testing.T) []string { return nil },
		"init":    func(*testing.T) []string { return []string{"init"} },
		"context": func(*testing.T) []string { return []string{"context", "--day"} },
		"save": func(t *testing.T) []string {
			return []string{"save", "day", "--body-file", planProfBodyFile(t)}
		},
	}
	for sub, base := range bases {
		for nname, extra := range names {
			t.Run(sub+"/"+nname, func(t *testing.T) {
				cacheDir := planProfSetup(t)
				fake := &fakeQuarterSearcher{issues: planProfInventory()}
				stubQuarterSearcher(t, fake)
				jira := newPlanProfJira(t)
				xdg := os.Getenv("XDG_CONFIG_HOME")
				before, _ := os.ReadDir(xdg)

				stdout, stderr, err := planProfCapture(t, append(base(t), extra...)...)

				var usage *errUsage
				if !errors.As(err, &usage) {
					t.Fatalf("err = %v (%T), want *errUsage (exit 2)", err, err)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want empty", stdout)
				}
				if !strings.Contains(stderr, "--profile") || strings.Contains(stderr, "\x1b") || strings.Contains(stderr, "evil") || strings.Contains(stderr, "not defined") {
					t.Errorf("stderr = %q, want a sanitized message naming --profile without echoing the name", stderr)
				}
				if fake.calls != 0 || jira.requests() != 0 {
					t.Errorf("search calls = %d, live requests = %d, want 0 and 0", fake.calls, jira.requests())
				}
				if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
					t.Errorf("cache dir exists (err %v), want no cache I/O", err)
				}
				if after, _ := os.ReadDir(xdg); len(after) != len(before) {
					t.Errorf("config dir changed (%d -> %d entries)", len(before), len(after))
				}
			})
		}
	}
}

func TestPlan_Profile_UsageConstsDocumentFlag(t *testing.T) {
	for name, usage := range map[string]string{
		"plan": planUsage, "plan init": planInitUsage, "plan context": planContextUsage, "plan save": planSaveUsage,
	} {
		if !strings.Contains(usage, "[--profile <name>]") {
			t.Errorf("%s usage = %q, want it to contain [--profile <name>]", name, usage)
		}
	}
}

func TestPlan_Profile_ProcessHelpListsFlag(t *testing.T) {
	for name, args := range map[string][]string{
		"plan":         {"plan", "--help"},
		"plan init":    {"plan", "init", "--help"},
		"plan context": {"plan", "context", "--help"},
		"plan save":    {"plan", "save", "--help"},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, args...)
			if exitCode != 0 || stderr != "" {
				t.Fatalf("--help: stderr=%q exit=%d, want exit 0 and empty stderr", stderr, exitCode)
			}
			if !strings.Contains(stdout, "--profile profile") || !strings.Contains(stdout, profileFlagUsage[:20]) {
				t.Errorf("--help stdout = %q, want the canonical --profile listing", stdout)
			}
		})
	}
}

func TestPlan_Profile_ProcessExitCodes(t *testing.T) {
	planProfSetup(t)
	body := planProfBodyFile(t)
	subs := map[string][]string{
		"show":    {"plan"},
		"init":    {"plan", "init"},
		"context": {"plan", "context", "--day"},
		"save":    {"plan", "save", "day", "--body-file", body},
	}

	_, visionBadErr, visionBadCode := runJirahereProcess(t, "vision", "--profile", "../evil")
	_, visionGhostErr, visionGhostCode := runJirahereProcess(t, "vision", "--profile", "ghost")
	if visionBadCode != 2 || visionGhostCode != 1 {
		t.Fatalf("baseline vision exit codes = %d, %d; want 2, 1", visionBadCode, visionGhostCode)
	}
	for name, args := range subs {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := runJirahereProcess(t, append(append([]string{}, args...), "--profile", "../evil")...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
				t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, code)
			}
			if stderr != visionBadErr {
				t.Errorf("bad name stderr = %q, want vision's %q", stderr, visionBadErr)
			}

			stdout, stderr, code = runJirahereProcess(t, append(append([]string{}, args...), "--profile", "ghost")...)
			if code != 1 || stdout != "" || stderr != visionGhostErr {
				t.Errorf("unknown profile: stdout=%q stderr=%q exit=%d, want empty stdout, vision's %q and exit 1", stdout, stderr, code, visionGhostErr)
			}
		})
	}
}
