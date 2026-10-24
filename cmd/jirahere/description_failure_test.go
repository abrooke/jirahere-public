package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

const descriptionSetFailureSecret = "s3cr3t-token <script>alert(1)</script> \x1b[31m"

func assertDescriptionSetFailure(t *testing.T, err error, stdout, stderr, want string) {
	t.Helper()
	var silent *errSilent
	if !errors.As(err, &silent) {
		t.Fatalf("err = %T (%v), want *errSilent", err, err)
	}
	var usageErr *errUsage
	if errors.As(err, &usageErr) {
		t.Errorf("err is *errUsage (exit 2), want a runtime failure (exit 1)")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if got := strings.TrimSuffix(stderr, "\n"); got != want || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line %q", stderr, want)
	}
	for _, leak := range []string{"s3cr3t", "<script>", "alert", "proxy", "hello"} {
		if strings.Contains(stderr, leak) || strings.Contains(err.Error(), leak) {
			t.Errorf("output leaks %q: stderr=%q err=%q", leak, stderr, err.Error())
		}
	}
	if strings.ContainsAny(stderr, "\x1b\r") {
		t.Errorf("stderr = %q carries control bytes", stderr)
	}
}

func runDescriptionSetForFailure(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			err = runDescription([]string{"set", "PROJ-1", "--body", "hello"})
		})
	})
	return stdout, stderr, err
}

type failingTransport struct {
	n   *int
	err error
}

func (rt failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	*rt.n++
	return nil, rt.err
}

func TestRunDescriptionSetFailure_AuthLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path, err := auth.ConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json "+descriptionSetFailureSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests int
	orig := http.DefaultTransport
	http.DefaultTransport = failingTransport{n: &requests, err: errors.New("unexpected network call")}
	t.Cleanup(func() { http.DefaultTransport = orig })

	const want = `jirahere: could not load Jira credentials. Run "jirahere login" again.`
	stdout, stderr, err := runDescriptionSetForFailure(t)
	assertDescriptionSetFailure(t, err, stdout, stderr, want)
	if requests != 0 {
		t.Errorf("requests = %d, want 0 (no write)", requests)
	}

	pOut, pErr, code := runJirahereProcess(t, "description", "set", "PROJ-1", "--body", "hello")
	if code != 1 || pOut != "" || pErr != want+"\n" {
		t.Errorf("process: code/stdout/stderr = %d/%q/%q", code, pOut, pErr)
	}
}

func TestRunDescriptionSetFailure_StatusErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		want   string
	}{
		{"404 unknown id", http.StatusNotFound, "jirahere: description set failed: Jira returned an unexpected response (404)."},
		{"401", http.StatusUnauthorized, "jirahere: description set failed: Jira returned an unexpected response (401)."},
		{"403", http.StatusForbidden, "jirahere: description set failed: Jira returned an unexpected response (403)."},
		{"400 description not editable", http.StatusBadRequest, "jirahere: description set failed: Jira returned an unexpected response (400)."},
		{"other non-2xx", http.StatusBadGateway, "jirahere: description set failed: Jira returned an unexpected response (502)."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"errorMessages":["proxy says ` + descriptionSetFailureSecret + `"]}`))
			})
			stdout, stderr, err := runDescriptionSetForFailure(t)
			assertDescriptionSetFailure(t, err, stdout, stderr, tt.want)
			if requests != 1 {
				t.Errorf("requests = %d, want exactly the one failed PUT", requests)
			}
		})
	}
}

func TestRunDescriptionSetFailure_Transport(t *testing.T) {
	commentTestLogin(t, nil)
	var requests int
	orig := http.DefaultTransport
	http.DefaultTransport = failingTransport{n: &requests, err: errors.New("proxy dial failed: " + descriptionSetFailureSecret)}
	t.Cleanup(func() { http.DefaultTransport = orig })

	stdout, stderr, err := runDescriptionSetForFailure(t)
	assertDescriptionSetFailure(t, err, stdout, stderr, "jirahere: description set failed: could not reach Jira.")
	if requests != 1 {
		t.Errorf("requests = %d, want 1 attempted PUT", requests)
	}
}
