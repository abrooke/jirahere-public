package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/quarter"
)

func runVisionCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runVision(args) })
	})
	return stdout, stderr, err
}

func visionAuthServer(t *testing.T) *[]string {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(visionChildrenBody))
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":` + visionActiveFields + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
	return &auths
}

func TestRunVision_Profile_UsesProfileCredentialsSettingsAndCache(t *testing.T) {
	cacheDir := quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ","current_quarter":"DEF-Q"}}`)
	saveQuarterProfileLogin(t, "work", "work@example.com", "work-token")
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ","current_quarter":"WORK-Q"}}`)

	fake := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")}
	seen := stubQuarterSearcherRecordingAuth(t, fake)
	auths := visionAuthServer(t)

	stdout, stderr, err := runVisionCapture(t, "--profile", "work")
	if err != nil {
		t.Fatalf("runVision: %v (stderr %q)", err, stderr)
	}
	if !strings.HasPrefix(stdout, "# PROJ-12 — Active vision item") || stderr != "" {
		t.Errorf("stdout = %q stderr = %q, want the vision item and empty stderr", stdout, stderr)
	}
	if fake.lastProject != "WORKPROJ" || fake.lastQuarter != "WORK-Q" {
		t.Errorf("searched (%q, %q), want (WORKPROJ, WORK-Q)", fake.lastProject, fake.lastQuarter)
	}
	wantAuth := basicAuth("work@example.com", "work-token")
	if len(*seen) != 1 || (*seen)[0].AuthHeader != wantAuth {
		t.Errorf("inventory credentials = %+v, want the work profile's login", *seen)
	}
	if len(*auths) != 2 {
		t.Fatalf("live Jira requests = %d, want 2 (children, issue)", len(*auths))
	}
	for i, got := range *auths {
		if got != wantAuth {
			t.Errorf("live request %d Authorization = %q, want the work profile's login", i, got)
		}
	}

	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("default cache dir %s exists (err %v), want untouched", cacheDir, err)
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

func TestRunVision_Profile_CacheAndSettingsIsolation(t *testing.T) {
	quarterListLogin(t)
	const settingsBody = `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q3"}}`
	writeSettingsFile(t, settingsBody)
	for _, p := range []string{"a", "b"} {
		saveQuarterProfileLogin(t, p, p+"@example.com", p+"-token")
		writeProfileSettings(t, p, settingsBody)
	}
	visionAuthServer(t)

	fakeA := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"A-1": "_Vision from a"}, "A-1")}
	stubQuarterSearcher(t, fakeA)
	if _, stderr, err := runVisionCapture(t, "--profile", "a"); err != nil || fakeA.calls != 1 {
		t.Fatalf("profile a: err %v (stderr %q), search calls %d, want 1", err, stderr, fakeA.calls)
	}
	if _, _, err := runVisionCapture(t, "--profile", "a"); err != nil || fakeA.calls != 1 {
		t.Fatalf("second profile a run: err %v, search calls %d, want warm cache (1)", err, fakeA.calls)
	}

	fakeB := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"B-1": "no epic here"}, "B-1")}
	stubQuarterSearcher(t, fakeB)
	_, stderr, err := runVisionCapture(t, "--profile", "b")
	if err == nil || fakeB.calls != 1 || !strings.Contains(stderr, "no Vision epic found") {
		t.Errorf("profile b: err %v stderr %q calls %d; want its own cold search and no-epic failure, not a's cache", err, stderr, fakeB.calls)
	}
	fakeD := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"D-1": "_Vision from default"}, "D-1")}
	stubQuarterSearcher(t, fakeD)
	if _, stderr, err := runVisionCapture(t); err != nil || fakeD.calls != 1 {
		t.Errorf("default: err %v stderr %q calls %d; want its own cold search", err, stderr, fakeD.calls)
	}

	stubQuarterSearcher(t, &fakeQuarterSearcher{err: errors.New("must not search")})
	if _, stderr, err := runVisionCapture(t, "--profile", "a"); err != nil {
		t.Errorf("profile a after others: err %v stderr %q, want warm success", err, stderr)
	}

	writeProfileSettings(t, "b", `{"defaults":{"project":"PROJ"}}`)
	fakeB2 := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"B-1": "_Vision"}, "B-1")}
	stubQuarterSearcher(t, fakeB2)
	_, stderr, err = runVisionCapture(t, "--profile", "b")
	want := `jirahere: defaults.current_quarter is not set; add it to settings.json (run "jirahere configure set --profile b --current-quarter <label>").` + "\n"
	if err == nil || stderr != want || fakeB2.calls != 0 {
		t.Errorf("unset quarter under b: err %v stderr %q calls %d, want %q and no search", err, stderr, fakeB2.calls, want)
	}
}

func TestRunVision_Profile_NotLoggedIn(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, visionSettings)
	writeProfileSettings(t, "empty", `{"defaults":{"project":"P","current_quarter":"Q"}}`)
	fake := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")}
	stubQuarterSearcher(t, fake)
	auths := visionAuthServer(t)

	stdout, stderr, err := runVisionCapture(t, "--profile", "empty")
	want := "jirahere: not logged in (profile \"empty\"). Run \"jirahere login --profile empty\" first.\n"
	if err == nil || stdout != "" || stderr != want {
		t.Errorf("err %v stdout %q stderr %q, want failure with stderr %q", err, stdout, stderr, want)
	}
	if fake.calls != 0 || len(*auths) != 0 {
		t.Errorf("search calls = %d, live requests = %d, want 0 and 0", fake.calls, len(*auths))
	}
}

func TestRunVision_NoProfile_MessagesUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettingsFile(t, visionSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{})
	_, stderr, err := runVisionCapture(t)
	if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; err == nil || stderr != want {
		t.Errorf("not logged in: err %v stderr %q, want %q", err, stderr, want)
	}

	saveQuarterProfileLogin(t, "", "d@example.com", "d-token")
	writeSettingsFile(t, `{"defaults":{"project":"PROJ"}}`)
	_, stderr, err = runVisionCapture(t)
	if want := "jirahere: defaults.current_quarter is not set; add it to settings.json.\n"; err == nil || stderr != want {
		t.Errorf("unset quarter: err %v stderr %q, want %q", err, stderr, want)
	}
}

func TestRunVision_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", []string{"--profile", ""}},
		{"missing value", []string{"--profile"}},
		{"path traversal", []string{"--profile", "../evil"}},
		{"separator", []string{"--profile", "a/b"}},
		{"leading dot", []string{"--profile", ".hidden"}},
		{"whitespace", []string{"--profile", "a b"}},
		{"too long", []string{"--profile", strings.Repeat("a", 65)}},
		{"control chars", []string{"--profile", "a\x1b[31mb"}},
		{"with json", []string{"--json", "--profile", "../evil"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cacheDir := quarterListLogin(t)
			writeSettingsFile(t, visionSettings)
			fake := &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")}
			stubQuarterSearcher(t, fake)
			auths := visionAuthServer(t)
			xdg := os.Getenv("XDG_CONFIG_HOME")
			before, _ := os.ReadDir(xdg)

			stdout, stderr, err := runVisionCapture(t, tc.args...)

			var usage *errUsage
			if !errors.As(err, &usage) {
				t.Fatalf("err = %v (%T), want *errUsage (exit 2)", err, err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "--profile") || strings.Count(stderr, "\n") != 1 ||
				strings.Contains(stderr, "\x1b") || strings.Contains(stderr, "evil") {
				t.Errorf("stderr = %q, want one sanitized line naming --profile without echoing the name", stderr)
			}
			if fake.calls != 0 || len(*auths) != 0 {
				t.Errorf("search calls = %d, live requests = %d, want 0 and 0", fake.calls, len(*auths))
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

func TestRunVision_Profile_OutputShapeUnchanged(t *testing.T) {
	quarterListLogin(t)
	saveQuarterProfileLogin(t, "work", "w@example.com", "w-token")
	writeProfileSettings(t, "work", `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q3"}}`)
	writeSettingsFile(t, visionSettings)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: visionInventory(map[string]string{"PROJ-2": "_Vision"}, "PROJ-2")})
	visionAuthServer(t)

	for _, extra := range [][]string{nil, {"--json"}, {"--md"}} {
		viaProfile, _, err := runVisionCapture(t, append([]string{"--profile", "work"}, extra...)...)
		if err != nil {
			t.Fatal(err)
		}
		viaDefault, _, err := runVisionCapture(t, extra...)
		if err != nil {
			t.Fatal(err)
		}
		if viaProfile != viaDefault {
			t.Errorf("%v: output under profile differs from default for the same inventory:\n%q\n%q", extra, viaProfile, viaDefault)
		}
	}
}

func TestVision_Profile_ProcessExitCodeAndHelp(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "vision", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "vision", "--help")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("--help: stderr=%q exit=%d, want exit 0 and empty stderr", stderr, exitCode)
	}
	if !strings.Contains(stdout, "--profile profile") || !strings.Contains(stdout, profileFlagUsage[:20]) {
		t.Errorf("--help stdout = %q, want the canonical --profile listing", stdout)
	}
}
