package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientResolveCreateMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/issue/createmeta" {
			t.Fatalf("request = %s %s, want GET /rest/api/3/issue/createmeta", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("projectKeys"); got != "PROJ" {
			t.Errorf("projectKeys = %q, want PROJ", got)
		}
		if got := r.URL.Query().Get("expand"); got != "projects.issuetypes" {
			t.Errorf("expand = %q, want projects.issuetypes", got)
		}
		_, _ = w.Write([]byte(`{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task","subtask":false},{"id":"6","name":"Epic"},{"id":"7","name":"Subtask","subtask":true}]}]}`))
	}))
	defer srv.Close()

	metadata, err := NewClient(srv.URL, "Bearer token").ResolveCreateMetadata(context.Background(), "PROJ")
	if err != nil {
		t.Fatalf("ResolveCreateMetadata: %v", err)
	}
	if metadata.ProjectID != "10000" {
		t.Errorf("ProjectID = %q, want 10000", metadata.ProjectID)
	}
	if got, want := metadata.IssueTypes["Task"].ID, "5"; got != want {
		t.Errorf("Task ID = %q, want %q", got, want)
	}
	if got := metadata.IssueTypes["Task"].Subtask; got == nil || *got {
		t.Errorf("Task.Subtask = %v, want pointer to false", got)
	}
	if got := metadata.IssueTypes["Subtask"].Subtask; got == nil || !*got {
		t.Errorf("Subtask.Subtask = %v, want pointer to true", got)
	}
	if got := metadata.IssueTypes["Epic"].Subtask; got != nil {
		t.Errorf("Epic.Subtask = %v, want nil (field omitted from response)", *got)
	}
}

func TestClientResolveCreateMetadata_ProjectMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[]}`))
	}))
	defer srv.Close()

	metadata, err := NewClient(srv.URL, "").ResolveCreateMetadata(context.Background(), "MISSING")
	if err != nil {
		t.Fatalf("ResolveCreateMetadata: %v", err)
	}
	if metadata.ProjectID != "" || len(metadata.IssueTypes) != 0 {
		t.Errorf("metadata = %+v, want empty result", metadata)
	}
}

func TestClientResolveCreateMetadata_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("sensitive proxy text"))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "").ResolveCreateMetadata(context.Background(), "PROJ")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusForbidden {
		t.Fatalf("err = %v, want status error 403", err)
	}
}
