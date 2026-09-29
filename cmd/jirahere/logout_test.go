package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withLogoutDirs(t *testing.T) (configDir, cacheDir string) {
	t.Helper()
	cfgHome := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	return filepath.Join(cfgHome, "jirahere"), filepath.Join(cacheHome, "jirahere")
}

func writeStubConfig(t *testing.T, configDir string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", configDir, err)
	}
	body := []byte(`{"provider":"api-token","site":"example.atlassian.net"}`)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), body, 0o600); err != nil {
		t.Fatalf("WriteFile config.json: %v", err)
	}
}

func populateCache(t *testing.T, cacheDir string) {
	t.Helper()
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", cacheDir, err)
	}
	for _, name := range []string{
		"quarter-inventory-PROJ-FY26-Q1-0011223344556677.json",
		"quarter-inventory-PROJ-FY26-Q2-8899aabbccddeeff.json",
	} {
		if err := os.WriteFile(filepath.Join(cacheDir, name), []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
}

func TestRunLogout_LoggedIn_ClearsCache(t *testing.T) {
	configDir, cacheDir := withLogoutDirs(t)
	writeStubConfig(t, configDir)
	populateCache(t, cacheDir)

	var err error
	var stderrOut string
	stdoutOut := captureStdout(t, func() {
		stderrOut = captureStderr(t, func() { err = runLogout(nil) })
	})

	if err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if stdoutOut != "Logged out.\n" {
		t.Errorf("stdout = %q, want %q", stdoutOut, "Logged out.\n")
	}
	if stderrOut != "" {
		t.Errorf("stderr = %q, want empty", stderrOut)
	}
	if _, statErr := os.Lstat(cacheDir); !os.IsNotExist(statErr) {
		t.Errorf("cache dir still present after logout, Lstat err = %v", statErr)
	}
}

func TestRunLogout_NotLoggedIn_StillClearsCache(t *testing.T) {
	_, cacheDir := withLogoutDirs(t)
	populateCache(t, cacheDir)

	var err error
	var stderrOut string
	stdoutOut := captureStdout(t, func() {
		stderrOut = captureStderr(t, func() { err = runLogout(nil) })
	})

	if err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if stdoutOut != "Not logged in.\n" {
		t.Errorf("stdout = %q, want %q", stdoutOut, "Not logged in.\n")
	}
	if stderrOut != "" {
		t.Errorf("stderr = %q, want empty", stderrOut)
	}
	if _, statErr := os.Lstat(cacheDir); !os.IsNotExist(statErr) {
		t.Errorf("orphaned cache dir not cleared, Lstat err = %v", statErr)
	}
}

func TestRunLogout_ClearCacheError_KeepsStdoutLineAndFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: directory permission bits do not block removal")
	}
	configDir, cacheDir := withLogoutDirs(t)
	writeStubConfig(t, configDir)

	sub := filepath.Join(cacheDir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "quarter-inventory-x.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })

	var err error
	var stderrOut string
	stdoutOut := captureStdout(t, func() {
		stderrOut = captureStderr(t, func() { err = runLogout(nil) })
	})

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %v (%T), want *errSilent (non-zero exit)", err, err)
	}
	if stdoutOut != "Logged out.\n" {
		t.Errorf("stdout = %q, want the credential line %q still present", stdoutOut, "Logged out.\n")
	}
	if n := strings.Count(stderrOut, "\n"); n != 1 {
		t.Errorf("stderr has %d lines, want exactly 1: %q", n, stderrOut)
	}
	if !strings.Contains(stderrOut, "credentials were removed") || !strings.Contains(stderrOut, "could not be cleared") {
		t.Errorf("stderr = %q, want it to state credentials were removed but the cache was not cleared", stderrOut)
	}
	if strings.Contains(stderrOut, cacheDir) {
		t.Errorf("stderr = %q, leaks the resolved absolute cache path %q", stderrOut, cacheDir)
	}
	if _, statErr := os.Stat(filepath.Join(configDir, "config.json")); !os.IsNotExist(statErr) {
		t.Errorf("config.json still present; credentials were not actually removed, Stat err = %v", statErr)
	}
}

func TestRunLogout_UnknownFlag_SanitizesStderr(t *testing.T) {
	const poison = "--\x1b[31mBOGUS\x1b[0m\r\nInjected forged line"

	out := captureStderr(t, func() { _ = runLogout([]string{poison}) })

	if strings.Contains(out, "\x1b") {
		t.Errorf("stderr contains an unsanitized escape sequence: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("stderr contains an unsanitized carriage return: %q", out)
	}
	if want := 1; strings.Count(out, "\n") != want {
		t.Errorf("stderr has %d lines, want %d (the flag package's own dump must be suppressed and the message must print exactly once): %q", strings.Count(out, "\n"), want, out)
	}
}

func TestRunLogout_Help(t *testing.T) {
	for _, flagForm := range []string{"-h", "--help"} {
		t.Run(flagForm, func(t *testing.T) {
			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() {
					err = runLogout([]string{flagForm})
				})
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}
			if !strings.HasPrefix(stdoutOut, "usage: jirahere logout [options]\n") {
				t.Errorf("stdout = %q, want it to start with logout's usage header", stdoutOut)
			}
			if !strings.Contains(stdoutOut, "--profile profile") {
				t.Errorf("stdout = %q, want it to list --profile", stdoutOut)
			}
			if stderrOut != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderrOut)
			}
		})
	}
}
