package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestClient_IssueLinkTypes_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issueLinkType" {
			t.Errorf("path = %q, want /rest/api/3/issueLinkType", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issueLinkTypes":[
			{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"},
			{"id":"10001","name":"Cloners","inward":"is cloned by","outward":"is a clone of"}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	types, err := c.IssueLinkTypes(context.Background())
	if err != nil {
		t.Fatalf("IssueLinkTypes: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("len(types) = %d, want 2", len(types))
	}
	if types[1].Name != "Cloners" || types[1].Outward != "is a clone of" {
		t.Errorf("types[1] = %+v, unexpected", types[1])
	}
}

func TestClient_CreateIssueLink_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issueLink" {
			t.Errorf("method/path = %s %s, want POST /rest/api/3/issueLink", r.Method, r.URL.Path)
		}
		var body struct {
			Type         map[string]string `json:"type"`
			InwardIssue  map[string]string `json:"inwardIssue"`
			OutwardIssue map[string]string `json:"outwardIssue"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode issue link request: %v", err)
		}
		if body.Type["name"] != "Cloners" {
			t.Errorf("type name = %q, want Cloners", body.Type["name"])
		}
		if body.InwardIssue["key"] != "PROJ-123" {
			t.Errorf("inwardIssue.key = %q, want PROJ-123", body.InwardIssue["key"])
		}
		if body.OutwardIssue["key"] != "PROJ-456" {
			t.Errorf("outwardIssue.key = %q, want PROJ-456", body.OutwardIssue["key"])
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.CreateIssueLink(context.Background(), "Cloners", "PROJ-123", "PROJ-456"); err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
}

func TestClient_CreateIssueLink_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	err := c.CreateIssueLink(context.Background(), "Cloners", "PROJ-123", "PROJ-456")
	if err == nil {
		t.Fatal("CreateIssueLink: expected error, got nil")
	}
}

func TestClient_SetParent_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/PROJ-456" {
			t.Errorf("method/path = %s %s, want PUT /rest/api/3/issue/PROJ-456", r.Method, r.URL.Path)
		}
		var body struct {
			Fields struct {
				Parent map[string]string `json:"parent"`
			} `json:"fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode set-parent request: %v", err)
		}
		if body.Fields.Parent["key"] != "PROJ-001" {
			t.Errorf("parent key = %q, want PROJ-001", body.Fields.Parent["key"])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.SetParent(context.Background(), "PROJ-456", "PROJ-001"); err != nil {
		t.Fatalf("SetParent: %v", err)
	}
}

func TestClient_SetParent_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["issue type cannot be a child of this parent"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	err := c.SetParent(context.Background(), "PROJ-456", "PROJ-001")
	if err == nil {
		t.Fatal("SetParent: expected error, got nil")
	}
}

func TestClient_SetSummary_Success(t *testing.T) {
	const summary = "Ship \"Q4\" \\ rollout\n\u2603"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/PROJ-456" {
			t.Errorf("method/path = %s %s, want PUT /rest/api/3/issue/PROJ-456", r.Method, r.URL.Path)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer tok"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		var body struct {
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode set-summary request: %v", err)
		}
		if body.Fields.Summary != summary {
			t.Errorf("summary = %q, want %q", body.Fields.Summary, summary)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.SetSummary(context.Background(), "PROJ-456", summary); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}
}

func TestClient_SetSummary_EscapesIssueKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.EscapedPath(), "/rest/api/3/issue/PROJ%2F456"; got != want {
			t.Errorf("escaped path = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.SetSummary(context.Background(), "PROJ/456", "Summary"); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}
}

func TestClient_SetSummary_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["summary is required"]}`))
	}))
	defer srv.Close()

	err := NewClient(srv.URL, "Bearer tok").SetSummary(context.Background(), "PROJ-456", "")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if statusErr.StatusCode != http.StatusBadRequest || statusErr.Body != `{"errorMessages":["summary is required"]}` {
		t.Errorf("StatusError = %+v, want status 400 and response body", statusErr)
	}
}

func TestClient_SetSummary_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewClient("https://example.invalid", "Bearer tok")
	c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}

	err := c.SetSummary(ctx, "PROJ-456", "Summary")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestClient_SetSummary_ResponseAtLimit(t *testing.T) {
	c := NewClient("https://example.invalid", "Bearer tok")
	c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", int(maxIssueUpdateResponseBytes)))),
			Header:     make(http.Header),
		}, nil
	})}

	if err := c.SetSummary(context.Background(), "PROJ-456", "Summary"); err != nil {
		t.Fatalf("SetSummary response at limit: %v", err)
	}
}

func TestClient_IssueUpdateResponseOverLimitIsSafeAndClosesBody(t *testing.T) {
	for _, update := range []struct {
		name string
		call func(*Client) error
	}{
		{
			name: "create issue link",
			call: func(c *Client) error {
				return c.CreateIssueLink(context.Background(), "Cloners", "PROJ-1", "PROJ-2")
			},
		},
		{
			name: "set parent",
			call: func(c *Client) error {
				return c.SetParent(context.Background(), "PROJ-1", "PROJ-2")
			},
		},
		{
			name: "set summary",
			call: func(c *Client) error {
				return c.SetSummary(context.Background(), "PROJ-1", "Summary")
			},
		},
		{
			name: "set labels",
			call: func(c *Client) error {
				return c.SetLabels(context.Background(), "PROJ-1", []string{"a"})
			},
		},
	} {
		t.Run(update.name, func(t *testing.T) {
			body := &trackingReadCloser{Reader: strings.NewReader(strings.Repeat("x", int(maxIssueUpdateResponseBytes+1)))}
			c := NewClient("https://example.invalid", "Bearer tok")
			c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
			})}

			err := update.call(c)
			var tooLarge *ResponseTooLargeError
			if !errors.As(err, &tooLarge) {
				t.Fatalf("error = %v, want *ResponseTooLargeError", err)
			}
			if tooLarge.Limit != maxIssueUpdateResponseBytes {
				t.Errorf("limit = %d, want %d", tooLarge.Limit, maxIssueUpdateResponseBytes)
			}
			if strings.Contains(err.Error(), strings.Repeat("x", 16)) {
				t.Errorf("error includes response body: %q", err)
			}
			if !body.closed {
				t.Error("response body was not closed")
			}
		})
	}
}

func TestClient_SetSummary_Non2xxWithinResponseLimit(t *testing.T) {
	const response = `{"errorMessages":["summary is required"]}`
	c := NewClient("https://example.invalid", "Bearer tok")
	c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(response)),
			Header:     make(http.Header),
		}, nil
	})}

	err := c.SetSummary(context.Background(), "PROJ-456", "Summary")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want *StatusError", err)
	}
	if statusErr.Body != response {
		t.Errorf("StatusError.Body = %q, want %q", statusErr.Body, response)
	}
}

func TestClient_SetLabels_Success(t *testing.T) {
	labels := []string{"alpha\"beta\\gamma\ndelta☃", "\x01ctrl"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/PROJ-456" {
			t.Errorf("method/path = %s %s, want PUT /rest/api/3/issue/PROJ-456", r.Method, r.URL.Path)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer tok"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		const want = `{"fields":{"labels":["alpha\"beta\\gamma\ndelta☃","\u0001ctrl"]}}`
		if string(rawBody) != want {
			t.Errorf("request body = %s, want %s", rawBody, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.SetLabels(context.Background(), "PROJ-456", labels); err != nil {
		t.Fatalf("SetLabels: %v", err)
	}
}

func TestClient_SetLabels_EscapesIssueKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.EscapedPath(), "/rest/api/3/issue/PROJ%2F456"; got != want {
			t.Errorf("escaped path = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if err := c.SetLabels(context.Background(), "PROJ/456", []string{"alpha"}); err != nil {
		t.Fatalf("SetLabels: %v", err)
	}
}

func TestClient_SetLabels_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["label is invalid"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	err := c.SetLabels(context.Background(), "PROJ-456", []string{"alpha"})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if statusErr.StatusCode != http.StatusBadRequest || statusErr.Body != `{"errorMessages":["label is invalid"]}` {
		t.Errorf("StatusError = %+v, want status 400 and response body", statusErr)
	}
}

func TestClient_SetLabels_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewClient("https://example.invalid", "Bearer tok")
	c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}

	err := c.SetLabels(ctx, "PROJ-456", []string{"alpha"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestClient_SetLabels_ResponseAtLimit(t *testing.T) {
	c := NewClient("https://example.invalid", "Bearer tok")
	c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", int(maxIssueUpdateResponseBytes)))),
			Header:     make(http.Header),
		}, nil
	})}

	if err := c.SetLabels(context.Background(), "PROJ-456", []string{"alpha"}); err != nil {
		t.Fatalf("SetLabels response at limit: %v", err)
	}
}

func TestClient_SetLabels_Non2xxWithinResponseLimit(t *testing.T) {
	const response = `{"errorMessages":["label is invalid"]}`
	c := NewClient("https://example.invalid", "Bearer tok")
	c.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(response)),
			Header:     make(http.Header),
		}, nil
	})}

	err := c.SetLabels(context.Background(), "PROJ-456", []string{"alpha"})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want *StatusError", err)
	}
	if statusErr.Body != response {
		t.Errorf("StatusError.Body = %q, want %q", statusErr.Body, response)
	}
}

func TestClient_SetLabels_NilAndEmptySerializeAsEmptyArray(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
	}{
		{name: "nil", labels: nil},
		{name: "empty", labels: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rawBody, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				const want = `{"fields":{"labels":[]}}`
				if string(rawBody) != want {
					t.Errorf("request body = %s, want %s", rawBody, want)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "Bearer tok")
			if err := c.SetLabels(context.Background(), "PROJ-456", tc.labels); err != nil {
				t.Fatalf("SetLabels: %v", err)
			}
		})
	}
}
