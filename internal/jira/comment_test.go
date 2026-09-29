package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_AddComment_Success(t *testing.T) {
	body := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/rest/api/3/issue/PROJ-123/comment"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/json"; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
			t.Errorf("Content-Type = %q, want %q", got, want)
		}
		var payload struct {
			Body json.RawMessage `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if string(payload.Body) != string(body) {
			t.Errorf("body = %s, want %s", payload.Body, body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"10001","created":"ignored"}`))
	}))
	defer srv.Close()

	result, err := NewClient(srv.URL, "Bearer token").AddComment(context.Background(), "PROJ-123", body)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if result.ID != "10001" {
		t.Errorf("ID = %q, want 10001", result.ID)
	}
}

func TestClient_AddComment_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorMessages":["untrusted"]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "Bearer token").AddComment(context.Background(), "PROJ-404", json.RawMessage(`{"type":"doc"}`))
	if !IsNotFound(err) {
		t.Errorf("AddComment error = %v, want a 404 StatusError", err)
	}
}

func TestClient_ListComments_SinglePage_OrderPreserved(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got, want := r.Method, http.MethodGet; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/rest/api/3/issue/PROJ-1/comment"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("startAt"), "0"; got != want {
			t.Errorf("startAt = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":2,"comments":[
			{"id":"1","author":{"displayName":"Alice"},"created":"2026-01-01T00:00:00.000+0000","body":{"type":"doc"}},
			{"id":"2","author":{"displayName":"Bob"},"created":"2026-01-02T00:00:00.000+0000","body":{"type":"doc"}}
		]}`))
	}))
	defer srv.Close()

	comments, err := NewClient(srv.URL, "Bearer token").ListComments(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if len(comments) != 2 || comments[0].ID != "1" || comments[1].ID != "2" {
		t.Fatalf("comments = %+v, want [1, 2] in order", comments)
	}
	if comments[0].Author.DisplayName != "Alice" || comments[1].Author.DisplayName != "Bob" {
		t.Errorf("authors = %+v", comments)
	}
}

func TestClient_ListComments_MultiPage_Pagination(t *testing.T) {
	var startAts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startAts = append(startAts, r.URL.Query().Get("startAt"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("startAt") {
		case "0":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":2,"total":3,"comments":[
				{"id":"1","author":{"displayName":"Alice"},"created":"t1","body":{"type":"doc"}},
				{"id":"2","author":{"displayName":"Bob"},"created":"t2","body":{"type":"doc"}}
			]}`))
		case "2":
			_, _ = w.Write([]byte(`{"startAt":2,"maxResults":2,"total":3,"comments":[
				{"id":"3","author":{"displayName":"Carol"},"created":"t3","body":{"type":"doc"}}
			]}`))
		default:
			t.Fatalf("unexpected startAt %q", r.URL.Query().Get("startAt"))
		}
	}))
	defer srv.Close()

	comments, err := NewClient(srv.URL, "Bearer token").ListComments(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if got, want := startAts, []string{"0", "2"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("startAts = %v, want %v", got, want)
	}
	if len(comments) != 3 || comments[0].ID != "1" || comments[1].ID != "2" || comments[2].ID != "3" {
		t.Fatalf("comments = %+v, want [1, 2, 3] in order", comments)
	}
}

func TestClient_ListComments_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":0,"comments":[]}`))
	}))
	defer srv.Close()

	comments, err := NewClient(srv.URL, "Bearer token").ListComments(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if comments == nil || len(comments) != 0 {
		t.Errorf("comments = %#v, want non-nil empty slice", comments)
	}
}

func TestClient_ListComments_PageCeiling(t *testing.T) {
	const pageCeiling = 3
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")

		_, _ = fmt.Fprintf(w, `{"startAt":%d,"maxResults":1,"total":1000,"comments":[{"id":"%d","author":{"displayName":"A"},"created":"t","body":{"type":"doc"}}]}`, requests-1, requests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer token")
	_, err := c.listComments(context.Background(), "PROJ-1", pageCeiling)
	if err == nil {
		t.Fatal("listComments: expected error for exceeding the page ceiling, got nil")
	}
	if requests != pageCeiling {
		t.Fatalf("requests = %d, want exactly pageCeiling (%d)", requests, pageCeiling)
	}
}

func TestClient_ListComments_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorMessages":["untrusted"]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "Bearer token").ListComments(context.Background(), "PROJ-404")
	if !IsNotFound(err) {
		t.Errorf("ListComments error = %v, want a 404 StatusError", err)
	}
}
