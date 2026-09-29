package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/quarter"
)

func setupCacheProfiles(t *testing.T, profs ...string) {
	t.Helper()
	withLogoutDirs(t)
	for _, prof := range profs {
		dir, err := quarter.CacheDir(prof)
		if err != nil {
			t.Fatalf("quarter.CacheDir(%q): %v", prof, err)
		}
		populateCache(t, dir)
	}
}

func assertCachePresent(t *testing.T, prof string, want bool) {
	t.Helper()
	dir, err := quarter.CacheDir(prof)
	if err != nil {
		t.Fatalf("quarter.CacheDir(%q): %v", prof, err)
	}
	_, statErr := os.Stat(dir)
	if want && statErr != nil {
		t.Errorf("profile %q cache %s should be intact, Stat err = %v", prof, dir, statErr)
	}
	if !want && !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("profile %q cache %s should be removed, Stat err = %v", prof, dir, statErr)
	}
}

func runCacheClearCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCacheClear(args) })
	})
	return stdout, stderr, err
}

func TestRunCacheClear_Profile_ClearsOnlyThatProfile(t *testing.T) {
	setupCacheProfiles(t, "", "a", "b")

	stdout, stderr, err := runCacheClearCapture(t, "--profile", "a")
	if err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}
	if want := "Cache cleared (profile \"a\").\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	assertCachePresent(t, "a", false)
	assertCachePresent(t, "", true)
	assertCachePresent(t, "b", true)
}

func TestRunCacheClear_Default_LeavesNamedProfiles(t *testing.T) {
	setupCacheProfiles(t, "", "a", "b")

	stdout, stderr, err := runCacheClearCapture(t)
	if err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}
	if stdout != "Cache cleared.\n" {
		t.Errorf("stdout = %q, want %q", stdout, "Cache cleared.\n")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	assertCachePresent(t, "", false)
	assertCachePresent(t, "a", true)
	assertCachePresent(t, "b", true)
}

func TestRunCacheClear_Profile_AbsentCacheStillConfirmed(t *testing.T) {
	setupCacheProfiles(t, "", "b")

	stdout, stderr, err := runCacheClearCapture(t, "--profile", "a")
	if err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}
	if want := "Cache cleared (profile \"a\").\n"; stdout != want || stderr != "" {
		t.Errorf("stdout = %q stderr = %q, want %q and empty stderr", stdout, stderr, want)
	}
	assertCachePresent(t, "", true)
	assertCachePresent(t, "b", true)
}

func TestRunCacheClear_Profile_PassesNameToClear(t *testing.T) {
	var got []string
	stubClearQuarterCache(t, func(prof string) error {
		got = append(got, prof)
		return nil
	})
	if _, _, err := runCacheClearCapture(t, "--profile", "work"); err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}
	if _, _, err := runCacheClearCapture(t); err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}
	if len(got) != 2 || got[0] != "work" || got[1] != "" {
		t.Errorf("clearQuarterCache calls = %q, want [\"work\" \"\"]", got)
	}
}

func TestRunCacheClear_Profile_ClearError_NamesProfileCachePath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: directory permission bits do not block removal")
	}
	setupCacheProfiles(t, "", "a")
	cacheDir, err := quarter.CacheDir("a")
	if err != nil {
		t.Fatalf("quarter.CacheDir: %v", err)
	}
	sub := filepath.Join(cacheDir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })

	stdout, stderr, err := runCacheClearCapture(t, "--profile", "a")

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v (%T), want *errSilent", err, err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on the failure path", stdout)
	}
	if n := strings.Count(stderr, "\n"); n != 1 {
		t.Errorf("stderr has %d lines, want 1: %q", n, stderr)
	}
	if !strings.Contains(stderr, "at ~/.cache/jirahere-profiles/a.") {
		t.Errorf("stderr = %q, want it to name the profile's cache path", stderr)
	}
	if strings.Contains(stderr, "~/.cache/jirahere.") {
		t.Errorf("stderr = %q, names the default cache path", stderr)
	}
	if strings.Contains(stderr, cacheDir) {
		t.Errorf("stderr = %q, leaks the resolved absolute cache path", stderr)
	}
	assertCachePresent(t, "", true)
}

func TestRunCacheClear_Profile_InvalidName_UsageErrorBeforeDeletion(t *testing.T) {
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupCacheProfiles(t, "", "a")
			stubClearQuarterCache(t, func(string) error {
				t.Error("clearQuarterCache called despite an invalid --profile")
				return nil
			})

			stdout, stderr, err := runCacheClearCapture(t, tc.args...)

			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %v (%T), want *errSilent usage error", err, err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "--profile") {
				t.Errorf("stderr = %q, want it to name --profile", stderr)
			}
			if strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "\x1b") {
				t.Errorf("stderr = %q, want exactly one sanitized line", stderr)
			}
			assertCachePresent(t, "", true)
			assertCachePresent(t, "a", true)
		})
	}
}

func TestMainDispatch_CacheClearProfile(t *testing.T) {
	setupCacheProfiles(t, "", "a", "b")

	stdout, stderr, exitCode := runJirahereProcess(t, "cache", "clear", "--profile", "a")
	if exitCode != 0 || stderr != "" || stdout != "Cache cleared (profile \"a\").\n" {
		t.Errorf("stdout=%q stderr=%q exit=%d, want confirmation and exit 0", stdout, stderr, exitCode)
	}
	assertCachePresent(t, "a", false)
	assertCachePresent(t, "", true)
	assertCachePresent(t, "b", true)

	stdout, stderr, exitCode = runJirahereProcess(t, "cache", "clear", "--profile", "../evil")
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "--profile") {
		t.Errorf("bad name: stdout=%q stderr=%q exit=%d, want body-free usage error and exit 2", stdout, stderr, exitCode)
	}
	assertCachePresent(t, "", true)
	assertCachePresent(t, "b", true)
}
