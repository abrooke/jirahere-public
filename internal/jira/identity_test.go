package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer the-token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer the-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Aslan Brooke","email":"aslan@example.com"}`))
	}))
	defer srv.Close()

	orig := identityURL
	identityURL = srv.URL
	defer func() { identityURL = orig }()

	me, err := Me(context.Background(), "the-token")
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.Name != "Aslan Brooke" || me.Email != "aslan@example.com" {
		t.Errorf("Me returned %+v, unexpected fields", me)
	}
}

func TestMe_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	orig := identityURL
	identityURL = srv.URL
	defer func() { identityURL = orig }()

	_, err := Me(context.Background(), "the-token")
	if err == nil {
		t.Fatal("Me: expected error, got nil")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if statusErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", statusErr.StatusCode, http.StatusForbidden)
	}
}
