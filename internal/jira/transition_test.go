package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Transitions_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/issue/PROJ-1/transitions" {
			t.Errorf("method/path = %s %s, want GET /rest/api/3/issue/PROJ-1/transitions", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"transitions":[
			{"id":"11","to":{"id":"3","name":"In Progress"}},
			{"id":"21","to":{"id":"4","name":"Done"}}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	transitions, err := c.Transitions(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	want := []Transition{
		{ID: "11", ToStatusName: "In Progress"},
		{ID: "21", ToStatusName: "Done"},
	}
	if len(transitions) != len(want) || transitions[0] != want[0] || transitions[1] != want[1] {
		t.Errorf("transitions = %+v, want %+v", transitions, want)
	}
}

func TestClient_Transitions_EscapesIssueKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.EscapedPath(), "/rest/api/3/issue/PROJ%2F1/transitions"; got != want {
			t.Errorf("escaped path = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"transitions":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if _, err := c.Transitions(context.Background(), "PROJ/1"); err != nil {
		t.Fatalf("Transitions: %v", err)
	}
}

func TestClient_Transitions_EmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"transitions":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	transitions, err := c.Transitions(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if len(transitions) != 0 {
		t.Errorf("transitions = %+v, want empty", transitions)
	}
}

func TestClient_Transitions_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.Transitions(context.Background(), "PROJ-404")
	if err == nil {
		t.Fatal("Transitions: expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(err) = false, want true for 404")
	}
}

func TestClient_DoTransition_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue/PROJ-1/transitions" {
			t.Errorf("method/path = %s %s, want POST /rest/api/3/issue/PROJ-1/transitions", r.Method, r.URL.Path)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer tok"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		var body struct {
			Transition struct {
				ID string `json:"id"`
			} `json:"transition"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode transition request: %v", err)
		}
		if body.Transition.ID != "21" {
			t.Errorf("transition.id = %q, want 21", body.Transition.ID)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.DoTransition(context.Background(), "PROJ-1", "21"); err != nil {
		t.Fatalf("DoTransition: %v", err)
	}
}

func TestClient_DoTransition_EscapesIssueKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.EscapedPath(), "/rest/api/3/issue/PROJ%2F1/transitions"; got != want {
			t.Errorf("escaped path = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.DoTransition(context.Background(), "PROJ/1", "21"); err != nil {
		t.Fatalf("DoTransition: %v", err)
	}
}

func TestClient_DoTransition_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["transition validation failed"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	err := c.DoTransition(context.Background(), "PROJ-1", "21")
	if err == nil {
		t.Fatal("DoTransition: expected error, got nil")
	}
}
