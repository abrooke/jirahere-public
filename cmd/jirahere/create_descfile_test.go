package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func createBase() []string {
	return []string{"--summary", "S", "--project", "PROJ", "--type", "Task"}
}

func TestRunCreate_DescriptionFileMatchesDescriptionPayload(t *testing.T) {
	text := "it's para one\nstill one\n\n*para* two"
	path := filepath.Join(t.TempDir(), "desc.txt")
	if err := os.WriteFile(path, []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, stdin string, extra ...string) string {
		t.Helper()
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		fields := createCapturePost(t, "PROJ-7")
		if stdin != "" {
			setStdin(t, stdin)
		}
		var err error
		_ = captureStdout(t, func() { err = runCreate(append(createBase(), extra...)) })
		if err != nil {
			t.Fatalf("runCreate = %v, want nil", err)
		}
		desc, ok := (*fields)["description"]
		if !ok {
			t.Fatalf("fields = %v, want a description", *fields)
		}
		b, _ := json.Marshal(desc)
		return string(b)
	}
	var want string
	t.Run("description", func(t *testing.T) { want = run(t, "", "--description", text) })
	t.Run("file", func(t *testing.T) {
		if got := run(t, "", "--description-file", path); got != want {
			t.Errorf("description = %s, want %s", got, want)
		}
	})
	t.Run("stdin", func(t *testing.T) {
		if got := run(t, text+"\n", "--description-file", "-"); got != want {
			t.Errorf("description = %s, want %s", got, want)
		}
	})
}

func TestRunCreate_DescriptionFilePreflightErrors(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	ok := filepath.Join(dir, "ok.txt")
	missing := filepath.Join(dir, "missing.txt")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ok, []byte("SECRETBODY"), 0o600); err != nil {
		t.Fatal(err)
	}
	const both = "jirahere: create: --description and --description-file may not be used together. Nothing was created."
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"both flags", []string{"--description", "x", "--description-file", ok}, both},
		{"both flags, empty description", []string{"--description", "", "--description-file", ok}, both},
		{"missing file", []string{"--description-file", missing}, "jirahere: create: body file " + strconv.Quote(missing) + " could not be read: no such file or directory. Nothing was created."},
		{"empty file", []string{"--description-file", empty}, "jirahere: create: body file " + strconv.Quote(empty) + " is empty or whitespace-only. Nothing was created."},
		{"empty stdin", []string{"--description-file", "-"}, "jirahere: create: body from stdin is empty or whitespace-only. Nothing was created."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			var requests int
			createTestLogin(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
			setStdin(t, "")
			var err error
			var stderr string
			stdout := captureStdout(t, func() {
				stderr = captureStderr(t, func() { err = runCreate(append(createBase(), tt.args...)) })
			})
			if err == nil {
				t.Fatal("err = nil, want failure")
			}

			var usageErr *errUsage
			if isUsage := errors.As(err, &usageErr); isUsage != strings.HasPrefix(tt.name, "both flags") {
				t.Errorf("errors.As(*errUsage) = %v, want %v", isUsage, strings.HasPrefix(tt.name, "both flags"))
			}
			if got := strings.TrimSuffix(stderr+err.Error(), "\n"); !strings.Contains(got, tt.want) {
				t.Errorf("diagnostic = %q, want %q", got, tt.want)
			}
			if strings.Contains(stderr+err.Error(), "SECRETBODY") {
				t.Errorf("file content echoed in error output")
			}
			if stdout != "" || requests != 0 {
				t.Errorf("stdout/requests = %q/%d, want empty/0", stdout, requests)
			}
		})
	}
}

func TestHelp_CreateListsDescriptionFile(t *testing.T) {
	stdout, stderr, exit := runJirahereProcess(t, "create", "--help")
	if exit != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q, want 0/empty", exit, stderr)
	}
	for _, want := range []string{"--description-file", "stdin"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want %q listed", stdout, want)
		}
	}
}
