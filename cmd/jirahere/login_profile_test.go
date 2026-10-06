package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

func withStdinContent(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatalf("write pipe: %v", err)
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

func withValidateAPIToken(t *testing.T, fn func(site, email, token string) (*jira.Myself, error)) {
	t.Helper()
	orig := validateAPIToken
	t.Cleanup(func() { validateAPIToken = orig })
	validateAPIToken = func(_ context.Context, site, email, token string, _ io.Writer) (*jira.Myself, error) {
		return fn(site, email, token)
	}
}

func saveTestConfig(t *testing.T, prof, site string) {
	t.Helper()
	cfg := &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     site,
		APIToken: &auth.APITokenConfig{Email: "old@example.com", Token: "old-token"},
	}
	if err := auth.Save(prof, cfg); err != nil {
		t.Fatalf("auth.Save(%q): %v", prof, err)
	}
}

func readTestConfig(t *testing.T, prof string) []byte {
	t.Helper()
	path, err := auth.ConfigPath(prof)
	if err != nil {
		t.Fatalf("auth.ConfigPath(%q): %v", prof, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func fakeMyself(_, _, _ string) (*jira.Myself, error) {
	return &jira.Myself{DisplayName: "Ada Lovelace", EmailAddress: "ada@example.com"}, nil
}

func TestRunLogin_Profile_InvalidName_UsageErrorBeforeAnything(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", []string{"--profile", ""}},
		{"empty with api-token", []string{"--api-token", "--profile", ""}},
		{"path traversal", []string{"--api-token", "--profile", "../evil"}},
		{"separator", []string{"--api-token", "--profile", "a/b"}},
		{"leading dot", []string{"--api-token", "--profile", ".hidden"}},
		{"whitespace", []string{"--api-token", "--profile", "a b"}},
		{"too long", []string{"--api-token", "--profile", strings.Repeat("a", 65)}},
		{"control chars", []string{"--api-token", "--profile", "a\x1b[31mb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withAcliAvailable(t, true)
			withEmptyStdin(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			withValidateAPIToken(t, func(_, _, _ string) (*jira.Myself, error) {
				t.Error("validateAPIToken called: network must not be reached")
				return nil, errors.New("unreachable")
			})

			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() {
					err = runLogin(tc.args)
				})
			})

			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("runLogin() error = %v, want an errSilent usage error", err)
			}
			if stdoutOut != "" {
				t.Errorf("stdout = %q, want empty (no notice, no prompt)", stdoutOut)
			}
			if strings.Contains(stderrOut, "acli") {
				t.Errorf("stderr = %q, want no acli notice before profile validation", stderrOut)
			}
			if !strings.Contains(stderrOut, "--profile") {
				t.Errorf("stderr = %q, want it to name --profile", stderrOut)
			}
			if strings.Count(stderrOut, "\n") != 1 || strings.ContainsAny(stderrOut, "\x1b\r") {
				t.Errorf("stderr = %q, want exactly one sanitized line", stderrOut)
			}
			if entries, rerr := os.ReadDir(home); rerr != nil || len(entries) != 0 {
				t.Errorf("home dir entries = %v (err %v), want none", entries, rerr)
			}
		})
	}
}

func TestRunLogin_Profile_InvalidName_BeatsAcliStop(t *testing.T) {
	withAcliAvailable(t, true)
	withEmptyStdin(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var err error
	stderrOut := captureStderr(t, func() { err = runLogin([]string{"--profile", "a/b"}) })

	if err == nil {
		t.Fatal("runLogin() error = nil, want usage error")
	}
	if strings.Contains(stderrOut, "acli") || strings.Contains(stderrOut, "login --api-token") {
		t.Errorf("stderr = %q, want the usage error only (no acli notice/pointer)", stderrOut)
	}
}

func TestRunLogin_Profile_AcliGateUnchanged(t *testing.T) {
	withAcliAvailable(t, true)
	withEmptyStdin(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	var err error
	stderrOut := captureStderr(t, func() { err = runLogin([]string{"--profile", "work"}) })

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("runLogin() error = %v, want errSilent", err)
	}
	want := acliUnsupportedNotice + "\n" + acliLoginPointer + "\n"
	if stderrOut != want {
		t.Errorf("stderr = %q, want %q", stderrOut, want)
	}
	if entries, rerr := os.ReadDir(xdg); rerr != nil || len(entries) != 0 {
		t.Errorf("config dir entries = %v (err %v), want none", entries, rerr)
	}
}

func TestRunLogin_Profile_WritesOnlyThatProfile(t *testing.T) {
	withAcliAvailable(t, false)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	saveTestConfig(t, "", "default.atlassian.net")
	saveTestConfig(t, "other", "other.atlassian.net")
	defaultBefore := readTestConfig(t, "")
	otherBefore := readTestConfig(t, "other")

	withStdinContent(t, "s3cret-token\n")
	var gotSite, gotEmail, gotToken string
	withValidateAPIToken(t, func(site, email, token string) (*jira.Myself, error) {
		gotSite, gotEmail, gotToken = site, email, token
		return fakeMyself(site, email, token)
	})

	var err error
	stdoutOut := captureStdout(t, func() {
		err = runLogin([]string{"--api-token", "--profile", "work", "--site", "work.atlassian.net", "--email", "ada@example.com"})
	})
	if err != nil {
		t.Fatalf("runLogin() error = %v", err)
	}
	if gotSite != "work.atlassian.net" || gotEmail != "ada@example.com" || gotToken != "s3cret-token" {
		t.Errorf("validated (%q, %q, %q), want the flag/stdin values", gotSite, gotEmail, gotToken)
	}

	want := "Checking credentials...\n" +
		"Logged in to work.atlassian.net as Ada Lovelace <ada@example.com> using API token (profile \"work\").\n" +
		"Credentials stored in ~/.config/jirahere-profiles/work/config.json.\n"
	if stdoutOut != want {
		t.Errorf("stdout = %q, want %q", stdoutOut, want)
	}

	cfg, err := auth.Load("work")
	if err != nil {
		t.Fatalf("auth.Load(work): %v", err)
	}
	if cfg.Site != "work.atlassian.net" || cfg.APIToken == nil || cfg.APIToken.Token != "s3cret-token" {
		t.Errorf("work config = %+v, want the new login", cfg)
	}
	workPath, _ := auth.ConfigPath("work")
	if fi, err := os.Stat(workPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("work config.json stat = %v, %v, want mode 0600", fi, err)
	}
	for _, d := range []string{filepath.Dir(workPath), filepath.Dir(filepath.Dir(workPath))} {
		if fi, err := os.Stat(d); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("dir %s stat = %v, %v, want mode 0700", d, fi, err)
		}
	}
	if got := readTestConfig(t, ""); string(got) != string(defaultBefore) {
		t.Errorf("default config.json changed: %q", got)
	}
	if got := readTestConfig(t, "other"); string(got) != string(otherBefore) {
		t.Errorf("other profile config.json changed: %q", got)
	}
}

func TestRunLogin_Profile_NoticeReadsProfileFile(t *testing.T) {
	withAcliAvailable(t, false)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, "", "default.atlassian.net")
	saveTestConfig(t, "work", "old-work.atlassian.net")

	withStdinContent(t, "tok\n")
	withValidateAPIToken(t, fakeMyself)

	var err error
	stdoutOut := captureStdout(t, func() {
		err = runLogin([]string{"--api-token", "--profile", "work", "--site", "new-work.atlassian.net", "--email", "ada@example.com"})
	})
	if err != nil {
		t.Fatalf("runLogin() error = %v", err)
	}
	wantNotice := "Currently logged in to old-work.atlassian.net via API token (profile \"work\"). Continuing will replace this.\n"
	if !strings.HasPrefix(stdoutOut, wantNotice) {
		t.Errorf("stdout = %q, want it to start with %q", stdoutOut, wantNotice)
	}
	if strings.Contains(stdoutOut, "default.atlassian.net") {
		t.Errorf("stdout = %q, must not mention the default profile's site", stdoutOut)
	}
}

func TestRunLogin_Profile_NoNoticeWhenProfileHasNoConfig(t *testing.T) {
	withAcliAvailable(t, false)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, "", "default.atlassian.net")

	withStdinContent(t, "tok\n")
	withValidateAPIToken(t, fakeMyself)

	var err error
	stdoutOut := captureStdout(t, func() {
		err = runLogin([]string{"--api-token", "--profile", "fresh", "--site", "fresh.atlassian.net", "--email", "ada@example.com"})
	})
	if err != nil {
		t.Fatalf("runLogin() error = %v", err)
	}
	if strings.Contains(stdoutOut, "Currently logged in") || strings.Contains(stdoutOut, "default.atlassian.net") {
		t.Errorf("stdout = %q, want no existing-config notice", stdoutOut)
	}
}

func TestRunLogin_NoProfile_OutputByteIdentical(t *testing.T) {
	withAcliAvailable(t, false)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestConfig(t, "", "old.atlassian.net")

	withStdinContent(t, "tok\n")
	withValidateAPIToken(t, fakeMyself)

	var err error
	stdoutOut := captureStdout(t, func() {
		err = runLogin([]string{"--api-token", "--site", "new.atlassian.net", "--email", "ada@example.com"})
	})
	if err != nil {
		t.Fatalf("runLogin() error = %v", err)
	}
	want := "Currently logged in to old.atlassian.net via API token. Continuing will replace this.\n" +
		"Checking credentials...\n" +
		"Logged in to new.atlassian.net as Ada Lovelace <ada@example.com> using API token.\n" +
		"Credentials stored in ~/.config/jirahere/config.json.\n"
	if stdoutOut != want {
		t.Errorf("stdout = %q, want %q", stdoutOut, want)
	}
	cfg, err := auth.Load("")
	if err != nil || cfg.Site != "new.atlassian.net" {
		t.Errorf("default config = %+v, %v, want the new login", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(mustConfigDir(t, "")), "jirahere-profiles")); !os.IsNotExist(err) {
		t.Errorf("profiles dir exists after a default login (stat err = %v)", err)
	}
}

func mustConfigDir(t *testing.T, prof string) string {
	t.Helper()
	d, err := auth.ConfigDir(prof)
	if err != nil {
		t.Fatalf("auth.ConfigDir(%q): %v", prof, err)
	}
	return d
}

func TestRunLogin_Help_ListsProfile(t *testing.T) {
	var err error
	stdoutOut := captureStdout(t, func() { err = runLogin([]string{"--help"}) })
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	want := "  --profile profile\n        run against the named profile (its own login, settings, and cache); omit for the default profile\n"
	if !strings.Contains(stdoutOut, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdoutOut, want)
	}
}
