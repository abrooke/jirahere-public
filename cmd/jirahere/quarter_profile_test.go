package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

func stubQuarterSearcherRecordingAuth(t *testing.T, f quarter.Searcher) *[]auth.Credentials {
	t.Helper()
	var seen []auth.Credentials
	orig := newQuarterSearcher
	newQuarterSearcher = func(c auth.Credentials) quarter.Searcher {
		seen = append(seen, c)
		return f
	}
	t.Cleanup(func() { newQuarterSearcher = orig })
	return &seen
}

func runQuarterListCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runQuarterList(args) })
	})
	return stdout, stderr, err
}

func saveQuarterProfileLogin(t *testing.T, prof, email, token string) {
	t.Helper()
	if err := auth.Save(prof, &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: email, Token: token},
	}); err != nil {
		t.Fatalf("auth.Save(%q): %v", prof, err)
	}
}

func TestRunQuarterList_Profile_UsesProfileCredentialsSettingsAndCache(t *testing.T) {
	cacheDir := quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ","current_quarter":"DEF-Q"}}`)
	saveQuarterProfileLogin(t, "work", "work@example.com", "work-token")
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ","current_quarter":"WORK-Q"}}`)

	fake := &fakeQuarterSearcher{issues: quarterListSample()}
	seen := stubQuarterSearcherRecordingAuth(t, fake)

	stdout, stderr, err := runQuarterListCapture(t, "--profile", "work")
	if err != nil {
		t.Fatalf("runQuarterList: %v (stderr %q)", err, stderr)
	}
	if stdout == "" || stderr != "" {
		t.Errorf("stdout = %q stderr = %q, want inventory and empty stderr", stdout, stderr)
	}
	if fake.lastProject != "WORKPROJ" || fake.lastQuarter != "WORK-Q" {
		t.Errorf("searched (%q, %q), want (WORKPROJ, WORK-Q)", fake.lastProject, fake.lastQuarter)
	}
	if len(*seen) != 1 || !strings.Contains((*seen)[0].AuthHeader, basicAuth("work@example.com", "work-token")) {
		t.Errorf("credentials = %+v, want the work profile's login", *seen)
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

func TestRunQuarterList_Profile_CacheIsolation(t *testing.T) {
	quarterListLogin(t)
	const settingsBody = `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`
	writeSettingsFile(t, settingsBody)
	for _, p := range []string{"a", "b"} {
		saveQuarterProfileLogin(t, p, p+"@example.com", p+"-token")
		writeProfileSettings(t, p, settingsBody)
	}

	fakeA := &fakeQuarterSearcher{issues: quarterListSample()}
	stubQuarterSearcher(t, fakeA)
	outA, _, err := runQuarterListCapture(t, "--profile", "a")
	if err != nil {
		t.Fatal(err)
	}
	if fakeA.calls != 1 {
		t.Fatalf("profile a search calls = %d, want 1", fakeA.calls)
	}

	if _, _, err := runQuarterListCapture(t, "--profile", "a"); err != nil || fakeA.calls != 1 {
		t.Fatalf("second profile a run: err %v, search calls %d, want warm cache (1)", err, fakeA.calls)
	}

	other := []quarter.QuarterIssue{{Key: "B-1", Summary: "from b"}}
	fakeB := &fakeQuarterSearcher{issues: other}
	stubQuarterSearcher(t, fakeB)
	outB, _, err := runQuarterListCapture(t, "--profile", "b")
	if err != nil {
		t.Fatal(err)
	}
	if fakeB.calls != 1 || outB == outA || !strings.Contains(outB, "B-1") || strings.Contains(outB, "PROJ-1") {
		t.Errorf("profile b: calls %d, stdout %q; want its own cold search, not a's cache (a was %q)", fakeB.calls, outB, outA)
	}
	fakeD := &fakeQuarterSearcher{issues: []quarter.QuarterIssue{{Key: "D-1", Summary: "from default"}}}
	stubQuarterSearcher(t, fakeD)
	outD, _, err := runQuarterListCapture(t)
	if err != nil {
		t.Fatal(err)
	}
	if fakeD.calls != 1 || !strings.Contains(outD, "D-1") {
		t.Errorf("default: calls %d, stdout %q; want its own cold search", fakeD.calls, outD)
	}

	stubQuarterSearcher(t, &fakeQuarterSearcher{err: errors.New("must not search")})
	if out, _, err := runQuarterListCapture(t, "--profile", "a"); err != nil || out != outA {
		t.Errorf("profile a after others: err %v, stdout %q, want unchanged %q", err, out, outA)
	}
}

func TestRunQuarterList_Profile_PreviousNextLabelResolveAgainstProfile(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ","previous_quarter":"DEF-PREV","next_quarter":"DEF-NEXT"}}`)
	saveQuarterProfileLogin(t, "work", "w@example.com", "w-token")
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ","previous_quarter":"WORK-PREV","next_quarter":"WORK-NEXT"}}`)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"previous", []string{"--previous"}, "WORK-PREV"},
		{"next", []string{"--next"}, "WORK-NEXT"},
		{"label", []string{"--label", "ANY-Q"}, "ANY-Q"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeQuarterSearcher{issues: quarterListSample()}
			stubQuarterSearcher(t, fake)
			if _, stderr, err := runQuarterListCapture(t, append([]string{"--profile", "work"}, tc.args...)...); err != nil {
				t.Fatalf("err %v (stderr %q)", err, stderr)
			}
			if fake.lastProject != "WORKPROJ" || fake.lastQuarter != tc.want {
				t.Errorf("searched (%q, %q), want (WORKPROJ, %q)", fake.lastProject, fake.lastQuarter, tc.want)
			}
		})
	}
}

func TestRunQuarterList_Profile_UnsetQuarterRemediation(t *testing.T) {
	quarterListLogin(t)
	saveQuarterProfileLogin(t, "work", "w@example.com", "w-token")

	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ","current_quarter":"C","previous_quarter":"P","next_quarter":"N"}}`)
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ"}}`)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"current", nil, `jirahere: defaults.current_quarter is not set; add it to settings.json (run "jirahere configure set --profile work --current-quarter <label>").` + "\n"},
		{"previous", []string{"--previous"}, `jirahere: defaults.previous_quarter is not set; add it to settings.json (run "jirahere configure set --profile work --previous-quarter <label>").` + "\n"},
		{"next", []string{"--next"}, `jirahere: defaults.next_quarter is not set; add it to settings.json (run "jirahere configure set --profile work --next-quarter <label>").` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeQuarterSearcher{issues: quarterListSample()}
			stubQuarterSearcher(t, fake)
			stdout, stderr, err := runQuarterListCapture(t, append([]string{"--profile", "work"}, tc.args...)...)
			if err == nil || stdout != "" || stderr != tc.want {
				t.Errorf("err %v stdout %q stderr %q, want failure with stderr %q", err, stdout, stderr, tc.want)
			}
			if fake.calls != 0 {
				t.Errorf("search calls = %d, want 0", fake.calls)
			}
		})
	}

	writeProfileSettings(t, "work", `{"defaults":{"current_quarter":"WQ"}}`)
	_, stderr, err := runQuarterListCapture(t, "--profile", "work")
	want := `jirahere: defaults.project is not set; add it to settings.json (run "jirahere configure set --profile work --project <key>").` + "\n"
	if err == nil || stderr != want {
		t.Errorf("project unset: err %v stderr %q, want %q", err, stderr, want)
	}
}

func TestRunQuarterList_Profile_NotLoggedIn(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, `{"defaults":{"project":"DEFPROJ","current_quarter":"C"}}`)
	writeProfileSettings(t, "empty", `{"defaults":{"project":"P","current_quarter":"Q"}}`)
	fake := &fakeQuarterSearcher{issues: quarterListSample()}
	stubQuarterSearcher(t, fake)

	stdout, stderr, err := runQuarterListCapture(t, "--profile", "empty")
	want := "jirahere: not logged in (profile \"empty\"). Run \"jirahere login --profile empty\" first.\n"
	if err == nil || stdout != "" || stderr != want {
		t.Errorf("err %v stdout %q stderr %q, want failure with stderr %q", err, stdout, stderr, want)
	}
	if fake.calls != 0 {
		t.Errorf("search calls = %d, want 0", fake.calls)
	}
}

func TestRunQuarterList_NoProfile_MessagesUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"Q"}}`)
	stubQuarterSearcher(t, &fakeQuarterSearcher{})
	_, stderr, err := runQuarterListCapture(t)
	if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; err == nil || stderr != want {
		t.Errorf("not logged in: err %v stderr %q, want %q", err, stderr, want)
	}

	saveQuarterProfileLogin(t, "", "d@example.com", "d-token")
	writeSettingsFile(t, `{"defaults":{"project":"PROJ"}}`)
	_, stderr, err = runQuarterListCapture(t)
	if want := "jirahere: defaults.current_quarter is not set; add it to settings.json.\n"; err == nil || stderr != want {
		t.Errorf("unset quarter: err %v stderr %q, want %q", err, stderr, want)
	}
	_, stderr, err = runQuarterListCapture(t, "--previous")
	if want := "jirahere: defaults.previous_quarter is not set; add it to settings.json.\n"; err == nil || stderr != want {
		t.Errorf("unset previous: err %v stderr %q, want %q", err, stderr, want)
	}
}

func TestRunQuarterList_Profile_InvalidName_UsageErrorBeforeIO(t *testing.T) {
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
		{"with label", []string{"--label", "Q", "--profile", "../evil"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cacheDir := quarterListLogin(t)
			writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"Q"}}`)
			fake := &fakeQuarterSearcher{issues: quarterListSample()}
			stubQuarterSearcher(t, fake)
			xdg := os.Getenv("XDG_CONFIG_HOME")
			before, _ := os.ReadDir(xdg)

			stdout, stderr, err := runQuarterListCapture(t, tc.args...)

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
			if fake.calls != 0 {
				t.Errorf("search calls = %d, want 0", fake.calls)
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

func TestRunQuarterList_Profile_JSONUnchangedShape(t *testing.T) {
	quarterListLogin(t)
	saveQuarterProfileLogin(t, "work", "w@example.com", "w-token")
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORKPROJ","current_quarter":"WQ"}}`)
	writeSettingsFile(t, `{"defaults":{"project":"PROJ","current_quarter":"Q"}}`)
	stubQuarterSearcher(t, &fakeQuarterSearcher{issues: quarterListSample()})

	viaProfile, _, err := runQuarterListCapture(t, "--json", "--profile", "work")
	if err != nil {
		t.Fatal(err)
	}
	viaDefault, _, err := runQuarterListCapture(t, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if viaProfile != viaDefault {
		t.Errorf("--json under profile differs from default for the same inventory:\n%q\n%q", viaProfile, viaDefault)
	}
}

func TestQuarterList_Profile_ProcessExitCodeAndHelp(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "quarter", "list", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "quarter", "list", "--help")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("--help: stderr=%q exit=%d, want exit 0 and empty stderr", stderr, exitCode)
	}
	if !strings.Contains(stdout, "--profile profile") || !strings.Contains(stdout, profileFlagUsage[:20]) {
		t.Errorf("--help stdout = %q, want the canonical --profile listing", stdout)
	}
}
