package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRunStatusSet_PreflightErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{"--status", "Done"}, statusSetUsage},
		{"empty id", []string{"", "--status", "Done"}, statusSetUsage},
		{"surplus positional", []string{"PROJ-1", "extra", "--status", "Done"}, statusSetUsage},
		{"missing status", []string{"PROJ-1"}, "jirahere: --status is required and must not be empty."},
		{"empty status", []string{"PROJ-1", "--status", ""}, "jirahere: --status is required and must not be empty."},
		{"bad flag", []string{"PROJ-1", "--bad", "value"}, "jirahere: flag provided but not defined: --bad"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runStatusSet(tt.args) })
			var silent *errSilent
			if !errors.As(err, &silent) {
				t.Fatalf("err = %T, want *errSilent", err)
			}
			if got := strings.TrimSuffix(stderr, "\n"); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want exactly one line", stderr)
			}
		})
	}
}

func statusSetHandler(t *testing.T, available []map[string]any, doPostStatus int, posted *string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/transitions"):
			w.Header().Set("Content-Type", "application/json")
			body, err := json.Marshal(map[string]any{"transitions": available})
			if err != nil {
				t.Fatalf("marshal transitions response: %v", err)
			}
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transitions"):
			var payload struct {
				Transition struct {
					ID string `json:"id"`
				} `json:"transition"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode transition request: %v", err)
			}
			if posted != nil {
				*posted = payload.Transition.ID
			}
			w.WriteHeader(doPostStatus)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestRunStatusSet_SingleMatchSuccess(t *testing.T) {
	var posted string
	handler := statusSetHandler(t, []map[string]any{
		{"id": "11", "to": map[string]any{"id": "3", "name": "In Progress"}},
		{"id": "21", "to": map[string]any{"id": "4", "name": "Done"}},
	}, http.StatusNoContent, &posted)
	createTestLogin(t, handler)

	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runStatusSet([]string{"PROJ-1", "--status", "Done"}) })
	})
	if err != nil {
		t.Fatalf("runStatusSet: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if stdout != "Status set: PROJ-1 -> Done\n" {
		t.Errorf("stdout = %q, want %q", stdout, "Status set: PROJ-1 -> Done\n")
	}
	if posted != "21" {
		t.Errorf("posted transition id = %q, want 21", posted)
	}
}

func TestRunStatusSet_ZeroMatch_ListsAvailable(t *testing.T) {
	handler := statusSetHandler(t, []map[string]any{
		{"id": "11", "to": map[string]any{"id": "3", "name": "In Progress"}},
		{"id": "31", "to": map[string]any{"id": "5", "name": "Blocked"}},
	}, http.StatusNoContent, nil)
	createTestLogin(t, handler)

	var err error
	stderr := captureStderr(t, func() { err = runStatusSet([]string{"PROJ-1", "--status", "Done"}) })
	if err == nil {
		t.Fatal("runStatusSet: expected error, got nil")
	}
	if !strings.Contains(stderr, `"Done" is not an available status`) {
		t.Errorf("stderr = %q, want it to name the requested status as unavailable", stderr)
	}
	if !strings.Contains(stderr, "In Progress") || !strings.Contains(stderr, "Blocked") {
		t.Errorf("stderr = %q, want it to list both available target status names", stderr)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestRunStatusSet_ZeroMatch_NoTransitionsAvailable(t *testing.T) {
	handler := statusSetHandler(t, []map[string]any{}, http.StatusNoContent, nil)
	createTestLogin(t, handler)

	var err error
	stderr := captureStderr(t, func() { err = runStatusSet([]string{"PROJ-1", "--status", "Done"}) })
	if err == nil {
		t.Fatal("runStatusSet: expected error, got nil")
	}
	if !strings.Contains(stderr, "no transitions are currently available") {
		t.Errorf("stderr = %q, want it to say no transitions are available", stderr)
	}
}

func TestRunStatusSet_MultiMatch_ReportsAmbiguity(t *testing.T) {
	handler := statusSetHandler(t, []map[string]any{
		{"id": "11", "to": map[string]any{"id": "4", "name": "Done"}},
		{"id": "12", "to": map[string]any{"id": "4", "name": "Done"}},
	}, http.StatusNoContent, nil)
	createTestLogin(t, handler)

	var err error
	stderr := captureStderr(t, func() { err = runStatusSet([]string{"PROJ-1", "--status", "Done"}) })
	if err == nil {
		t.Fatal("runStatusSet: expected error, got nil")
	}
	if !strings.Contains(stderr, "ambiguous") || !strings.Contains(stderr, "2 matching transitions") {
		t.Errorf("stderr = %q, want it to name the ambiguity and match count", stderr)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestRunStatusSet_TransitionsCallFailure(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("untrusted proxy response"))
	}
	createTestLogin(t, handler)

	var err error
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { err = runStatusSet([]string{"PROJ-1", "--status", "Done"}) })
	})
	if err == nil {
		t.Fatal("runStatusSet: expected error, got nil")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (nothing changed)", stdout)
	}
	if strings.Contains(stderr, "proxy response") {
		t.Errorf("stderr leaked response body: %q", stderr)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestRunStatusSet_DoTransitionFailure(t *testing.T) {
	handler := statusSetHandler(t, []map[string]any{
		{"id": "21", "to": map[string]any{"id": "4", "name": "Done"}},
	}, http.StatusBadRequest, nil)
	createTestLogin(t, handler)

	var err error
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { err = runStatusSet([]string{"PROJ-1", "--status", "Done"}) })
	})
	if err == nil {
		t.Fatal("runStatusSet: expected error, got nil")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestRunStatusSet_PoisonedStatusNeverEchoesRaw(t *testing.T) {
	poisoned := "Done\x1b[31m\r\nforged\u202e"
	handler := statusSetHandler(t, []map[string]any{
		{"id": "11", "to": map[string]any{"id": "3", "name": "In Progress"}},
	}, http.StatusNoContent, nil)
	createTestLogin(t, handler)

	var err error
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { err = runStatusSet([]string{"PROJ-1", "--status", poisoned}) })
	})
	if err == nil {
		t.Fatal("runStatusSet: expected error, got nil (zero matches)")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if strings.ContainsAny(stderr, "\x1b\r\u202e") {
		t.Errorf("stderr leaked raw control/ANSI/bidi bytes: %q", stderr)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line", stderr)
	}
}

func TestMainStatusSet_UsageAndHelpExitBehavior(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
		wantErr  string
	}{
		{"root help lists status set", []string{"--help"}, 0, "status set", ""},
		{"help", []string{"status", "set", "--help"}, 0, statusUsage, ""},
		{"missing id", []string{"status", "set", "--status", "Done"}, 2, "", statusSetUsage + "\n"},
		{"missing status", []string{"status", "set", "PROJ-1"}, 2, "", "jirahere: --status is required and must not be empty.\n"},
		{"set-status no longer a command", []string{"set-status", "PROJ-1", "--status", "Done"}, 2, "", `jirahere: command "set-status" not implemented yet` + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runJirahereProcess(t, tt.args...)
			if exitCode != tt.wantExit || (tt.wantOut != "" && !strings.Contains(stdout, tt.wantOut)) || (tt.wantOut == "" && stdout != "") || stderr != tt.wantErr {
				t.Errorf("args=%v: stdout=%q stderr=%q exit=%d", tt.args, stdout, stderr, exitCode)
			}
		})
	}
}
