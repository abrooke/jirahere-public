package main

import (
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

func withEmptyStdin(t *testing.T) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() error = %v", err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
}

func withAcliAvailable(t *testing.T, available bool) {
	t.Helper()
	orig := acliAvailable
	t.Cleanup(func() { acliAvailable = orig })
	acliAvailable = func() bool { return available }
}

func TestRunLogin_AcliPresent_NoApiToken(t *testing.T) {
	withAcliAvailable(t, true)
	withEmptyStdin(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var err error
	var stderrOut string
	stdoutOut := captureStdout(t, func() {
		stderrOut = captureStderr(t, func() {
			err = runLogin(nil)
		})
	})

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("runLogin() error = %v, want an errSilent (non-zero exit, message already printed)", err)
	}
	if stdoutOut != "" {
		t.Errorf("stdout = %q, want empty", stdoutOut)
	}

	wantStderr := "jirahere: acli is installed but is not supported as a Jira provider: acli cannot set a parent on an existing work item. jirahere uses its own credentials.\n" +
		"Run \"jirahere login --api-token\" to log in with jirahere's own credentials.\n"
	if stderrOut != wantStderr {
		t.Errorf("stderr = %q, want %q", stderrOut, wantStderr)
	}
	if strings.Contains(stderrOut, "acli jira auth login") {
		t.Errorf("stderr = %q, want no acli login guidance", stderrOut)
	}
	if got := strings.Count(stderrOut, "\n"); got != 2 {
		t.Errorf("stderr has %d lines, want 2 (notice + pointer): %q", got, stderrOut)
	}
	if entries, rerr := os.ReadDir(home); rerr != nil || len(entries) != 0 {
		t.Errorf("home dir entries = %v (err %v), want none: login must not read or write config", entries, rerr)
	}
}

func TestRunLogin_AcliPresent_ApiToken(t *testing.T) {
	withAcliAvailable(t, true)
	withEmptyStdin(t)

	var err error
	var stdoutOut string
	stderrOut := captureStderr(t, func() {
		stdoutOut = captureStdout(t, func() {
			err = runLogin([]string{"--api-token", "--site", "team.atlassian.net", "--email", "user@example.com"})
		})
	})

	if err == nil || !strings.Contains(err.Error(), "no API token supplied") {
		t.Fatalf("runLogin() error = %v, want the API-token flow's own failure (proof the flow ran)", err)
	}
	wantNotice := "jirahere: acli is installed but is not supported as a Jira provider: acli cannot set a parent on an existing work item. jirahere uses its own credentials.\n"
	if !strings.HasPrefix(stderrOut, wantNotice) {
		t.Errorf("stderr = %q, want it to start with %q", stderrOut, wantNotice)
	}
	if strings.Contains(stderrOut, "login --api-token") {
		t.Errorf("stderr = %q, want no pointer line (stop case only)", stderrOut)
	}
	if strings.Contains(stdoutOut, "acli") {
		t.Errorf("stdout = %q, want no acli text on stdout", stdoutOut)
	}
}

func TestRunLogin_AcliAbsent_NoApiToken(t *testing.T) {
	withAcliAvailable(t, false)
	origClientID := auth.OAuthClientID
	auth.OAuthClientID = ""
	t.Cleanup(func() { auth.OAuthClientID = origClientID })

	var err error
	var stdoutOut string
	stderrOut := captureStderr(t, func() {
		stdoutOut = captureStdout(t, func() {
			err = runLogin(nil)
		})
	})

	if err == nil || !strings.Contains(err.Error(), "OAuth is not configured") {
		t.Fatalf("runLogin() error = %v, want the OAuth-not-configured failure (proof the OAuth path ran)", err)
	}
	if !strings.Contains(stderrOut, "OAuth is not configured") {
		t.Errorf("stderr = %q, want the OAuth-not-configured failure message", stderrOut)
	}
	if strings.Contains(stderrOut, "not supported") || strings.Contains(stdoutOut, "acli") {
		t.Errorf("stdout = %q, stderr = %q, want no acli notice when acli is absent", stdoutOut, stderrOut)
	}
}

func TestRunLogin_UnknownFlag_SanitizesStderr(t *testing.T) {
	const poison = "--\x1b[31mBOGUS\x1b[0m\r\nInjected forged line"

	out := captureStderr(t, func() { _ = runLogin([]string{poison}) })

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

func TestRunLogin_Help(t *testing.T) {
	for _, flagForm := range []string{"-h", "--help"} {
		t.Run(flagForm, func(t *testing.T) {
			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() {
					err = runLogin([]string{flagForm})
				})
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}

			if !strings.Contains(stdoutOut, "also forces this path even when acli is present") {
				t.Errorf("stdout = %q, want --api-token's usage text to explain it also forces the direct path when acli is present", stdoutOut)
			}
			for _, want := range []string{"--api-token", "--site string", "--email string", "--debug", "--profile profile"} {
				if !strings.Contains(stdoutOut, want) {
					t.Errorf("stdout = %q, want it to contain %q (login's option listing)", stdoutOut, want)
				}
			}
			if stderrOut != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderrOut)
			}
		})
	}
}
