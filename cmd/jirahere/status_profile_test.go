package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

func saveProfileStatusConfig(t *testing.T, prof, site string) {
	t.Helper()
	cfg := &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     site,
		APIToken: &auth.APITokenConfig{Email: "user@example.com", Token: "tok"},
	}
	if err := auth.Save(prof, cfg); err != nil {
		t.Fatalf("auth.Save(%q): %v", prof, err)
	}
}

func runStatusCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runStatus(args) })
	})
	return stdout, stderr, err
}

func TestRunStatus_Profile_LoggedIn(t *testing.T) {
	withStatusConfigDir(t)
	saveProfileStatusConfig(t, "", "default.atlassian.net")
	saveProfileStatusConfig(t, "work", "work.atlassian.net")

	stdout, stderr, err := runStatusCapture(t, "--profile", "work")
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if want := "Logged in to work.atlassian.net via API token (profile \"work\").\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestRunStatus_Profile_LoggedInJSON(t *testing.T) {
	withStatusConfigDir(t)
	saveProfileStatusConfig(t, "", "default.atlassian.net")
	saveProfileStatusConfig(t, "work", "work.atlassian.net")

	stdout, stderr, err := runStatusCapture(t, "--json", "--profile", "work")
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	var raw map[string]json.RawMessage
	if uerr := json.Unmarshal([]byte(stdout), &raw); uerr != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%q", uerr, stdout)
	}
	if len(raw) != 3 {
		t.Fatalf("top-level keys = %v, want exactly {logged_in, site, provider}", keysOf(raw))
	}
	for k, want := range map[string]string{"logged_in": "true", "site": `"work.atlassian.net"`, "provider": `"api-token"`} {
		if got := strings.TrimSpace(string(raw[k])); got != want {
			t.Errorf("%s = %s, want %s", k, got, want)
		}
	}
}

func TestRunStatus_Profile_NotLoggedIn(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		withStatusConfigDir(t)
		saveProfileStatusConfig(t, "", "default.atlassian.net")

		stdout, stderr, err := runStatusCapture(t, "--profile", "work")
		if err != nil {
			t.Fatalf("runStatus: %v", err)
		}
		if want := "Not logged in. Run \"jirahere login --profile work\" first.\n"; stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})

	t.Run("json", func(t *testing.T) {
		withStatusConfigDir(t)
		saveProfileStatusConfig(t, "", "default.atlassian.net")

		stdout, stderr, err := runStatusCapture(t, "--json", "--profile", "work")
		if err != nil {
			t.Fatalf("runStatus: %v", err)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		want := "{\n  \"logged_in\": false,\n  \"site\": null,\n  \"provider\": null\n}\n"
		if stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
	})
}

func TestRunStatus_Profile_DefaultLoggedOutIgnoresProfileLogin(t *testing.T) {
	withStatusConfigDir(t)
	saveProfileStatusConfig(t, "work", "work.atlassian.net")

	stdout, _, err := runStatusCapture(t)
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if want := "Not logged in. Run \"jirahere login\" first.\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRunStatus_Profile_MalformedConfigNamesProfilePath(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{"unparseable", "{"},
		{"unknown provider", `{"provider":"bogus","site":"acme.atlassian.net"}`},
		{"containment-rejected site", `{"provider":"oauth","site":"not a host","oauth":{}}`},
	} {
		for _, form := range []struct {
			name string
			args []string
		}{
			{"text", []string{"--profile", "work"}},
			{"json", []string{"--json", "--profile", "work"}},
		} {
			t.Run(tt.name+"/"+form.name, func(t *testing.T) {
				withStatusConfigDir(t)

				saveProfileStatusConfig(t, "", "default.atlassian.net")
				writeRawStatusConfig(t, mustConfigDir(t, "work"), tt.body)

				stdout, stderr, err := runStatusCapture(t, form.args...)

				var silent *errSilent
				if !errors.As(err, &silent) {
					t.Fatalf("err = %v (%T), want *errSilent (exit 1)", err, err)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want empty", stdout)
				}
				want := "jirahere: cannot determine login state: ~/.config/jirahere-profiles/work/config.json is unreadable, malformed, or holds an invalid site.\n"
				if stderr != want {
					t.Errorf("stderr = %q, want %q", stderr, want)
				}
				if strings.Contains(stderr, configPathDisplay) {
					t.Errorf("stderr = %q, must not name the default config path", stderr)
				}
			})
		}
	}
}

func TestStatusUnreadableFailure_NamesPath(t *testing.T) {
	for _, tt := range []struct {
		prof, path string
	}{
		{"", "~/.config/jirahere/config.json"},
		{"work", "~/.config/jirahere-profiles/work/config.json"},
	} {
		var err error
		stderr := captureStderr(t, func() { err = statusUnreadableFailure(tt.prof) })
		var silent *errSilent
		if !errors.As(err, &silent) {
			t.Fatalf("prof %q: err = %T, want *errSilent", tt.prof, err)
		}
		if !strings.Contains(stderr, " "+tt.path+" is unreadable") {
			t.Errorf("prof %q: stderr = %q, want it to name %s", tt.prof, stderr, tt.path)
		}
	}
}

func TestRunStatus_Profile_InvalidNameIsBodyFreeUsageError(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", []string{"--profile", ""}},
		{"empty json", []string{"--json", "--profile", ""}},
		{"path traversal", []string{"--profile", "../evil"}},
		{"separator", []string{"--profile", "a/b"}},
		{"leading dot", []string{"--profile", ".hidden"}},
		{"whitespace", []string{"--profile", "a b"}},
		{"too long", []string{"--profile", strings.Repeat("a", 65)}},
		{"control chars", []string{"--profile", "a\x1b[31mb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")

			stdout, stderr, err := runStatusCapture(t, tc.args...)

			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %v (%T), want an errSilent usage error", err, err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty (no verdict)", stdout)
			}
			if !strings.Contains(stderr, "--profile") {
				t.Errorf("stderr = %q, want it to name --profile", stderr)
			}
			if strings.Count(stderr, "\n") != 1 || strings.ContainsAny(stderr, "\x1b\r") {
				t.Errorf("stderr = %q, want exactly one sanitized line", stderr)
			}
			if entries, rerr := os.ReadDir(home); rerr != nil || len(entries) != 0 {
				t.Errorf("home dir entries = %v (err %v), want none", entries, rerr)
			}
		})
	}
}

func TestRunStatus_Profile_FlagMissingValue(t *testing.T) {
	withStatusConfigDir(t)

	stdout, stderr, err := runStatusCapture(t, "--profile")
	if err == nil {
		t.Fatal("err = nil, want a usage error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestRenderStatusText_Profile_SanitizesName(t *testing.T) {
	for _, loggedIn := range []bool{true, false} {
		out := captureStdout(t, func() {
			if err := renderStatusText(loggedIn, "acme.atlassian.net", "oauth", "wo\r\nrk\x1b[31m"); err != nil {
				t.Fatalf("renderStatusText: %v", err)
			}
		})
		if strings.Count(out, "\n") != 1 || strings.ContainsAny(out, "\r\x1b") {
			t.Errorf("loggedIn=%v: output = %q, want one sanitized line", loggedIn, out)
		}
	}
}

func TestMainStatus_Profile_ExitCodes(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	saveProfileStatusConfig(t, "work", "work.atlassian.net")
	writeRawStatusConfig(t, filepath.Join(cfgHome, "jirahere-profiles", "broken"), "{")

	for _, tt := range []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
		wantErr  string
	}{
		{
			"logged in", []string{"status", "--profile", "work"}, 0,
			"Logged in to work.atlassian.net via API token (profile \"work\").\n", "",
		},
		{
			"not logged in", []string{"status", "--profile", "other"}, 0,
			"Not logged in. Run \"jirahere login --profile other\" first.\n", "",
		},
		{
			"unreadable", []string{"status", "--profile", "broken"}, 1, "",
			"jirahere: cannot determine login state: ~/.config/jirahere-profiles/broken/config.json is unreadable, malformed, or holds an invalid site.\n",
		},
		{"empty name", []string{"status", "--profile", ""}, 2, "", ""},
		{"invalid name", []string{"status", "--profile", "a/b"}, 2, "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != tt.wantExit {
				t.Errorf("exit = %d, want %d (stderr=%q)", exitCode, tt.wantExit, stderr)
			}
			if stdout != tt.wantOut {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantOut)
			}
			if tt.wantErr != "" && stderr != tt.wantErr {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantErr)
			}
			if tt.wantExit == 2 && (!strings.Contains(stderr, "--profile") || strings.Count(stderr, "\n") != 1) {
				t.Errorf("stderr = %q, want one usage line naming --profile", stderr)
			}
		})
	}
}

func TestMainStatus_NoProfile_ByteIdentical(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	stdout, stderr, exit := runJirahereProcess(t, "status")
	if exit != 0 || stderr != "" || stdout != "Not logged in. Run \"jirahere login\" first.\n" {
		t.Errorf("not logged in: out=%q err=%q exit=%d", stdout, stderr, exit)
	}

	saveProfileStatusConfig(t, "", "acme.atlassian.net")
	stdout, stderr, exit = runJirahereProcess(t, "status")
	if exit != 0 || stderr != "" || stdout != "Logged in to acme.atlassian.net via API token.\n" {
		t.Errorf("logged in: out=%q err=%q exit=%d", stdout, stderr, exit)
	}

	if err := os.WriteFile(filepath.Join(cfgHome, "jirahere", "config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exit = runJirahereProcess(t, "status")
	want := "jirahere: cannot determine login state: ~/.config/jirahere/config.json is unreadable, malformed, or holds an invalid site.\n"
	if exit != 1 || stdout != "" || stderr != want {
		t.Errorf("unreadable: out=%q err=%q exit=%d, want stderr %q", stdout, stderr, exit, want)
	}
}

func TestStatusUsage_DocumentsProfile(t *testing.T) {
	const wantUsage = "usage: jirahere status [--json] [--profile <name>]\n       jirahere status set <id> --status <name> [--profile <name>]"
	const wantReject = `usage: jirahere status [--json] [--profile <name>] (see "jirahere status --help" for status set)`
	if statusUsage != wantUsage {
		t.Errorf("statusUsage = %q, want %q", statusUsage, wantUsage)
	}
	if statusRejectUsage != wantReject {
		t.Errorf("statusRejectUsage = %q, want %q", statusRejectUsage, wantReject)
	}
}

func TestRunStatus_Profile_EqualsForm(t *testing.T) {
	withStatusConfigDir(t)
	saveProfileStatusConfig(t, "", "default.atlassian.net")
	saveProfileStatusConfig(t, "work", "work.atlassian.net")

	stdout, _, err := runStatusCapture(t, "--profile=work")
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if want := "Logged in to work.atlassian.net via API token (profile \"work\").\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRunStatus_SetAcceptsProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	handler := statusSetHandler(t, []map[string]any{
		{"id": "21", "to": map[string]any{"id": "4", "name": "Done"}},
	}, http.StatusNoContent, nil)
	createTestLogin(t, handler)
	saveCreateProfileLogin(t, "work", "work@example.com", "work-token")

	stdout, stderr, err := runStatusCapture(t, "set", "PROJ-1", "--status", "Done", "--profile", "work")
	if err != nil {
		t.Fatalf("runStatus: %v (stderr %q)", err, stderr)
	}
	if stdout != "Status set: PROJ-1 -> Done\n" {
		t.Errorf("stdout = %q, want the transition confirmation", stdout)
	}
}
