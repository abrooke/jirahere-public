package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func setStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	_ = w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig; _ = r.Close() })
}

func descriptionSetPayload(t *testing.T, args ...string) (payload string, stdout string) {
	t.Helper()
	var requests int
	commentTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		b, _ := io.ReadAll(r.Body)
		payload = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	var err error
	stdout = captureStdout(t, func() { err = runDescription(append([]string{"set"}, args...)) })
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	return payload, stdout
}

func TestRunDescriptionSet_BodyFileMatchesBodyPayload(t *testing.T) {
	text := "it's first\nsecond\n\nthird"
	path := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(path, []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var want string
	t.Run("body", func(t *testing.T) {
		want, _ = descriptionSetPayload(t, "PROJ-1", "--body", text)
	})
	for _, tt := range []struct {
		name  string
		stdin string
		args  []string
	}{
		{"file", "", []string{"PROJ-1", "--body-file", path}},
		{"stdin", text + "\n", []string{"PROJ-1", "--body-file", "-"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.stdin != "" {
				setStdin(t, tt.stdin)
			}
			got, stdout := descriptionSetPayload(t, tt.args...)
			if got != want {
				t.Errorf("PUT payload = %s, want %s", got, want)
			}
			if stdout != "Description set: PROJ-1\n" {
				t.Errorf("stdout = %q", stdout)
			}
		})
	}
}

func TestRunDescriptionSet_BodyFilePreflightErrorsMakeNoNetworkCall(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(ok, []byte("SECRETBODY"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"both flags", []string{"PROJ-1", "--body", "x", "--body-file", ok}, "jirahere: description set: --body and --body-file may not be used together."},
		{"both flags, empty body", []string{"PROJ-1", "--body", "", "--body-file", ok}, "jirahere: description set: --body and --body-file may not be used together."},
		{"missing file", []string{"PROJ-1", "--body-file", missing}, "jirahere: description set: body file " + strconv.Quote(missing) + " could not be read: no such file or directory."},
		{"empty file", []string{"PROJ-1", "--body-file", empty}, "jirahere: description set: body file " + strconv.Quote(empty) + " is empty or whitespace-only."},
		{"empty stdin", []string{"PROJ-1", "--body-file", "-"}, "jirahere: description set: body from stdin is empty or whitespace-only."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			commentTestLogin(t, descriptionTestHandler(t, `{}`, &requests))
			setStdin(t, "")
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runDescription(append([]string{"set"}, tt.args...)) })
			})
			if err == nil {
				t.Fatal("err = nil, want failure")
			}

			var usageErr *errUsage
			if isUsage := errors.As(err, &usageErr); isUsage != strings.HasPrefix(tt.name, "both flags") {
				t.Errorf("errors.As(*errUsage) = %v, want %v", isUsage, strings.HasPrefix(tt.name, "both flags"))
			}
			if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
			if strings.Contains(stderr, "SECRETBODY") {
				t.Errorf("file content echoed in error output")
			}
			if stdout != "" || requests != 0 {
				t.Errorf("stdout/requests = %q/%d, want empty/0", stdout, requests)
			}
		})
	}
}
