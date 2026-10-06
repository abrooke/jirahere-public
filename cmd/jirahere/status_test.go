package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

func withStatusConfigDir(t *testing.T) string {
	t.Helper()
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	return filepath.Join(cfgHome, "jirahere")
}

func saveStatusConfig(t *testing.T, provider string) {
	t.Helper()
	cfg := &auth.Config{Provider: provider, Site: "acme.atlassian.net"}
	switch provider {
	case auth.ProviderOAuth:
		cfg.CloudID = "11111111-2222-3333-4444-555555555555"
		cfg.OAuth = &auth.OAuthConfig{
			AccessToken:  "access",
			RefreshToken: "refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		}
	case auth.ProviderAPIToken:
		cfg.APIToken = &auth.APITokenConfig{Email: "user@example.com", Token: "tok"}
	}
	if err := auth.Save("", cfg); err != nil {
		t.Fatalf("auth.Save(%s): %v", provider, err)
	}
}

func writeRawStatusConfig(t *testing.T, configDir, body string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", configDir, err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile config.json: %v", err)
	}
}

func TestRunStatus_LoggedInText(t *testing.T) {
	for _, tt := range []struct {
		provider string
		want     string
	}{
		{auth.ProviderOAuth, "Logged in to acme.atlassian.net via OAuth.\n"},
		{auth.ProviderAPIToken, "Logged in to acme.atlassian.net via API token.\n"},
	} {
		t.Run(tt.provider, func(t *testing.T) {
			withStatusConfigDir(t)
			saveStatusConfig(t, tt.provider)

			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runStatus(nil) })
			})

			if err != nil {
				t.Fatalf("runStatus: %v", err)
			}
			if stdout != tt.want {
				t.Errorf("stdout = %q, want %q", stdout, tt.want)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestRunStatus_LoggedInJSON(t *testing.T) {
	for _, provider := range []string{auth.ProviderOAuth, auth.ProviderAPIToken} {
		t.Run(provider, func(t *testing.T) {
			withStatusConfigDir(t)
			saveStatusConfig(t, provider)

			var err error
			out := captureStdout(t, func() { err = runStatus([]string{"--json"}) })
			if err != nil {
				t.Fatalf("runStatus --json: %v", err)
			}

			var doc struct {
				LoggedIn bool    `json:"logged_in"`
				Site     *string `json:"site"`
				Provider *string `json:"provider"`
			}
			if uerr := json.Unmarshal([]byte(out), &doc); uerr != nil {
				t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, out)
			}
			if !doc.LoggedIn {
				t.Errorf("logged_in = false, want true")
			}
			if doc.Site == nil || *doc.Site != "acme.atlassian.net" {
				t.Errorf("site = %v, want %q", doc.Site, "acme.atlassian.net")
			}
			if doc.Provider == nil || *doc.Provider != provider {
				t.Errorf("provider = %v, want %q", doc.Provider, provider)
			}

			dec := json.NewDecoder(strings.NewReader(out))
			var discard any
			if derr := dec.Decode(&discard); derr != nil {
				t.Fatalf("first decode: %v", derr)
			}
			if dec.More() {
				t.Errorf("stdout carries more than one JSON value: %q", out)
			}
		})
	}
}

func TestRunStatus_NotLoggedIn(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		withStatusConfigDir(t)

		var err error
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() { err = runStatus(nil) })
		})

		if err != nil {
			t.Fatalf("runStatus: %v", err)
		}
		if want := "Not logged in. Run \"jirahere login\" first.\n"; stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})

	t.Run("json", func(t *testing.T) {
		withStatusConfigDir(t)

		var err error
		var stderr string
		out := captureStdout(t, func() {
			stderr = captureStderr(t, func() { err = runStatus([]string{"--json"}) })
		})

		if err != nil {
			t.Fatalf("runStatus --json: %v", err)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}

		var raw map[string]json.RawMessage
		if uerr := json.Unmarshal([]byte(out), &raw); uerr != nil {
			t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, out)
		}
		if len(raw) != 3 {
			t.Fatalf("top-level keys = %v, want exactly {logged_in, site, provider}", keysOf(raw))
		}
		if got := strings.TrimSpace(string(raw["logged_in"])); got != "false" {
			t.Errorf("logged_in = %s, want false", got)
		}
		for _, k := range []string{"site", "provider"} {
			if got := strings.TrimSpace(string(raw[k])); got != "null" {
				t.Errorf("%s = %s, want null", k, got)
			}
		}
	})
}

func TestRunStatus_MalformedConfigFails(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{"unparseable", "{"},
		{"unknown provider", `{"provider":"bogus","site":"acme.atlassian.net"}`},
		{"missing provider section", `{"provider":"oauth","site":"acme.atlassian.net"}`},
		{"containment-rejected site", `{"provider":"oauth","site":"not a host","oauth":{}}`},
	} {
		for _, form := range []struct {
			name string
			args []string
		}{
			{"text", nil},
			{"json", []string{"--json"}},
		} {
			t.Run(tt.name+"/"+form.name, func(t *testing.T) {
				configDir := withStatusConfigDir(t)
				writeRawStatusConfig(t, configDir, tt.body)

				var err error
				var stderr string
				stdout := captureStdout(t, func() {
					stderr = captureStderr(t, func() { err = runStatus(form.args) })
				})

				var silent *errSilent
				if !errors.As(err, &silent) {
					t.Fatalf("err = %v (%T), want *errSilent (exit 1)", err, err)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want empty (no verdict on the error path)", stdout)
				}
				if strings.Count(stderr, "\n") != 1 {
					t.Errorf("stderr = %q, want exactly one line", stderr)
				}
				if !strings.Contains(stderr, "cannot determine login state") {
					t.Errorf("stderr = %q, want the 'cannot determine login state' line", stderr)
				}
				if strings.Contains(stderr, "Logged in") || strings.Contains(stderr, "Not logged in") {
					t.Errorf("stderr = %q, must not state a login verdict", stderr)
				}
				if strings.Contains(stderr, "{") {
					t.Errorf("stderr = %q, want a plain line, not JSON", stderr)
				}
			})
		}
	}
}

func TestRunStatus_Help(t *testing.T) {
	for _, flagForm := range []string{"-h", "--help"} {
		t.Run(flagForm, func(t *testing.T) {
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runStatus([]string{flagForm}) })
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}
			if stdout != statusUsage+"\n" {
				t.Errorf("stdout = %q, want %q", stdout, statusUsage+"\n")
			}
			if !strings.Contains(stdout, "usage: jirahere status") {
				t.Errorf("stdout = %q, want status's bare usage header", stdout)
			}
			if !strings.Contains(stdout, "--json") {
				t.Errorf("stdout = %q, want the --json flag documented", stdout)
			}
			if !strings.Contains(stdout, "jirahere status set <id> --status <name>") {
				t.Errorf("stdout = %q, want the set form documented", stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderr)
			}
		})
	}
}

func TestRunStatus_HelpAnywhereBeforeSet(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"set --help", []string{"set", "--help"}},
		{"set <id> --help", []string{"set", "PROJ-1", "--help"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stdout := captureStdout(t, func() { err = runStatus(tt.args) })
			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}
			if stdout != statusUsage+"\n" {
				t.Errorf("stdout = %q, want the combined usage text %q", stdout, statusUsage+"\n")
			}
		})
	}
}

func TestRunStatus_DispatchesSetToRunStatusSet(t *testing.T) {
	var err error
	stderr := captureStderr(t, func() { err = runStatus([]string{"set", "--status", "Done"}) })
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %T, want *errSilent", err)
	}
	if got := strings.TrimSuffix(stderr, "\n"); got != statusSetUsage {
		t.Errorf("stderr = %q, want %q (proves routing to runStatusSet)", got, statusSetUsage)
	}
}

func TestRunStatus_UnknownTokenUsesStatusUsage(t *testing.T) {
	withStatusConfigDir(t)

	var err error
	stderr := captureStderr(t, func() { err = runStatus([]string{"bogus"}) })

	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %T, want *errSilent", err)
	}
	if got := strings.TrimSuffix(stderr, "\n"); got != statusRejectUsage {
		t.Errorf("stderr = %q, want %q", got, statusRejectUsage)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestRunStatus_SurplusPositional(t *testing.T) {
	withStatusConfigDir(t)

	var err error
	stderr := captureStderr(t, func() { err = runStatus([]string{"extra"}) })

	if err == nil || !strings.Contains(err.Error(), statusRejectUsage) {
		t.Fatalf("err = %v, want %q", err, statusRejectUsage)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one body-free usage line", stderr)
	}
}

func TestRunStatus_UnknownFlagSanitizesStderr(t *testing.T) {
	const poison = "--\x1b[31mBOGUS\x1b[0m\r\nInjected forged line"

	out := captureStderr(t, func() { _ = runStatus([]string{poison}) })

	if strings.Contains(out, "\x1b") {
		t.Errorf("stderr contains an unsanitized escape sequence: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("stderr contains an unsanitized carriage return: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("stderr has %d lines, want 1: %q", strings.Count(out, "\n"), out)
	}
}

func TestRenderStatusText_SanitizesSiteAndProvider(t *testing.T) {
	out := captureStdout(t, func() {
		if err := renderStatusText(true, "acme\r\n.evil\x1b[31m.net", "oa\ruth", ""); err != nil {
			t.Fatalf("renderStatusText: %v", err)
		}
	})

	if strings.Count(out, "\n") != 1 {
		t.Errorf("output has %d lines, want 1: %q", strings.Count(out, "\n"), out)
	}
	for _, bad := range []rune{'\r', 0x1b} {
		if strings.ContainsRune(out, bad) {
			t.Errorf("output retains poisoned rune %U: %q", bad, out)
		}
	}
}

func TestMainStatus_RejectPathAndHelpExact(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, tt := range []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
		wantErr  string
	}{
		{"status bogus", []string{"status", "bogus"}, 2, "", statusRejectUsage + "\n"},
		{"status --help", []string{"status", "--help"}, 0, statusUsage + "\n", ""},
		{"status set --help", []string{"status", "set", "--help"}, 0, statusUsage + "\n", ""},
		{"status set <id> --help", []string{"status", "set", "PROJ-1", "--help"}, 0, statusUsage + "\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != tt.wantExit {
				t.Errorf("args=%v: exit = %d, want %d", tt.args, exitCode, tt.wantExit)
			}
			if stdout != tt.wantOut {
				t.Errorf("args=%v: stdout = %q, want %q", tt.args, stdout, tt.wantOut)
			}
			if stderr != tt.wantErr {
				t.Errorf("args=%v: stderr = %q, want %q", tt.args, stderr, tt.wantErr)
			}
		})
	}
}

func TestMainDispatch_StatusIsReachableAndRootUsage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	stdout, stderr, exitCode := runJirahereProcess(t, "status")
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (not logged in still succeeds)", exitCode)
	}
	if strings.Contains(stderr, "not implemented yet") {
		t.Errorf("stderr = %q, status must be reachable, not fall through to the default case", stderr)
	}
	if want := "Not logged in. Run \"jirahere login\" first.\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	stdout, stderr, exitCode = runJirahereProcess(t, "--help")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "  status ") {
		t.Errorf("root help: stdout=%q stderr=%q exit=%d, want a `status` line on stdout", stdout, stderr, exitCode)
	}
	if !strings.Contains(stdout, "  status set ") {
		t.Errorf("root help: stdout=%q, want a `status set` line on stdout", stdout)
	}
	if strings.Contains(stdout, "set-status") {
		t.Errorf("root help: stdout=%q, set-status must no longer be listed", stdout)
	}
}
