package jira

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_SearchQuarterIssues_EmitsExactJQLAndFields(t *testing.T) {
	tests := []struct {
		name         string
		projectKey   string
		quarterLabel string
		wantJQL      string
	}{
		{
			name:         "plain operands",
			projectKey:   "PROJ",
			quarterLabel: "FY26-Q1",
			wantJQL:      `project = "PROJ" AND labels = "FY26-Q1"`,
		},
		{
			name:         "both operands escaped",
			projectKey:   `PROJ" OR 1=1--`,
			quarterLabel: `FY26-Q1\"`,
			wantJQL:      `project = "PROJ\" OR 1=1--" AND labels = "FY26-Q1\\\""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotJQL, gotFields, gotPath, gotMaxResults string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotJQL = r.URL.Query().Get("jql")
				gotFields = r.URL.Query().Get("fields")
				gotMaxResults = r.URL.Query().Get("maxResults")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"issues":[]}`))
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "Bearer tok")
			issues, err := c.SearchQuarterIssues(context.Background(), tt.projectKey, tt.quarterLabel)
			if err != nil {
				t.Fatalf("SearchQuarterIssues: %v", err)
			}
			if issues == nil {
				t.Error("issues = nil, want non-nil empty slice")
			}
			if len(issues) != 0 {
				t.Errorf("len(issues) = %d, want 0", len(issues))
			}
			if gotPath != "/rest/api/3/search/jql" {
				t.Errorf("path = %q, want /rest/api/3/search/jql", gotPath)
			}
			if gotJQL != tt.wantJQL {
				t.Errorf("jql = %q, want %q", gotJQL, tt.wantJQL)
			}
			if gotFields != "summary,created,updated,status" {
				t.Errorf("fields = %q, want summary,created,updated,status", gotFields)
			}
			wantMaxResults := fmt.Sprintf("%d", jqlChildrenPageSize)
			if gotMaxResults != wantMaxResults {
				t.Errorf("maxResults = %q, want %q (jqlChildrenPageSize, mirrored from ChildrenOf)", gotMaxResults, wantMaxResults)
			}
		})
	}
}

func TestClient_SearchQuarterIssues_MissingTimestampFields_IsErrorAndKeepsEarlierPages(t *testing.T) {
	tests := []struct {
		name     string
		badIssue string
		wantName string
	}{
		{
			name:     "fields object absent entirely",
			badIssue: `{"key":"PROJ-2"}`,
			wantName: "created timestamp",
		},
		{
			name:     "fields present but created and updated absent",
			badIssue: `{"key":"PROJ-2","fields":{"summary":"No timestamps"}}`,
			wantName: "created timestamp",
		},
		{
			name:     "created present, updated absent",
			badIssue: `{"key":"PROJ-2","fields":{"summary":"Half","created":"2026-01-02T15:04:05.000-0700"}}`,
			wantName: "updated timestamp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Query().Get("nextPageToken") {
				case "":
					_, _ = w.Write([]byte(`{"issues":[
						{"key":"PROJ-1","fields":{"summary":"Good","created":"2026-01-01T00:00:00.000+0000","updated":"2026-01-01T00:00:00.000+0000"}}
					],"nextPageToken":"page-2"}`))
				case "page-2":
					_, _ = fmt.Fprintf(w, `{"issues":[%s]}`, tt.badIssue)
				default:
					t.Errorf("unexpected nextPageToken %q", r.URL.Query().Get("nextPageToken"))
				}
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "Bearer tok")
			issues, err := c.SearchQuarterIssues(context.Background(), "PROJ", "FY26-Q1")
			if err == nil {
				t.Fatal("SearchQuarterIssues: expected an error for a missing created/updated timestamp, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantName) {
				t.Errorf("err = %v, want it to name the %s", err, tt.wantName)
			}

			if len(issues) != 1 || issues[0].Key != "PROJ-1" {
				t.Errorf("issues = %+v, want the single PROJ-1 accumulated before the bad page", issues)
			}
		})
	}
}

func TestClient_SearchQuarterIssues_ParsesDatetimeOffsets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"PROJ-1","fields":{"summary":"Neg offset","status":{"id":"10001","name":"In Progress"},"created":"2026-01-02T15:04:05.000-0700","updated":"2026-01-02T18:30:00.250-0700"}},
			{"key":"PROJ-2","fields":{"summary":"Pos offset","status":{"id":"10002","name":"Done"},"created":"2026-03-04T09:10:11.000+0530","updated":"2026-03-04T22:00:00.500+0530"}}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	issues, err := c.SearchQuarterIssues(context.Background(), "PROJ", "FY26-Q1")
	if err != nil {
		t.Fatalf("SearchQuarterIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d, want 2", len(issues))
	}

	if issues[0].Key != "PROJ-1" || issues[1].Key != "PROJ-2" {
		t.Fatalf("order = [%q %q], want [PROJ-1 PROJ-2]", issues[0].Key, issues[1].Key)
	}
	if issues[0].Summary != "Neg offset" {
		t.Errorf("issues[0].Summary = %q, want %q", issues[0].Summary, "Neg offset")
	}
	if issues[0].Status != (Status{ID: "10001", Name: "In Progress"}) {
		t.Errorf("issues[0].Status = %+v, want {ID:10001 Name:In Progress}", issues[0].Status)
	}
	if issues[1].Status != (Status{ID: "10002", Name: "Done"}) {
		t.Errorf("issues[1].Status = %+v, want {ID:10002 Name:Done}", issues[1].Status)
	}

	wantNegCreated := time.Date(2026, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*3600))
	if !issues[0].Created.Equal(wantNegCreated) {
		t.Errorf("issues[0].Created = %s, want %s", issues[0].Created, wantNegCreated)
	}
	wantNegUpdated := time.Date(2026, 1, 2, 18, 30, 0, 250*int(time.Millisecond), time.FixedZone("", -7*3600))
	if !issues[0].Updated.Equal(wantNegUpdated) {
		t.Errorf("issues[0].Updated = %s, want %s", issues[0].Updated, wantNegUpdated)
	}

	wantPosCreated := time.Date(2026, 3, 4, 9, 10, 11, 0, time.FixedZone("", 5*3600+30*60))
	if !issues[1].Created.Equal(wantPosCreated) {
		t.Errorf("issues[1].Created = %s, want %s", issues[1].Created, wantPosCreated)
	}
	wantPosUpdated := time.Date(2026, 3, 4, 22, 0, 0, 500*int(time.Millisecond), time.FixedZone("", 5*3600+30*60))
	if !issues[1].Updated.Equal(wantPosUpdated) {
		t.Errorf("issues[1].Updated = %s, want %s", issues[1].Updated, wantPosUpdated)
	}
}

func TestClient_SearchQuarterIssues_UnparseableDatetime_IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"PROJ-1","fields":{"summary":"Bad","created":"2026-01-02 15:04:05","updated":"2026-01-02T15:04:05.000-0700"}}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.SearchQuarterIssues(context.Background(), "PROJ", "FY26-Q1")
	if err == nil {
		t.Fatal("SearchQuarterIssues: expected an error for an unparseable datetime, got nil")
	}
	if !strings.Contains(err.Error(), "created timestamp") {
		t.Errorf("err = %v, want it to name the created timestamp", err)
	}
}

func TestClient_SearchQuarterIssues_MultiPageWalk(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("nextPageToken") {
		case "":
			_, _ = w.Write([]byte(`{"issues":[
				{"key":"PROJ-1","fields":{"summary":"A","created":"2026-01-01T00:00:00.000+0000","updated":"2026-01-01T00:00:00.000+0000"}}
			],"nextPageToken":"page-2"}`))
		case "page-2":
			_, _ = w.Write([]byte(`{"issues":[
				{"key":"PROJ-2","fields":{"summary":"B","created":"2026-01-02T00:00:00.000+0000","updated":"2026-01-02T00:00:00.000+0000"}}
			],"nextPageToken":"page-3"}`))
		case "page-3":
			_, _ = w.Write([]byte(`{"issues":[
				{"key":"PROJ-3","fields":{"summary":"C","created":"2026-01-03T00:00:00.000+0000","updated":"2026-01-03T00:00:00.000+0000"}}
			]}`))
		default:
			t.Errorf("unexpected nextPageToken %q", r.URL.Query().Get("nextPageToken"))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	issues, err := c.SearchQuarterIssues(context.Background(), "PROJ", "FY26-Q1")
	if err != nil {
		t.Fatalf("SearchQuarterIssues: %v", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if len(issues) != 3 || issues[0].Key != "PROJ-1" || issues[1].Key != "PROJ-2" || issues[2].Key != "PROJ-3" {
		t.Errorf("issues = %+v, unexpected", issues)
	}
}

func TestClient_SearchQuarterIssues_RepeatedToken_Terminates(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("nextPageToken") == "" {
			_, _ = w.Write([]byte(`{"issues":[
				{"key":"PROJ-1","fields":{"summary":"A","created":"2026-01-01T00:00:00.000+0000","updated":"2026-01-01T00:00:00.000+0000"}}
			],"nextPageToken":"stuck"}`))
			return
		}
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"PROJ-2","fields":{"summary":"B","created":"2026-01-02T00:00:00.000+0000","updated":"2026-01-02T00:00:00.000+0000"}}
		],"nextPageToken":"stuck"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	issues, err := c.SearchQuarterIssues(context.Background(), "PROJ", "FY26-Q1")
	if err == nil {
		t.Fatal("SearchQuarterIssues: expected an error for a repeated nextPageToken, got nil")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want exactly 2 (stop as soon as the repeat is detected)", requests)
	}
	if len(issues) != 2 {
		t.Errorf("issues = %+v, want the 2 accumulated before the repeat was detected", issues)
	}
}

func TestClient_SearchQuarterIssues_EverChangingToken_HitsPageCeiling(t *testing.T) {
	const pageCeiling = 3

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"issues":[],"nextPageToken":"tok-%d"}`, requests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.searchQuarterIssues(context.Background(), "PROJ", "FY26-Q1", pageCeiling)
	if err == nil {
		t.Fatal("searchQuarterIssues: expected an error for exceeding the page ceiling, got nil")
	}
	if requests != pageCeiling {
		t.Fatalf("requests = %d, want exactly pageCeiling (%d)", requests, pageCeiling)
	}
	if !strings.Contains(err.Error(), "page ceiling") {
		t.Errorf("err = %v, want it to name the page ceiling", err)
	}
}

func TestClient_SearchQuarterIssues_Non2xx_PassesStatusErrorUnwrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorMessages":["nope"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.SearchQuarterIssues(context.Background(), "PROJ", "FY26-Q1")
	if err == nil {
		t.Fatal("SearchQuarterIssues: expected an error for a non-2xx response, got nil")
	}

	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *StatusError", err)
	}

	if _, ok := err.(*StatusError); !ok {
		t.Errorf("err concrete type = %T, want *StatusError (returned unwrapped)", err)
	}
	if se.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", se.StatusCode, http.StatusForbidden)
	}
	if !strings.Contains(se.Body, "nope") {
		t.Errorf("Body = %q, want it to carry the response body", se.Body)
	}
}

func TestClient_LatestQuarterUpdate_EmitsProbeQueryAndParses(t *testing.T) {
	var gotPath, gotJQL, gotFields, gotMaxResults string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotJQL = r.URL.Query().Get("jql")
		gotFields = r.URL.Query().Get("fields")
		gotMaxResults = r.URL.Query().Get("maxResults")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"key":"PROJ-3","fields":{"updated":"2026-05-06T07:08:09.250-0700"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	got, found, err := c.LatestQuarterUpdate(context.Background(), "PROJ", "FY26-Q1")
	if err != nil {
		t.Fatalf("LatestQuarterUpdate: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	want := time.Date(2026, 5, 6, 7, 8, 9, 250*int(time.Millisecond), time.FixedZone("", -7*3600))
	if !got.Equal(want) {
		t.Errorf("updated = %s, want %s", got, want)
	}
	if gotPath != "/rest/api/3/search/jql" {
		t.Errorf("path = %q, want /rest/api/3/search/jql", gotPath)
	}
	if wantJQL := `project = "PROJ" AND labels = "FY26-Q1" ORDER BY updated DESC`; gotJQL != wantJQL {
		t.Errorf("jql = %q, want %q", gotJQL, wantJQL)
	}
	if gotFields != "updated" {
		t.Errorf("fields = %q, want updated", gotFields)
	}
	if gotMaxResults != "1" {
		t.Errorf("maxResults = %q, want 1", gotMaxResults)
	}
}

func TestClient_LatestQuarterUpdate_EscapesOperands(t *testing.T) {
	var gotJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotJQL = r.URL.Query().Get("jql")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	if _, _, err := c.LatestQuarterUpdate(context.Background(), `PROJ" OR 1=1--`, `FY26-Q1\"`); err != nil {
		t.Fatalf("LatestQuarterUpdate: %v", err)
	}
	want := `project = "PROJ\" OR 1=1--" AND labels = "FY26-Q1\\\"" ORDER BY updated DESC`
	if gotJQL != want {
		t.Errorf("jql = %q, want %q", gotJQL, want)
	}
}

func TestClient_LatestQuarterUpdate_EmptyQuarter_NotFoundNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	got, found, err := c.LatestQuarterUpdate(context.Background(), "PROJ", "FY26-Q1")
	if err != nil {
		t.Fatalf("LatestQuarterUpdate: %v", err)
	}
	if found {
		t.Errorf("found = true, want false for an empty quarter")
	}
	if !got.IsZero() {
		t.Errorf("updated = %s, want the zero time", got)
	}
}

func TestClient_LatestQuarterUpdate_Non2xx_PassesStatusErrorUnwrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorMessages":["nope"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, _, err := c.LatestQuarterUpdate(context.Background(), "PROJ", "FY26-Q1")
	if err == nil {
		t.Fatal("LatestQuarterUpdate: expected an error for a non-2xx response, got nil")
	}
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if _, ok := err.(*StatusError); !ok {
		t.Errorf("err concrete type = %T, want *StatusError (returned unwrapped)", err)
	}
	if se.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", se.StatusCode, http.StatusForbidden)
	}
}

func TestClient_LatestQuarterUpdate_UnparseableTimestamp_IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"key":"PROJ-3","fields":{"updated":"2026-05-06 07:08:09"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, found, err := c.LatestQuarterUpdate(context.Background(), "PROJ", "FY26-Q1")
	if err == nil {
		t.Fatal("LatestQuarterUpdate: expected an error for an unparseable updated timestamp, got nil")
	}
	if !strings.Contains(err.Error(), "updated timestamp") {
		t.Errorf("err = %v, want it to name the updated timestamp", err)
	}
	if found {
		t.Errorf("found = true, want false on a parse failure")
	}
}
