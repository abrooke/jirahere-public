package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

func TestFailf_PrintsAndReturnsSilentError(t *testing.T) {
	err := failf("Example message %d.", 42)
	if err == nil {
		t.Fatal("failf returned nil error")
	}
	if err.Error() != "Example message 42." {
		t.Errorf("err.Error() = %q, want %q", err.Error(), "Example message 42.")
	}
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Errorf("failf did not return an error wrapping *errSilent")
	}
}

func TestFailf_SanitizesStderr(t *testing.T) {
	out := captureStderr(t, func() {
		_ = failf("jirahere: %s.", "\x1b[31mHIDDEN\x1b[0m\r\nInjected forged line")
	})

	if strings.Contains(out, "\x1b") {
		t.Errorf("stderr contains an unsanitized escape sequence: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("stderr contains an unsanitized carriage return: %q", out)
	}
	if want := 1; strings.Count(out, "\n") != want {
		t.Errorf("stderr has %d lines, want %d: %q", strings.Count(out, "\n"), want, out)
	}
}

func TestPrintExistingConfigNotice_SanitizesSite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net\r\nCurrently logged in to evil.example.com",
		APIToken: &auth.APITokenConfig{Email: "a@example.com", Token: "tok"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	path, err := auth.ConfigPath("")
	if err != nil {
		t.Fatalf("auth.ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(t, func() { printExistingConfigNotice("") })

	if strings.Contains(out, "\r") {
		t.Errorf("output contains an unsanitized carriage return: %q", out)
	}
	if want := 1; strings.Count(out, "\n") != want {
		t.Errorf("output has %d lines, want %d (a CR/LF in the stored site must not add a line): %q", strings.Count(out, "\n"), want, out)
	}
}

func TestPrintExistingConfigNotice_SanitizesUnknownProviderLabel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	provider := "custom\x1b[31m\r\n\u009b\u202E\u2067provider"
	cfg := auth.Config{Provider: provider, Site: "acme.atlassian.net"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	path, err := auth.ConfigPath("")
	if err != nil {
		t.Fatalf("auth.ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(t, func() { printExistingConfigNotice("") })
	if strings.ContainsAny(out, "\x1b\r\u009b\u202e\u2067") || strings.Count(out, "\n") != 1 {
		t.Errorf("output contains terminal controls or forged line: %q", out)
	}
	if !strings.Contains(out, "custom[31mprovider") {
		t.Errorf("output lost printable provider text: %q", out)
	}
}

func TestResolveField_UsesFlagValueWithoutPrompting(t *testing.T) {
	got, err := resolveField(nil, "acme.atlassian.net", "Jira site", "site", false)
	if err != nil {
		t.Fatalf("resolveField: %v", err)
	}
	if got != "acme.atlassian.net" {
		t.Errorf("got %q, want %q", got, "acme.atlassian.net")
	}
}

func TestResolveField_NonInteractiveMissingFlagFails(t *testing.T) {
	_, err := resolveField(nil, "", "Jira site", "site", false)
	if err == nil {
		t.Fatal("expected error for missing flag in non-interactive mode, got nil")
	}
	if err.Error() != "login: --site is required when running non-interactively" {
		t.Errorf("err = %q, unexpected message", err.Error())
	}
}

func TestApiTokenLoginFailure_401(t *testing.T) {
	err := apiTokenLoginFailure("acme.atlassian.net", &jira.StatusError{StatusCode: 401, Body: "unauthorized"})
	want := "Login failed: Jira rejected the email/token combination (401 Unauthorized). Check your email and API token and try again."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestApiTokenLoginFailure_OtherStatus(t *testing.T) {
	err := apiTokenLoginFailure("acme.atlassian.net", &jira.StatusError{StatusCode: 500, Body: "boom"})
	want := "Login failed: Jira returned an unexpected response (500) from acme.atlassian.net."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestApiTokenLoginFailure_DNSError(t *testing.T) {
	err := apiTokenLoginFailure("bogus.atlassian.net", &net.DNSError{Err: "no such host", Name: "bogus.atlassian.net", IsNotFound: true})
	want := `Could not reach "bogus.atlassian.net" — check the site URL (it should look like yourteam.atlassian.net).`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestIdentityLine_Success(t *testing.T) {
	line := identityLine(&jira.Identity{Name: "Aslan Brooke", Email: "aslan@example.com"}, nil)
	want := "Logged in as Aslan Brooke <aslan@example.com>."
	if line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
}

func TestIdentityLine_FailureSkipsLine(t *testing.T) {
	line := identityLine(nil, errors.New("network error"))
	if line != "" {
		t.Errorf("line = %q, want empty string on lookup failure", line)
	}
}

func TestIdentityLine_SanitizesSingleLineUnicodeControls(t *testing.T) {
	line := identityLine(&jira.Identity{Name: "Name\x1b[31m\r\n\u009b", Email: "mail\t@example.com"}, nil)
	if strings.ContainsAny(line, "\x1b\r\n\t\u009b") {
		t.Errorf("identity line contains terminal controls: %q", line)
	}
	if !strings.Contains(line, "Name[31m") || !strings.Contains(line, "mail@example.com") {
		t.Errorf("identity line lost printable text: %q", line)
	}
}

func TestIdentityLine_StripsBidiFormatControls(t *testing.T) {
	line := identityLine(&jira.Identity{Name: "Ada\u202ELovelace", Email: "ada\u2067@example.com"}, nil)
	if strings.ContainsAny(line, "\u202e\u2067") {
		t.Errorf("identity line contains bidi format controls: %q", line)
	}
	if !strings.Contains(line, "AdaLovelace <ada@example.com>") {
		t.Errorf("identity line lost visible text: %q", line)
	}
}

func TestPromptForSite_SanitizesRemoteURLControls(t *testing.T) {
	originalStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString("1\n"); err != nil {
		t.Fatalf("write choice: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = originalStdin; _ = r.Close() }()

	var idx int
	out := captureStdout(t, func() {
		idx, err = promptForSite([]auth.Site{{URL: "https://acme.atlassian.net\x1b[31m\r\nFORGED\u009b"}})
	})
	if err != nil || idx != 0 {
		t.Fatalf("promptForSite = (%d, %v), want (0, nil)", idx, err)
	}
	if strings.ContainsAny(out, "\x1b\r\u009b") || strings.Count(out, "\n") != 2 {
		t.Errorf("picker output contains forged terminal content: %q", out)
	}
}

func TestApiTokenLoginFailure_GenericNetworkError(t *testing.T) {
	err := apiTokenLoginFailure("acme.atlassian.net", errors.New("connection reset by peer"))
	want := "Could not reach Atlassian to complete login. Check your network connection and try again."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestAPITokenLoginFailure_TransportErrorDoesNotReachStderr(t *testing.T) {
	const secret = "transport-secret-must-not-leak"
	err := fmt.Errorf("validating API token: %w", &url.Error{Op: "Get", URL: "https://example.invalid", Err: errors.New(secret)})

	var loginErr error
	stderr := captureStderr(t, func() {
		loginErr = apiTokenLoginFailure("acme.atlassian.net", err)
	})
	if strings.Contains(loginErr.Error(), secret) {
		t.Errorf("returned error exposes transport secret: %q", loginErr)
	}
	if strings.Contains(stderr, secret) {
		t.Errorf("stderr exposes transport secret: %q", stderr)
	}
}

func TestOAuthLoginFailure_StatusBodyDoesNotReachStderr(t *testing.T) {
	const secret = "oauth-status-body-secret-must-not-leak"

	var loginErr error
	stderr := captureStderr(t, func() {
		loginErr = oauthLoginFailure("exchanging the authorization code", &jira.StatusError{StatusCode: 400, Body: secret})
	})
	if strings.Contains(loginErr.Error(), secret) {
		t.Errorf("returned error exposes OAuth response secret: %q", loginErr)
	}
	if strings.Contains(stderr, secret) {
		t.Errorf("stderr exposes OAuth response secret: %q", stderr)
	}
	if !strings.Contains(loginErr.Error(), "(400)") {
		t.Errorf("returned error = %q, want safe HTTP status", loginErr)
	}
}

func TestOAuthLoginFailure_ExchangeStatusIsRejectionNotNetworkFailure(t *testing.T) {
	got := oauthLoginFailure("exchanging the authorization code", &auth.ExchangeStatusError{StatusCode: 400})
	if !strings.Contains(got.Error(), "(400)") {
		t.Errorf("returned error = %q, want it to name the HTTP status", got)
	}
	if strings.Contains(got.Error(), "Could not reach Atlassian") {
		t.Errorf("returned error = %q, a rejected exchange is not a reachability failure", got)
	}

	networkErr := oauthLoginFailure("exchanging the authorization code", errors.New("connection reset by peer"))
	if !strings.Contains(networkErr.Error(), "Could not reach Atlassian") {
		t.Errorf("network error = %q, want the generic reachability message", networkErr)
	}
}

func TestOAuthLoginFailure_StageDistinguishesCallSites(t *testing.T) {
	genericErr := errors.New("connection reset by peer")
	exchangeErr := oauthLoginFailure("exchanging the authorization code", genericErr)
	resourcesErr := oauthLoginFailure("looking up your Jira sites", genericErr)

	if exchangeErr.Error() == resourcesErr.Error() {
		t.Errorf("expected distinct messages per stage, got the same for both: %q", exchangeErr.Error())
	}
	if !strings.Contains(exchangeErr.Error(), "exchanging the authorization code") {
		t.Errorf("exchange error = %q, want it to name the failing stage", exchangeErr.Error())
	}
	if !strings.Contains(resourcesErr.Error(), "looking up your Jira sites") {
		t.Errorf("resources error = %q, want it to name the failing stage", resourcesErr.Error())
	}
}

func TestOAuthLoginFailure_HTTPStatusBoundaries(t *testing.T) {
	for _, status := range []int{99, 100, 599, 600} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			var got error
			out := captureStderr(t, func() {
				got = oauthLoginFailure("exchanging the authorization code", &jira.StatusError{StatusCode: status, Body: "untrusted"})
			})
			marker := fmt.Sprintf("(%d)", status)
			wantStatus := status >= 100 && status <= 599
			if strings.Contains(got.Error(), "untrusted") || strings.Contains(out, "untrusted") {
				t.Errorf("OAuth output leaked response body: error=%q stderr=%q", got, out)
			}
			if strings.Contains(got.Error(), marker) != wantStatus || strings.Contains(out, marker) != wantStatus {
				t.Errorf("error=%q stderr=%q, status display want %t", got, out, wantStatus)
			}
		})
	}
}

func TestApiTokenLoginFailure_DialRefused(t *testing.T) {
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	err := apiTokenLoginFailure("acme.atlassian.net", opErr)
	want := `Could not reach "acme.atlassian.net" — check the site URL (it should look like yourteam.atlassian.net).`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestApiTokenLoginFailure_NonJiraSiteJSONError(t *testing.T) {

	err := json.Unmarshal([]byte("<html>not json</html>"), &struct{}{})
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("test setup: expected *json.SyntaxError, got %T", err)
	}
	wrapped := apiTokenLoginFailure("acme.example.com", err)
	want := `Could not reach "acme.example.com" — check the site URL (it should look like yourteam.atlassian.net).`
	if wrapped.Error() != want {
		t.Errorf("err = %q, want %q", wrapped.Error(), want)
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	os.Args = append([]string{"jirahere"}, args...)
	main()
	os.Exit(0)
}

func runJirahereProcess(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("runJirahereProcess(%v): %v", args, err)
		}
		exitCode = exitErr.ExitCode()
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestMainHelp_ExitsZeroWithUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"login -h", []string{"login", "-h"}, "--api-token"},
		{"login --help", []string{"login", "--help"}, "--site string"},
		{"logout -h", []string{"logout", "-h"}, "usage: jirahere logout [options]"},
		{"logout --help", []string{"logout", "--help"}, "--profile profile"},
		{"clone -h", []string{"clone", "PROJ-123", "-h"}, "--summary-old value"},
		{"clone --help", []string{"clone", "PROJ-123", "--help"}, "--parent value"},
		{"move -h", []string{"move", "PROJ-123", "-h"}, "--parent value"},
		{"move --help", []string{"move", "PROJ-123", "--help"}, "--label-old value"},
		{"rehome-children -h", []string{"rehome-children", "PROJ-100", "-h"}, "--to value"},
		{"rehome-children --help", []string{"rehome-children", "PROJ-100", "--help"}, "--skip-status value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)

			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0", exitCode)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tt.want)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty (no additional \"jirahere: ...\" line on a successful -h/--help)", stderr)
			}
		})
	}
}

func TestMainPositionalCommandHelpPrecedesArityValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"clone long", []string{"clone", "--help"}, "usage: jirahere clone <source-key> [options]"},
		{"clone short", []string{"clone", "-h"}, "usage: jirahere clone <source-key> [options]"},
		{"move long", []string{"move", "--help"}, "usage: jirahere move <source-key> [options]"},
		{"move short", []string{"move", "-h"}, "usage: jirahere move <source-key> [options]"},
		{"rehome long", []string{"rehome-children", "--help"}, "usage: jirahere rehome-children <old-parent-key> [options]"},
		{"rehome short", []string{"rehome-children", "-h"}, "usage: jirahere rehome-children <old-parent-key> [options]"},
		{"context get long", []string{"context", "get", "--help"}, "usage: jirahere context get <id> [options]"},
		{"context get short", []string{"context", "get", "-h"}, "usage: jirahere context get <id> [options]"},
		{"skills install long", []string{"skills", "install", "--help"}, "usage: jirahere skills install <app> [<agent-name>] [options]"},
		{"skills install short", []string{"skills", "install", "-h"}, "usage: jirahere skills install <app> [<agent-name>] [options]"},
		{"context group long", []string{"context", "--help"}, "usage: jirahere context get <id> [options]"},
		{"context group short", []string{"context", "-h"}, "usage: jirahere context get <id> [options]"},
		{"skills group long", []string{"skills", "--help"}, "usage: jirahere skills install <app> [<agent-name>] [options]"},
		{"skills group short", []string{"skills", "-h"}, "usage: jirahere skills install <app> [<agent-name>] [options]"},
		{"before positional", []string{"clone", "--help", "PROJ-123"}, "usage: jirahere clone <source-key> [options]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != 0 || stderr != "" || !strings.Contains(stdout, tt.want) {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d, want help on stdout and exit 0", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}

func TestMainPositionalCommandHelpPreservesUsageErrorsAndDoubleDashBoundary(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"missing clone positional", []string{"clone"}},
		{"surplus move positional", []string{"move", "PROJ-1", "PROJ-2"}},
		{"double dash hides help", []string{"clone", "--", "--help", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != 2 || stdout != "" || !strings.HasPrefix(stderr, "usage: jirahere ") {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d, want unchanged usage error", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}

func TestMainUnknownFlag_ExitsTwoSanitized(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "login", "--nope")

	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2", exitCode)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	want := "jirahere: flag provided but not defined: --nope\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestMainHelp_RootListsRehomeChildren(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "--help")

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	found := false
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.Contains(line, "rehome-children") {
			continue
		}
		found = true
		if strings.Contains(line, "not implemented") {
			t.Errorf("root help line = %q, must not claim rehome-children is unimplemented", line)
		}
	}
	if !found {
		t.Errorf("stdout = %q, want the root help listing to mention rehome-children", stdout)
	}
}

func TestMainDispatch_RehomeChildrenIsReachable(t *testing.T) {
	stdout, stderr, exitCode := runJirahereProcess(t, "rehome-children")

	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2 (missing required arguments)", exitCode)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if strings.Contains(stderr, "not implemented") {
		t.Errorf("stderr = %q, rehome-children must be reachable, not fall through to the unimplemented-command default", stderr)
	}
	if !strings.HasPrefix(stderr, "usage: jirahere rehome-children ") {
		t.Errorf("stderr = %q, want it to start with rehome-children's usage string", stderr)
	}
}

func TestMainDispatch_CreateIsReachable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	stdout, stderr, exitCode := runJirahereProcess(t, "create", "--summary", "Widget rollout", "--project", "PROJ", "--type", "Task")

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 (authentication failure)", exitCode)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if strings.Contains(stderr, `command "create" not implemented yet`) {
		t.Errorf("stderr = %q, create must be reachable, not fall through to the unimplemented-command default", stderr)
	}
	if want := "jirahere: not logged in. Run \"jirahere login\" first.\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestSanitizeForTerminal_StripsControlChars(t *testing.T) {
	got := sanitizeForTerminal("Evil\x1b[31mName\x07 <evil\x00@example.com>")
	want := "Evil[31mName <evil@example.com>"
	if got != want {
		t.Errorf("sanitizeForTerminal = %q, want %q", got, want)
	}
}

func TestTerminalSanitizers_StripC1ControlsPreserveUnicode(t *testing.T) {
	input := "caf\u00e9\u009b]\u0080ok"
	for _, sanitize := range []func(string) string{sanitizeForTerminal, sanitizeSingleLine} {
		if got, want := sanitize(input), "caf\u00e9]ok"; got != want {
			t.Errorf("sanitizer = %q, want %q", got, want)
		}
	}
}
