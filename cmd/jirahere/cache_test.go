package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubClearQuarterCache(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := clearQuarterCache
	clearQuarterCache = fn
	t.Cleanup(func() { clearQuarterCache = orig })
}

func cacheClearHome(t *testing.T) (cacheDir string) {
	t.Helper()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	return filepath.Join(cacheHome, "jirahere")
}

func TestRunCache_BadOrAbsentSubcommand(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"absent", nil},
		{"bad", []string{"frobnicate"}},
		{"clear plus surplus positional", []string{"clear", "extra"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stdout := captureStdout(t, func() {
				stderr := captureStderr(t, func() { err = runCache(tt.args) })
				if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, cacheClearUsage) {
					t.Errorf("stderr = %q, want one body-free line containing %q", stderr, cacheClearUsage)
				}
			})
			if err == nil || !strings.Contains(err.Error(), cacheClearUsage) {
				t.Fatalf("err = %v, want %q", err, cacheClearUsage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

const cacheClearHelp = "usage: jirahere cache clear [options]\n\noptions:\n  --profile profile\n        run against the named profile (its own login, settings, and cache); omit for the default profile\n"

func TestRunCache_HelpToken(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"clear", "--help"}} {
		var err error
		var stderr string
		out := captureStdout(t, func() {
			stderr = captureStderr(t, func() { err = runCache(args) })
		})
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("runCache(%v) err = %v, want flag.ErrHelp", args, err)
		}
		if stderr != "" {
			t.Errorf("runCache(%v) stderr = %q, want empty (help is not a failure)", args, stderr)
		}
		if out != cacheClearHelp {
			t.Errorf("runCache(%v) help stdout = %q, want exactly %q", args, out, cacheClearHelp)
		}
	}
}

func TestRunCacheClear_PopulatedCacheRemovedAndConfirmed(t *testing.T) {
	cacheDir := cacheClearHome(t)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "quarter-inventory-PROJ-FY26-Q1-deadbeef.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var err error
	out := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runCacheClear(nil) })
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
	if err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}
	if out != "Cache cleared.\n" {
		t.Errorf("stdout = %q, want %q", out, "Cache cleared.\n")
	}
	if _, statErr := os.Stat(cacheDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("cache dir %s still present after clear (stat err = %v)", cacheDir, statErr)
	}
}

func TestRunCacheClear_AbsentCacheStillConfirmed(t *testing.T) {
	cacheDir := cacheClearHome(t)
	if _, statErr := os.Stat(cacheDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("test setup: cache dir %s unexpectedly exists", cacheDir)
	}

	var err error
	out := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runCacheClear(nil) })
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
	if err != nil {
		t.Fatalf("runCacheClear: %v", err)
	}
	if out != "Cache cleared.\n" {
		t.Errorf("stdout = %q, want the same confirmation as the populated case", out)
	}
}

func TestRunCacheClear_ClearErrorIsBodyFreeNonZero(t *testing.T) {
	stubClearQuarterCache(t, func(string) error {
		return errors.New("could not clear quarter inventory cache /home/someone/.cache/jirahere: permission denied")
	})

	var err error
	out := captureStdout(t, func() {
		stderr := captureStderr(t, func() { err = runCacheClear(nil) })
		if strings.Count(stderr, "\n") != 1 {
			t.Errorf("stderr = %q, want exactly one line", stderr)
		}
		if !strings.Contains(stderr, "could not clear the quarter inventory cache") {
			t.Errorf("stderr = %q, want the cache-clear failure line", stderr)
		}
		if strings.Contains(stderr, "permission denied") || strings.Contains(stderr, "/home/someone") {
			t.Errorf("stderr = %q leaks the raw removal error / resolved path", stderr)
		}
	})
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v (%T), want an errSilent so main exits non-zero without re-prefixing", err, err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty on the failure path", out)
	}
}

func TestMainDispatch_CacheClearAndRootUsage(t *testing.T) {

	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	cacheDir := filepath.Join(cacheHome, "jirahere")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stdout, stderr, exitCode := runJirahereProcess(t, "cache", "clear")
	if exitCode != 0 || stderr != "" || stdout != "Cache cleared.\n" {
		t.Errorf("cache clear: stdout=%q stderr=%q exit=%d, want confirmation and exit 0", stdout, stderr, exitCode)
	}
	if _, statErr := os.Stat(cacheDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("cache dir %s still present after `cache clear` (stat err = %v)", cacheDir, statErr)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "--help")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "cache clear") {
		t.Errorf("root help: stdout=%q stderr=%q exit=%d, want `cache clear` listed on stdout", stdout, stderr, exitCode)
	}
}
