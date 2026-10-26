package jira

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClient_Myself_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			t.Errorf("path = %q, want /rest/api/3/myself", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Basic creds" {
			t.Errorf("Authorization header = %q, want %q", got, "Basic creds")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accountId":"1","displayName":"Aslan Brooke","emailAddress":"aslan@example.com"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Basic creds")
	me, err := c.Myself(context.Background())
	if err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if me.DisplayName != "Aslan Brooke" || me.EmailAddress != "aslan@example.com" {
		t.Errorf("Myself returned %+v, unexpected fields", me)
	}
}

func TestClient_DebugTraceExcludesSecretsAndKeepsSafeDiagnostics(t *testing.T) {
	const authorization = "Basic c2VjcmV0LWVtYWlsOnNlY3JldC10b2tlbg=="
	const requestSecret = "request-token-secret"
	const headerSecret = "response-token-secret"
	const bodySecret = "response-body-secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Token", headerSecret)
		_, _ = w.Write([]byte(`{"message":"` + bodySecret + `"}`))
	}))
	defer srv.Close()

	var debug bytes.Buffer
	c := NewClient(srv.URL, authorization)
	c.Debug = &debug
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/diagnostic?access_token="+requestSecret, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Request-Token", requestSecret)
	c.debugRequest(req)
	_, _ = c.Myself(context.Background())

	out := debug.String()
	for _, secret := range []string{authorization, requestSecret, headerSecret, bodySecret} {
		if strings.Contains(out, secret) {
			t.Errorf("debug trace exposes secret %q: %q", secret, out)
		}
	}
	for _, want := range []string{
		"[debug] request: GET\n",
		"[debug] response: HTTP 200 (body:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug trace = %q, want safe diagnostic %q", out, want)
		}
	}
}

func TestClient_DebugTraceTransportFailureExcludesErrorText(t *testing.T) {
	const secret = "transport-secret-must-not-leak"
	var debug bytes.Buffer
	c := NewClient("https://example.invalid", "Basic credentials")
	c.Debug = &debug
	c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(secret)
	})}

	_, err := c.Myself(context.Background())
	if err == nil {
		t.Fatal("Myself: expected transport error")
	}
	if strings.Contains(debug.String(), secret) {
		t.Errorf("debug trace exposes transport error text: %q", debug.String())
	}
	if got, want := debug.String(), "[debug] request: GET\n[debug] request failed\n"; got != want {
		t.Errorf("debug trace = %q, want %q", got, want)
	}
}

func TestClient_Myself_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Basic bad-creds")
	_, err := c.Myself(context.Background())
	if err == nil {
		t.Fatal("Myself: expected error, got nil")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if statusErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", statusErr.StatusCode, http.StatusUnauthorized)
	}
}
