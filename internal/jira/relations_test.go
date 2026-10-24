package jira

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchRelations(t *testing.T) {
	var jqls []string
	var fields []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jqls = append(jqls, r.URL.Query().Get("jql"))
		fields = append(fields, r.URL.Query().Get("fields"))
		_, _ = fmt.Fprint(w, `{"issues":[
			{"key":"P-1","fields":{"status":{"name":"To Do"},"created":"2026-10-01T09:00:00.000+0000"}},
			{"key":"P-2","fields":{"parent":{"key":"P-1"},"status":{"name":"Done"},"created":"2026-10-02T09:00:00.000+0000","resolutiondate":"2026-10-09T09:00:00.000+0000"}}]}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "auth")

	got, err := c.SearchRelations(context.Background(), []string{"P-1", `P"2`})
	if err != nil {
		t.Fatal(err)
	}
	if len(jqls) != 1 || jqls[0] != `key in ("P-1", "P\"2") OR parent in ("P-1", "P\"2")` {
		t.Errorf("jql = %q", jqls)
	}
	if fields[0] != relationsFieldsParam {
		t.Errorf("fields = %q", fields[0])
	}
	if len(got) != 2 || got[1].Fields.Parent == nil || got[1].Fields.Parent.Key != "P-1" ||
		got[1].Fields.Status.Name != "Done" || got[1].Fields.Resolved == nil {
		t.Errorf("got = %+v", got)
	}
}

func TestSearchRelationsChunksAndDedupes(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = fmt.Fprint(w, `{"issues":[{"key":"SAME","fields":{"status":{"name":"To Do"}}}]}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "auth")

	keys := make([]string, relationsKeyChunk+1)
	for i := range keys {
		keys[i] = fmt.Sprintf("P-%d", i)
	}
	got, err := c.SearchRelations(context.Background(), keys)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("requests = %d, want 2 chunks", calls)
	}
	if len(got) != 1 {
		t.Errorf("got %d issues, want 1 after dedupe", len(got))
	}
}

func TestSearchRelationsEmptyKeysNoRequest(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "auth")
	got, err := c.SearchRelations(context.Background(), nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("got %v, %v; want empty non-nil, nil", got, err)
	}
}

func TestSearchRelationsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "auth")
	_, err := c.SearchRelations(context.Background(), []string{"P-1"})
	var se *StatusError
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v (%T), want status error", err, se)
	}
}

func TestSearchRelationsBadRequestMentionsStaleInventory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "issue does not exist", http.StatusBadRequest)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "auth")
	_, err := c.SearchRelations(context.Background(), []string{"P-1"})
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusBadRequest {
		t.Fatalf("err = %v, want wrapped 400 *StatusError", err)
	}
	if !strings.Contains(err.Error(), "stale") || !strings.Contains(err.Error(), "refresh") {
		t.Errorf("err = %q, want stale-inventory and refresh hint", err)
	}
}
