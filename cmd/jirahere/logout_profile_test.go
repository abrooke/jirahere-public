package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

func setupLogoutProfiles(t *testing.T, profs ...string) {
	t.Helper()
	withLogoutDirs(t)
	for _, prof := range profs {
		saveTestConfig(t, prof, "example.atlassian.net")
		dir, err := quarter.CacheDir(prof)
		if err != nil {
			t.Fatalf("quarter.CacheDir(%q): %v", prof, err)
		}
		populateCache(t, dir)
	}
}

func assertProfileState(t *testing.T, prof string, wantPresent bool) {
	t.Helper()
	cfgPath, err := auth.ConfigPath(prof)
	if err != nil {
		t.Fatalf("auth.ConfigPath(%q): %v", prof, err)
	}
	cacheDir, err := quarter.CacheDir(prof)
	if err != nil {
		t.Fatalf("quarter.CacheDir(%q): %v", prof, err)
	}
	for _, path := range []string{cfgPath, cacheDir} {
		_, statErr := os.Lstat(path)
		switch {
		case wantPresent && statErr != nil:
			t.Errorf("profile %q: %s should be intact, Lstat err = %v", prof, path, statErr)
		case !wantPresent && !os.IsNotExist(statErr):
			t.Errorf("profile %q: %s should be removed, Lstat err = %v", prof, path, statErr)
		}
	}
}

func runLogoutCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runLogout(args) })
	})
	return stdout, stderr, err
}

func TestRunLogout_Profile_RemovesOnlyThatProfile(t *testing.T) {
	setupLogoutProfiles(t, "", "a", "b")

	stdout, stderr, err := runLogoutCapture(t, "--profile", "a")
	if err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if want := "Logged out (profile \"a\").\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	assertProfileState(t, "a", false)
	assertProfileState(t, "", true)
	assertProfileState(t, "b", true)
}

func TestRunLogout_Default_LeavesNamedProfiles(t *testing.T) {
	setupLogoutProfiles(t, "", "a", "b")

	stdout, stderr, err := runLogoutCapture(t)
	if err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if stdout != "Logged out.\n" {
		t.Errorf("stdout = %q, want %q", stdout, "Logged out.\n")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	assertProfileState(t, "", false)
	assertProfileState(t, "a", true)
	assertProfileState(t, "b", true)
}

func TestRunLogout_Profile_NotLoggedIn_StillClearsThatCache(t *testing.T) {
	setupLogoutProfiles(t, "", "b")
	orphan, err := quarter.CacheDir("a")
	if err != nil {
		t.Fatalf("quarter.CacheDir: %v", err)
	}
	populateCache(t, orphan)

	stdout, stderr, err := runLogoutCapture(t, "--profile", "a")
	if err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if want := "Not logged in (profile \"a\").\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	assertProfileState(t, "a", false)
	assertProfileState(t, "", true)
	assertProfileState(t, "b", true)
}

func TestRunLogout_Profile_ClearCacheError_NamesProfileCachePath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: directory permission bits do not block removal")
	}
	setupLogoutProfiles(t, "", "a")
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

	stdout, stderr, err := runLogoutCapture(t, "--profile", "a")

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v (%T), want *errSilent", err, err)
	}
	if want := "Logged out (profile \"a\").\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if n := strings.Count(stderr, "\n"); n != 1 {
		t.Errorf("stderr has %d lines, want 1: %q", n, stderr)
	}
	if !strings.Contains(stderr, "at ~/.cache/jirahere-profiles/a ") {
		t.Errorf("stderr = %q, want it to name the profile's cache path ~/.cache/jirahere-profiles/a", stderr)
	}
	if strings.Contains(stderr, "~/.cache/jirahere ") {
		t.Errorf("stderr = %q, names the default cache path", stderr)
	}
	if strings.Contains(stderr, cacheDir) {
		t.Errorf("stderr = %q, leaks the resolved absolute cache path", stderr)
	}
	assertProfileState(t, "", true)
	cfgPath, _ := auth.ConfigPath("a")
	if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
		t.Errorf("profile config still present, Stat err = %v", statErr)
	}
}

func TestProfileCachePathDisplay(t *testing.T) {
	if got := profileCachePathDisplay(""); got != cachePathDisplay {
		t.Errorf("profileCachePathDisplay(\"\") = %q, want %q", got, cachePathDisplay)
	}
	if got, want := profileCachePathDisplay("a"), "~/.cache/jirahere-profiles/a"; got != want {
		t.Errorf("profileCachePathDisplay(\"a\") = %q, want %q", got, want)
	}
}

func TestRunLogout_Profile_InvalidName_UsageErrorBeforeDeletion(t *testing.T) {
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
			setupLogoutProfiles(t, "", "a")

			stdout, stderr, err := runLogoutCapture(t, tc.args...)

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
			if tc.name == "missing value" && !strings.Contains(stderr, "flag needs an argument: --profile") {
				t.Errorf("stderr = %q, want the flag-parse message naming --profile", stderr)
			}
			if strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "\x1b") {
				t.Errorf("stderr = %q, want exactly one sanitized line", stderr)
			}
			assertProfileState(t, "", true)
			assertProfileState(t, "a", true)
		})
	}
}
