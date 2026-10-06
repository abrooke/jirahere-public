package jira

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_GetIssue_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PROJ-123" {
			t.Errorf("path = %q, want /rest/api/3/issue/PROJ-123", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "10001",
			"key": "PROJ-123",
			"fields": {
				"summary": "Widget rollout Q3'26",
				"description": {"type": "doc", "version": 1, "content": []},
				"labels": ["FY26-Q3", "team-widgets"],
				"issuetype": {"id": "5", "name": "Epic"},
				"project": {"id": "10000", "key": "PROJ"}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	issue, err := c.GetIssue(context.Background(), "PROJ-123")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Key != "PROJ-123" {
		t.Errorf("Key = %q, want PROJ-123", issue.Key)
	}
	if issue.Fields.Summary != "Widget rollout Q3'26" {
		t.Errorf("Summary = %q, unexpected", issue.Fields.Summary)
	}
	if len(issue.Fields.Labels) != 2 || issue.Fields.Labels[0] != "FY26-Q3" {
		t.Errorf("Labels = %v, unexpected", issue.Fields.Labels)
	}
	if issue.Fields.IssueType.Name != "Epic" || issue.Fields.IssueType.ID != "5" {
		t.Errorf("IssueType = %+v, unexpected", issue.Fields.IssueType)
	}
	if issue.Fields.Project.ID != "10000" {
		t.Errorf("Project = %+v, unexpected", issue.Fields.Project)
	}
	if len(issue.Fields.Description) == 0 {
		t.Errorf("Description was not preserved as raw JSON")
	}
}

func TestClient_GetIssueRelation_RequestsOnlyRelationFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("fields"); got != relationFieldsParam {
			t.Errorf("fields = %q, want relation-only %q", got, relationFieldsParam)
		}
		_, _ = w.Write([]byte(`{"key":"PROJ-20","fields":{"summary":"Parent","issuetype":{"name":"Story"},"status":{"name":"To Do"},"parent":{"key":"PROJ-1"}}}`))
	}))
	defer srv.Close()

	issue, err := NewClient(srv.URL, "").GetIssueRelation(context.Background(), "PROJ-20")
	if err != nil {
		t.Fatalf("GetIssueRelation: %v", err)
	}
	if issue.Key != "PROJ-20" || issue.Fields.Parent == nil || issue.Fields.Parent.Key != "PROJ-1" {
		t.Errorf("issue = %#v, want decoded shallow relation and parent", issue)
	}
}

func TestClient_GetIssue_ParentPopulatedFromFieldsParent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("fields"); !strings.Contains(got, "parent") {
			t.Errorf("fields query = %q, want it to request parent", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "10001",
			"key": "PROJ-123",
			"fields": {
				"summary": "A child story",
				"issuetype": {"id": "5", "name": "Story"},
				"project": {"id": "10000", "key": "PROJ"},
				"status": {"id": "3", "name": "In Progress"},
				"parent": {
					"id": "10000",
					"key": "PROJ-1",
					"fields": {
						"summary": "The parent epic",
						"status": {"id": "1", "name": "To Do"},
						"issuetype": {"id": "6", "name": "Epic"}
					}
				}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	issue, err := c.GetIssue(context.Background(), "PROJ-123")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Fields.Parent == nil {
		t.Fatal("Parent = nil, want populated ParentRef")
	}
	p := issue.Fields.Parent
	if p.Key != "PROJ-1" || p.Fields.Summary != "The parent epic" {
		t.Errorf("Parent = %+v, unexpected", p)
	}
	if p.Fields.Status.Name != "To Do" || p.Fields.IssueType.Name != "Epic" {
		t.Errorf("Parent.Fields = %+v, unexpected", p.Fields)
	}
}

func TestClient_GetIssue_NoParentLeavesParentNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "10001",
			"key": "PROJ-1",
			"fields": {
				"summary": "A top-level epic",
				"issuetype": {"id": "6", "name": "Epic"},
				"project": {"id": "10000", "key": "PROJ"},
				"status": {"id": "1", "name": "To Do"}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	issue, err := c.GetIssue(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Fields.Parent != nil {
		t.Errorf("Parent = %+v, want nil for an item Jira returned no fields.parent for", issue.Fields.Parent)
	}
}

func TestClient_GetIssue_ParentWithMissingSubFieldsDegradesGracefully(t *testing.T) {
	tests := []struct {
		name        string
		parentJSON  string
		wantKey     string
		wantSummary string
		wantStatus  string
		wantType    string
	}{
		{
			name:       "parent has key only, no fields object",
			parentJSON: `{"id": "10000", "key": "PROJ-1"}`,
			wantKey:    "PROJ-1",
		},
		{
			name:       "parent fields object present but empty",
			parentJSON: `{"key": "PROJ-1", "fields": {}}`,
			wantKey:    "PROJ-1",
		},
		{
			name:        "parent fields has summary but no status or issuetype",
			parentJSON:  `{"key": "PROJ-1", "fields": {"summary": "Just a summary"}}`,
			wantKey:     "PROJ-1",
			wantSummary: "Just a summary",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123","fields":{"summary":"child","parent":` + tt.parentJSON + `}}`))
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "Bearer tok")
			issue, err := c.GetIssue(context.Background(), "PROJ-123")
			if err != nil {
				t.Fatalf("GetIssue: %v", err)
			}
			if issue.Fields.Parent == nil {
				t.Fatal("Parent = nil, want non-nil (fields.parent was present)")
			}
			p := issue.Fields.Parent
			if p.Key != tt.wantKey || p.Fields.Summary != tt.wantSummary ||
				p.Fields.Status.Name != tt.wantStatus || p.Fields.IssueType.Name != tt.wantType {
				t.Errorf("Parent = %+v, want key=%q summary=%q status=%q type=%q",
					p, tt.wantKey, tt.wantSummary, tt.wantStatus, tt.wantType)
			}
		})
	}
}

func TestClient_GetIssue_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.GetIssue(context.Background(), "PROJ-999")
	if err == nil {
		t.Fatal("GetIssue: expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(err) = false, want true (err = %v)", err)
	}
}

func TestClient_GetIssue_OtherError_NotNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.GetIssue(context.Background(), "PROJ-123")
	if err == nil {
		t.Fatal("GetIssue: expected error, got nil")
	}
	if IsNotFound(err) {
		t.Errorf("IsNotFound(err) = true, want false for a 500 (err = %v)", err)
	}
}

func TestClient_GetIssue_ResponseSizeLimit(t *testing.T) {
	validIssue := []byte(`{"id":"1","key":"PROJ-123","fields":{"summary":"x"}}`)
	boundaryBody := append(validIssue, []byte(strings.Repeat(" ", int(maxIssueResponseBytes)-len(validIssue)))...)
	if int64(len(boundaryBody)) != maxIssueResponseBytes {
		t.Fatalf("boundary body length = %d, want %d", len(boundaryBody), maxIssueResponseBytes)
	}

	t.Run("accepts exact limit", func(t *testing.T) {
		body := &trackingReadCloser{Reader: strings.NewReader(string(boundaryBody))}
		client := NewClient("https://jira.example.test", "Bearer tok")
		client.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		})}

		issue, err := client.GetIssue(context.Background(), "PROJ-123")
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		if issue.Key != "PROJ-123" {
			t.Errorf("Key = %q, want PROJ-123", issue.Key)
		}
		if !body.closed {
			t.Error("response body was not closed")
		}
	})

	t.Run("rejects overflow without response body", func(t *testing.T) {
		const secret = "response-body-secret-must-not-leak"
		body := &trackingReadCloser{Reader: strings.NewReader(strings.Repeat("x", int(maxIssueResponseBytes+1)) + secret)}
		client := NewClient("https://jira.example.test", "Bearer tok")
		client.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: body, Header: make(http.Header)}, nil
		})}

		_, err := client.GetIssue(context.Background(), "PROJ-123")
		var tooLarge *ResponseTooLargeError
		if !errors.As(err, &tooLarge) {
			t.Fatalf("err = %v, want *ResponseTooLargeError", err)
		}
		if tooLarge.Limit != maxIssueResponseBytes {
			t.Errorf("limit = %d, want %d", tooLarge.Limit, maxIssueResponseBytes)
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaked response body: %v", err)
		}
		if !body.closed {
			t.Error("response body was not closed")
		}
	})
}

func TestClient_CreateIssue_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue" {
			t.Errorf("method/path = %s %s, want POST /rest/api/3/issue", r.Method, r.URL.Path)
		}
		var body struct {
			Fields struct {
				Project   map[string]string `json:"project"`
				IssueType map[string]string `json:"issuetype"`
				Summary   string            `json:"summary"`
				Labels    []string          `json:"labels"`
				Parent    map[string]string `json:"parent"`
			} `json:"fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode create request: %v", err)
		}
		if body.Fields.Project["id"] != "10000" {
			t.Errorf("project id = %q, want 10000", body.Fields.Project["id"])
		}
		if body.Fields.IssueType["id"] != "5" {
			t.Errorf("issuetype id = %q, want 5", body.Fields.IssueType["id"])
		}
		if body.Fields.Summary != "Widget rollout Q4'26" {
			t.Errorf("summary = %q, unexpected", body.Fields.Summary)
		}
		if body.Fields.Parent != nil {
			t.Errorf("fields.parent = %v, want no parent key when ParentKey is unset", body.Fields.Parent)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"10002","key":"PROJ-456"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.CreateIssue(context.Background(), CreateIssueFields{
		ProjectID:   "10000",
		IssueTypeID: "5",
		Summary:     "Widget rollout Q4'26",
		Labels:      []string{"FY26-Q4", "team-widgets"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if result.Key != "PROJ-456" {
		t.Errorf("Key = %q, want PROJ-456", result.Key)
	}
}

func TestClient_CreateIssue_WithParent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Fields struct {
				Parent map[string]string `json:"parent"`
			} `json:"fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode create request: %v", err)
		}
		if body.Fields.Parent["key"] != "PROJ-1" {
			t.Errorf("fields.parent.key = %q, want PROJ-1", body.Fields.Parent["key"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"10003","key":"PROJ-457"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.CreateIssue(context.Background(), CreateIssueFields{
		ProjectID:   "10000",
		IssueTypeID: "7",
		Summary:     "Subtask work",
		ParentKey:   "PROJ-1",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if result.Key != "PROJ-457" {
		t.Errorf("Key = %q, want PROJ-457", result.Key)
	}
}

func TestClient_CreateIssue_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["invalid project"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.CreateIssue(context.Background(), CreateIssueFields{ProjectID: "bad", IssueTypeID: "5", Summary: "x"})
	if err == nil {
		t.Fatal("CreateIssue: expected error, got nil")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
}

func TestIssueFields_Created_DecodesJiraLayout(t *testing.T) {
	var neg, pos Issue
	if err := json.Unmarshal([]byte(`{"key":"P-1","fields":{"summary":"s","created":"2026-01-02T15:04:05.000-0700"}}`), &neg); err != nil {
		t.Fatalf("Unmarshal neg: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"key":"P-2","fields":{"created":"2026-03-04T09:10:11.000+0530"}}`), &pos); err != nil {
		t.Fatalf("Unmarshal pos: %v", err)
	}
	wantNeg := time.Date(2026, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*3600))
	if !neg.Fields.Created.Equal(wantNeg) {
		t.Errorf("neg Created = %s, want %s", neg.Fields.Created, wantNeg)
	}
	if neg.Fields.Summary != "s" {
		t.Errorf("Summary = %q, other fields must still decode", neg.Fields.Summary)
	}
	wantPos := time.Date(2026, 3, 4, 9, 10, 11, 0, time.FixedZone("", 5*3600+30*60))
	if !pos.Fields.Created.Equal(wantPos) {
		t.Errorf("pos Created = %s, want %s", pos.Fields.Created, wantPos)
	}

	want, _ := time.Parse(jiraDateTimeLayout, "2026-01-02T15:04:05.000-0700")
	if !neg.Fields.Created.Equal(want) {
		t.Errorf("Created differs from jiraDateTimeLayout parse")
	}
}

func TestIssueFields_Created_AbsentOrEmptyIsZero(t *testing.T) {
	for name, body := range map[string]string{
		"absent": `{"fields":{"summary":"s"}}`,
		"null":   `{"fields":{"created":null}}`,
		"empty":  `{"fields":{"created":""}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var issue Issue
			if err := json.Unmarshal([]byte(body), &issue); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !issue.Fields.Created.IsZero() {
				t.Errorf("Created = %s, want zero", issue.Fields.Created)
			}
		})
	}
}

func TestIssueFields_Created_MalformedFails(t *testing.T) {
	var issue Issue
	err := json.Unmarshal([]byte(`{"fields":{"created":"2026-01-02"}}`), &issue)
	if err == nil || !strings.Contains(err.Error(), "created") {
		t.Errorf("err = %v, want created parse error", err)
	}
}

func TestClient_GetIssue_RequestsCreated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(","+r.URL.Query().Get("fields")+",", ",created,") {
			t.Errorf("fields = %q, want it to include created", r.URL.Query().Get("fields"))
		}
		_, _ = w.Write([]byte(`{"key":"P-1","fields":{"created":"2026-01-02T15:04:05.000-0700"}}`))
	}))
	defer srv.Close()
	issue, err := NewClient(srv.URL, "Bearer tok").GetIssue(context.Background(), "P-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Fields.Created.IsZero() {
		t.Errorf("Created not populated")
	}
}

func TestIssueFields_Resolved_DecodesJiraLayout(t *testing.T) {
	var issue Issue
	if err := json.Unmarshal([]byte(`{"key":"P-1","fields":{"summary":"s","resolutiondate":"2026-01-02T15:04:05.000-0700"}}`), &issue); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := time.Date(2026, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*3600))
	if issue.Fields.Resolved == nil || !issue.Fields.Resolved.Equal(want) {
		t.Errorf("Resolved = %v, want %s", issue.Fields.Resolved, want)
	}
	if issue.Fields.Summary != "s" {
		t.Errorf("Summary = %q, other fields must still decode", issue.Fields.Summary)
	}
}

func TestIssueFields_Resolved_AbsentOrEmptyIsNil(t *testing.T) {
	for name, body := range map[string]string{
		"absent": `{"fields":{"summary":"s"}}`,
		"null":   `{"fields":{"resolutiondate":null}}`,
		"empty":  `{"fields":{"resolutiondate":""}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var issue Issue
			if err := json.Unmarshal([]byte(body), &issue); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if issue.Fields.Resolved != nil {
				t.Errorf("Resolved = %s, want nil (unresolved)", issue.Fields.Resolved)
			}
		})
	}
}

func TestIssueFields_Resolved_MalformedFails(t *testing.T) {
	var issue Issue
	err := json.Unmarshal([]byte(`{"fields":{"resolutiondate":"2026-01-02"}}`), &issue)
	if err == nil || !strings.Contains(err.Error(), "resolutiondate") {
		t.Errorf("err = %v, want resolutiondate parse error", err)
	}
}

func TestClient_GetIssue_RequestsResolutionDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(","+r.URL.Query().Get("fields")+",", ",resolutiondate,") {
			t.Errorf("fields = %q, want it to include resolutiondate", r.URL.Query().Get("fields"))
		}
		_, _ = w.Write([]byte(`{"key":"P-1","fields":{"resolutiondate":"2026-01-02T15:04:05.000-0700"}}`))
	}))
	defer srv.Close()
	issue, err := NewClient(srv.URL, "Bearer tok").GetIssue(context.Background(), "P-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Fields.Resolved == nil {
		t.Errorf("Resolved not populated")
	}
}

func TestIssueFields_Assignee_DecodesAssignedNullAndOmitted(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "assigned",
			body: `{"fields":{"summary":"s","assignee":{"displayName":"Jamie Rivera"}}}`,
			want: "Jamie Rivera",
		},
		{
			name: "null",
			body: `{"fields":{"summary":"s","assignee":null}}`,
			want: "",
		},
		{
			name: "omitted",
			body: `{"fields":{"summary":"s"}}`,
			want: "",
		},
		{
			name: "present but no displayName",
			body: `{"fields":{"summary":"s","assignee":{"accountId":"abc123"}}}`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var issue Issue
			if err := json.Unmarshal([]byte(tt.body), &issue); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if issue.Fields.Assignee != tt.want {
				t.Errorf("Assignee = %q, want %q", issue.Fields.Assignee, tt.want)
			}
			if issue.Fields.Summary != "s" {
				t.Errorf("Summary = %q, other fields must still decode", issue.Fields.Summary)
			}
		})
	}
}

func TestClient_GetIssue_RequestsAndPopulatesAssignee(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(","+r.URL.Query().Get("fields")+",", ",assignee,") {
			t.Errorf("fields = %q, want it to include assignee", r.URL.Query().Get("fields"))
		}
		_, _ = w.Write([]byte(`{"key":"P-1","fields":{"assignee":{"displayName":"Jamie Rivera"}}}`))
	}))
	defer srv.Close()
	issue, err := NewClient(srv.URL, "Bearer tok").GetIssue(context.Background(), "P-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Fields.Assignee != "Jamie Rivera" {
		t.Errorf("Assignee = %q, want %q", issue.Fields.Assignee, "Jamie Rivera")
	}
}

func TestClient_GetIssueRelation_RequestsAndPopulatesAssignee(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(","+r.URL.Query().Get("fields")+",", ",assignee,") {
			t.Errorf("fields = %q, want it to include assignee", r.URL.Query().Get("fields"))
		}
		_, _ = w.Write([]byte(`{"key":"PROJ-20","fields":{"summary":"Parent","assignee":{"displayName":"Ana Souza"}}}`))
	}))
	defer srv.Close()

	issue, err := NewClient(srv.URL, "").GetIssueRelation(context.Background(), "PROJ-20")
	if err != nil {
		t.Fatalf("GetIssueRelation: %v", err)
	}
	if issue.Fields.Assignee != "Ana Souza" {
		t.Errorf("Assignee = %q, want %q", issue.Fields.Assignee, "Ana Souza")
	}
}

func TestParentRefFields_Assignee_DecodesNestedAssignedNullAndOmitted(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "assigned",
			body: `{"fields":{"parent":{"key":"PROJ-1","fields":{"summary":"Parent","assignee":{"displayName":"Jamie Rivera"}}}}}`,
			want: "Jamie Rivera",
		},
		{
			name: "null",
			body: `{"fields":{"parent":{"key":"PROJ-1","fields":{"summary":"Parent","assignee":null}}}}`,
			want: "",
		},
		{
			name: "omitted",
			body: `{"fields":{"parent":{"key":"PROJ-1","fields":{"summary":"Parent"}}}}`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var issue Issue
			if err := json.Unmarshal([]byte(tt.body), &issue); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if issue.Fields.Parent == nil {
				t.Fatal("Parent = nil, want populated ParentRef")
			}
			if got := issue.Fields.Parent.Fields.Assignee; got != tt.want {
				t.Errorf("Parent.Fields.Assignee = %q, want %q", got, tt.want)
			}
		})
	}
}
