package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEscapeJQLString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain key", "PROJ-123", "PROJ-123"},
		{"embedded quote", `PROJ-1" OR 1=1--`, `PROJ-1\" OR 1=1--`},
		{"embedded backslash", `PROJ-1\x`, `PROJ-1\\x`},
		{"quote and backslash", `PROJ-1\"`, `PROJ-1\\\"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeJQLString(tt.in); got != tt.want {
				t.Errorf("escapeJQLString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

var parentKeyPattern = regexp.MustCompile(`\Aparent = "(.*)"\z`)

func TestClient_ChildrenOf_EscapesKeyInJQL(t *testing.T) {
	const evilKey = `PROJ-1" OR 1=1--`
	wantJQL := `parent = "PROJ-1\" OR 1=1--"`

	var gotJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotJQL = r.URL.Query().Get("jql")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	children, err := c.ChildrenOf(context.Background(), evilKey)
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if children == nil {
		t.Error("children = nil, want non-nil empty slice")
	}
	if len(children) != 0 {
		t.Errorf("len(children) = %d, want 0", len(children))
	}
	if gotJQL != wantJQL {
		t.Errorf("jql sent = %q, want %q", gotJQL, wantJQL)
	}
}

func TestClient_ChildrenOf_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("path = %q, want /rest/api/3/search/jql", r.URL.Path)
		}
		if got := r.URL.Query().Get("jql"); got != `parent = "PROJ-1"` {
			t.Errorf("jql = %q, want parent = \"PROJ-1\"", got)
		}
		if got := r.URL.Query().Get("fields"); got != issueFieldsParam {
			t.Errorf("fields = %q, want %q", got, issueFieldsParam)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[
			{"id":"1","key":"PROJ-2","fields":{"summary":"Child A","labels":["x"],"issuetype":{"id":"5","name":"Story"},"project":{"id":"10000","key":"PROJ"}}}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	children, err := c.ChildrenOf(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if len(children) != 1 || children[0].Key != "PROJ-2" {
		t.Errorf("children = %+v, unexpected", children)
	}
}

func TestClient_ChildrenOf_PopulatesCreated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"PROJ-2","fields":{"created":"2026-01-02T15:04:05.000-0700"}},
			{"key":"PROJ-3","fields":{"summary":"no created"}}
		]}`))
	}))
	defer srv.Close()

	children, err := NewClient(srv.URL, "Bearer tok").ChildrenOf(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("len(children) = %d, want 2", len(children))
	}
	want := time.Date(2026, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*3600))
	if !children[0].Fields.Created.Equal(want) {
		t.Errorf("children[0].Created = %s, want %s", children[0].Fields.Created, want)
	}
	if !children[1].Fields.Created.IsZero() {
		t.Errorf("children[1].Created = %s, want zero", children[1].Fields.Created)
	}
}

func TestClient_ChildrenOf_PopulatesAssignee(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"PROJ-2","fields":{"assignee":{"displayName":"Jamie Rivera"}}},
			{"key":"PROJ-3","fields":{"summary":"null assignee","assignee":null}},
			{"key":"PROJ-4","fields":{"summary":"omitted assignee"}}
		]}`))
	}))
	defer srv.Close()

	children, err := NewClient(srv.URL, "Bearer tok").ChildrenOf(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if len(children) != 3 {
		t.Fatalf("len(children) = %d, want 3", len(children))
	}
	if children[0].Fields.Assignee != "Jamie Rivera" {
		t.Errorf("children[0].Assignee = %q, want %q", children[0].Fields.Assignee, "Jamie Rivera")
	}
	if children[1].Fields.Assignee != "" {
		t.Errorf("children[1].Assignee = %q, want \"\" (null assignee)", children[1].Fields.Assignee)
	}
	if children[2].Fields.Assignee != "" {
		t.Errorf("children[2].Assignee = %q, want \"\" (omitted assignee)", children[2].Fields.Assignee)
	}
}

func TestClient_ChildrenOf_MalformedCreatedFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"key":"PROJ-2","fields":{"created":"not-a-date"}}]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "Bearer tok").ChildrenOf(context.Background(), "PROJ-1")
	if err == nil || !strings.Contains(err.Error(), "created") {
		t.Errorf("err = %v, want created parse error", err)
	}
}

func TestClient_GetIssue_MalformedCreatedFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"key":"P-1","fields":{"created":"not-a-date"}}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "Bearer tok").GetIssue(context.Background(), "P-1")
	if err == nil || !strings.Contains(err.Error(), "created") {
		t.Errorf("err = %v, want created parse error", err)
	}
}

func TestClient_ChildrenOf_NoChildren_ReturnsEmptyNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	children, err := c.ChildrenOf(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if children == nil {
		t.Fatal("children = nil, want non-nil empty slice")
	}
	if len(children) != 0 {
		t.Errorf("len(children) = %d, want 0", len(children))
	}
}

func TestClient_ChildrenOf_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.ChildrenOf(context.Background(), "PROJ-1")
	if err == nil {
		t.Fatal("ChildrenOf: expected error, got nil")
	}
}

func TestClient_ChildrenOf_ResponseSizeLimit(t *testing.T) {
	validPage := []byte(`{"issues":[]}`)
	boundaryBody := append(validPage, []byte(strings.Repeat(" ", int(maxIssueResponseBytes)-len(validPage)))...)
	if int64(len(boundaryBody)) != maxIssueResponseBytes {
		t.Fatalf("boundary body length = %d, want %d", len(boundaryBody), maxIssueResponseBytes)
	}

	t.Run("accepts exact limit", func(t *testing.T) {
		body := &trackingReadCloser{Reader: strings.NewReader(string(boundaryBody))}
		client := NewClient("https://jira.example.test", "Bearer tok")
		client.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		})}

		children, err := client.ChildrenOf(context.Background(), "PROJ-1")
		if err != nil {
			t.Fatalf("ChildrenOf: %v", err)
		}
		if children == nil || len(children) != 0 {
			t.Errorf("children = %#v, want non-nil empty slice", children)
		}
		if !body.closed {
			t.Error("response body was not closed")
		}
	})

	t.Run("rejects overflow without response body", func(t *testing.T) {
		const secret = "search-response-secret-must-not-leak"
		body := &trackingReadCloser{Reader: strings.NewReader(strings.Repeat("x", int(maxIssueResponseBytes+1)) + secret)}
		client := NewClient("https://jira.example.test", "Bearer tok")
		client.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: body, Header: make(http.Header)}, nil
		})}

		_, err := client.ChildrenOf(context.Background(), "PROJ-1")
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

func TestClient_ChildrenOf_Pagination(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("nextPageToken") == "" {
			_, _ = w.Write([]byte(`{"issues":[
				{"id":"1","key":"PROJ-2","fields":{"summary":"A","labels":[],"issuetype":{"id":"5","name":"Story"},"project":{"id":"10000","key":"PROJ"}}}
			],"nextPageToken":"page-2"}`))
			return
		}
		if r.URL.Query().Get("nextPageToken") != "page-2" {
			t.Errorf("nextPageToken = %q, want page-2", r.URL.Query().Get("nextPageToken"))
		}
		_, _ = w.Write([]byte(`{"issues":[
			{"id":"2","key":"PROJ-3","fields":{"summary":"B","labels":[],"issuetype":{"id":"5","name":"Story"},"project":{"id":"10000","key":"PROJ"}}}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	children, err := c.ChildrenOf(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(children) != 2 || children[0].Key != "PROJ-2" || children[1].Key != "PROJ-3" {
		t.Errorf("children = %+v, unexpected", children)
	}
}

func TestClient_ChildrenOf_RepeatedToken_Terminates(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("nextPageToken") == "" {
			_, _ = w.Write([]byte(`{"issues":[
				{"id":"1","key":"PROJ-2","fields":{"summary":"A","labels":[],"issuetype":{"id":"5","name":"Story"},"project":{"id":"10000","key":"PROJ"}}}
			],"nextPageToken":"stuck"}`))
			return
		}

		_, _ = w.Write([]byte(`{"issues":[
			{"id":"2","key":"PROJ-3","fields":{"summary":"B","labels":[],"issuetype":{"id":"5","name":"Story"},"project":{"id":"10000","key":"PROJ"}}}
		],"nextPageToken":"stuck"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	children, err := c.ChildrenOf(context.Background(), "PROJ-1")
	if err == nil {
		t.Fatal("ChildrenOf: expected error for repeated nextPageToken, got nil")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want exactly 2 (stop as soon as the repeat is detected)", requests)
	}
	if len(children) != 2 {
		t.Errorf("children = %+v, want the 2 issues accumulated before the repeat was detected", children)
	}
}

func TestClient_ChildrenOf_EverChangingToken_HitsPageCeiling(t *testing.T) {
	const pageCeiling = 3

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")

		_, _ = fmt.Fprintf(w, `{"issues":[],"nextPageToken":"tok-%d"}`, requests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	_, err := c.childrenOf(context.Background(), "PROJ-1", pageCeiling)
	if err == nil {
		t.Fatal("childrenOf: expected error for exceeding the page ceiling, got nil")
	}
	if requests != pageCeiling {
		t.Fatalf("requests = %d, want exactly pageCeiling (%d)", requests, pageCeiling)
	}
}

type descendantsFixture struct {
	pages      [][]Issue
	failStatus int
}

func newDescendantsServer(t *testing.T, fixtures map[string]*descendantsFixture) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	callIndex := map[string]int{}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		m := parentKeyPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		key := m[1]

		fx, ok := fixtures[key]
		if !ok {
			t.Fatalf("unexpected ChildrenOf request for key %q", key)
		}

		mu.Lock()
		idx := callIndex[key]
		callIndex[key]++
		mu.Unlock()

		if idx >= len(fx.pages) {
			if fx.failStatus != 0 {
				w.WriteHeader(fx.failStatus)
				return
			}
			t.Fatalf("more ChildrenOf requests than configured pages for key %q", key)
		}

		resp := searchPage{Issues: fx.pages[idx]}
		if idx < len(fx.pages)-1 || fx.failStatus != 0 {
			resp.NextPageToken = fmt.Sprintf("%s-page-%d", key, idx+1)
		}
		body, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal fixture response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
}

func issueStub(key string) Issue {
	return Issue{
		ID:  key,
		Key: key,
		Fields: IssueFields{
			Summary:   key + " summary",
			Labels:    []string{},
			IssueType: IssueType{ID: "5", Name: "Story"},
			Project:   Project{ID: "10000", Key: "PROJ"},
		},
	}
}

func TestClient_WalkDescendants_MultiBranchMultiDepth(t *testing.T) {
	fixtures := map[string]*descendantsFixture{
		"ROOT": {pages: [][]Issue{{issueStub("A"), issueStub("B")}}},
		"A":    {pages: [][]Issue{{issueStub("A1"), issueStub("A2")}}},
		"B":    {pages: [][]Issue{{}}},
		"A1":   {pages: [][]Issue{{}}},
		"A2":   {pages: [][]Issue{{}}},
	}
	srv := newDescendantsServer(t, fixtures)
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.WalkDescendants(context.Background(), "ROOT")
	if err != nil {
		t.Fatalf("WalkDescendants: %v", err)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("Failures = %+v, want none", result.Failures)
	}

	byKey := map[string]DescendantNode{}
	for _, n := range result.Nodes {
		byKey[n.Issue.Key] = n
	}
	if _, ok := byKey["ROOT"]; ok {
		t.Error("Nodes contains the root/target; WalkDescendants must not include the starting item")
	}
	if len(byKey) != 4 {
		t.Fatalf("len(Nodes) = %d, want 4 (A, B, A1, A2); got %+v", len(byKey), result.Nodes)
	}

	wantDepthParent := map[string][2]any{
		"A":  {1, "ROOT"},
		"B":  {1, "ROOT"},
		"A1": {2, "A"},
		"A2": {2, "A"},
	}
	for key, want := range wantDepthParent {
		n, ok := byKey[key]
		if !ok {
			t.Errorf("missing node %q", key)
			continue
		}
		if n.Depth != want[0] {
			t.Errorf("%s.Depth = %d, want %d", key, n.Depth, want[0])
		}
		if n.ParentKey != want[1] {
			t.Errorf("%s.ParentKey = %q, want %q", key, n.ParentKey, want[1])
		}
	}
}

func TestClient_WalkDescendants_Cycle_Terminates(t *testing.T) {

	fixtures := map[string]*descendantsFixture{
		"ROOT": {pages: [][]Issue{{issueStub("X")}}},
		"X":    {pages: [][]Issue{{issueStub("Y")}}},
		"Y":    {pages: [][]Issue{{issueStub("X")}}},
	}
	srv := newDescendantsServer(t, fixtures)
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	var result *DescendantWalkResult
	var err error
	go func() {
		result, err = c.WalkDescendants(ctx, "ROOT")
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("WalkDescendants did not terminate on a cyclic graph within the test timeout")
	}

	if err != nil {
		t.Fatalf("WalkDescendants: %v", err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("len(Nodes) = %d, want 2 (X, Y); got %+v", len(result.Nodes), result.Nodes)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("Failures = %+v, want none", result.Failures)
	}
}

func TestClient_WalkDescendants_PerNodeFailure_DoesNotAbortWalk(t *testing.T) {

	fixtures := map[string]*descendantsFixture{
		"ROOT": {pages: [][]Issue{{issueStub("A"), issueStub("B")}}},
		"A":    {failStatus: http.StatusInternalServerError},
		"B":    {pages: [][]Issue{{issueStub("B1")}}},
		"B1":   {pages: [][]Issue{{}}},
	}
	srv := newDescendantsServer(t, fixtures)
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.WalkDescendants(context.Background(), "ROOT")
	if err != nil {
		t.Fatalf("WalkDescendants: %v", err)
	}

	byKey := map[string]DescendantNode{}
	for _, n := range result.Nodes {
		byKey[n.Issue.Key] = n
	}
	for _, want := range []string{"A", "B", "B1"} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("missing node %q; rest of walk should complete despite A's failure", want)
		}
	}

	if len(result.Failures) != 1 {
		t.Fatalf("Failures = %+v, want exactly 1", result.Failures)
	}
	f := result.Failures[0]
	if f.Key != "A" || f.ParentKey != "ROOT" || f.Depth != 1 {
		t.Errorf("Failures[0] = %+v, want Key=A ParentKey=ROOT Depth=1", f)
	}
	if f.Err == nil {
		t.Error("Failures[0].Err = nil, want the underlying fetch error")
	}
}

func TestClient_WalkDescendants_NoDescendants(t *testing.T) {
	fixtures := map[string]*descendantsFixture{
		"ROOT": {pages: [][]Issue{{}}},
	}
	srv := newDescendantsServer(t, fixtures)
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.WalkDescendants(context.Background(), "ROOT")
	if err != nil {
		t.Fatalf("WalkDescendants: %v", err)
	}
	if result.Nodes == nil || len(result.Nodes) != 0 {
		t.Errorf("Nodes = %+v, want non-nil empty slice", result.Nodes)
	}
	if result.Failures == nil || len(result.Failures) != 0 {
		t.Errorf("Failures = %+v, want non-nil empty slice", result.Failures)
	}
}

func TestClient_WalkDescendants_LaterPageFailure_PreservesAndWalksFirstPageChildren(t *testing.T) {

	fixtures := map[string]*descendantsFixture{
		"ROOT": {pages: [][]Issue{{issueStub("A"), issueStub("B")}}},
		"A":    {pages: [][]Issue{{issueStub("A1")}}, failStatus: http.StatusInternalServerError},
		"A1":   {pages: [][]Issue{{issueStub("A1a")}}},
		"A1a":  {pages: [][]Issue{{}}},
		"B":    {pages: [][]Issue{{}}},
	}
	srv := newDescendantsServer(t, fixtures)
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.WalkDescendants(context.Background(), "ROOT")
	if err != nil {
		t.Fatalf("WalkDescendants: %v", err)
	}

	if len(result.Failures) != 1 {
		t.Fatalf("Failures = %+v, want exactly 1 (A's later-page failure)", result.Failures)
	}
	if f := result.Failures[0]; f.Key != "A" || f.ParentKey != "ROOT" || f.Depth != 1 {
		t.Errorf("Failures[0] = %+v, want Key=A ParentKey=ROOT Depth=1", f)
	}

	wantDepthParent := map[string][2]any{
		"A":   {1, "ROOT"},
		"B":   {1, "ROOT"},
		"A1":  {2, "A"},
		"A1a": {3, "A1"},
	}
	byKey := map[string]DescendantNode{}
	for _, n := range result.Nodes {
		byKey[n.Issue.Key] = n
	}
	if len(byKey) != len(wantDepthParent) {
		t.Fatalf("Nodes = %+v, want exactly %v", result.Nodes, wantDepthParent)
	}
	for key, want := range wantDepthParent {
		n, ok := byKey[key]
		if !ok {
			t.Errorf("missing node %q -- A's page-1 children must still be enqueued and walked despite A's page-2 failure", key)
			continue
		}
		if n.Depth != want[0] || n.ParentKey != want[1] {
			t.Errorf("%s = {Depth:%d ParentKey:%q}, want {Depth:%d ParentKey:%q}", key, n.Depth, n.ParentKey, want[0], want[1])
		}
	}
}

func TestClient_WalkDescendants_ContextCancelled_LastQueuedItem_ReturnsHardError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		m := parentKeyPattern.FindStringSubmatch(jql)
		if m == nil {
			t.Fatalf("could not extract parent key from jql %q", jql)
		}
		switch m[1] {
		case "ROOT":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issues":[
				{"id":"1","key":"A","fields":{"summary":"A","labels":[],"issuetype":{"id":"5","name":"Story"},"project":{"id":"10000","key":"PROJ"}}}
			]}`))
		case "A":

			cancel()
			<-r.Context().Done()
		default:
			t.Fatalf("unexpected request for key %q", m[1])
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")

	done := make(chan struct{})
	var result *DescendantWalkResult
	var err error
	go func() {
		result, err = c.WalkDescendants(ctx, "ROOT")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("WalkDescendants did not return after its context was cancelled")
	}

	if err == nil {
		t.Fatal("WalkDescendants: expected a hard error from context cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(result.Failures) != 0 {
		t.Errorf("Failures = %+v, want none -- cancellation must never be recorded as a per-node DescendantFailure", result.Failures)
	}
	found := false
	for _, n := range result.Nodes {
		if n.Issue.Key == "A" {
			found = true
		}
	}
	if !found {
		t.Error("Nodes missing A -- already-gathered nodes must be preserved when cancellation aborts the walk")
	}
}

func TestClient_WalkDescendantsFromChildren_NilFirstHopDoesNotFetchRoot(t *testing.T) {
	var rootCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := parentKeyPattern.FindStringSubmatch(r.URL.Query().Get("jql")); len(got) == 2 && got[1] == "ROOT" {
			rootCalls++
		}
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.WalkDescendantsFromChildren(context.Background(), "ROOT", nil)
	if err != nil {
		t.Fatalf("WalkDescendantsFromChildren: %v", err)
	}
	if rootCalls != 0 {
		t.Errorf("ChildrenOf(ROOT) calls = %d, want 0 for a supplied nil first hop", rootCalls)
	}
	if len(result.Nodes) != 0 || len(result.Failures) != 0 {
		t.Errorf("result = %+v, want empty nodes and failures", result)
	}
}

func TestClient_WalkDescendants_MaxNodeCeiling_StopsAndReturnsPartialResult(t *testing.T) {
	const nodeCeiling = 3

	fixtures := map[string]*descendantsFixture{
		"ROOT": {pages: [][]Issue{{issueStub("A"), issueStub("B")}}},
		"A":    {pages: [][]Issue{{issueStub("A1"), issueStub("A2")}}},
		"B":    {pages: [][]Issue{{issueStub("B1")}}},
	}
	srv := newDescendantsServer(t, fixtures)
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.walkDescendants(context.Background(), "ROOT", false, nil, nodeCeiling)
	if err == nil {
		t.Fatal("walkDescendants: expected a hard error for exceeding the max-node ceiling, got nil")
	}

	wantKeys := map[string]bool{"A": true, "B": true, "A1": true}
	if len(result.Nodes) != len(wantKeys) {
		t.Fatalf("Nodes = %+v, want exactly %v", result.Nodes, wantKeys)
	}
	for _, n := range result.Nodes {
		if !wantKeys[n.Issue.Key] {
			t.Errorf("unexpected node %q past the ceiling -- walk should have stopped enqueuing", n.Issue.Key)
		}
	}
}

func TestClient_WalkDescendants_Diamond_KeptOnceUnderFirstDiscoveredLineage(t *testing.T) {

	fixtures := map[string]*descendantsFixture{
		"ROOT": {pages: [][]Issue{{issueStub("A"), issueStub("B")}}},
		"A":    {pages: [][]Issue{{issueStub("D")}}},
		"B":    {pages: [][]Issue{{issueStub("D")}}},
		"D":    {pages: [][]Issue{{}}},
	}
	srv := newDescendantsServer(t, fixtures)
	defer srv.Close()

	c := NewClient(srv.URL, "Bearer tok")
	result, err := c.WalkDescendants(context.Background(), "ROOT")
	if err != nil {
		t.Fatalf("WalkDescendants: %v", err)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("Failures = %+v, want none", result.Failures)
	}

	var dCount int
	var dNode DescendantNode
	for _, n := range result.Nodes {
		if n.Issue.Key == "D" {
			dCount++
			dNode = n
		}
	}
	if dCount != 1 {
		t.Fatalf("D appears %d times in Nodes, want exactly 1", dCount)
	}
	if dNode.Depth != 2 || dNode.ParentKey != "A" {
		t.Errorf("D = {Depth:%d ParentKey:%q}, want {Depth:2 ParentKey:\"A\"} (first breadth-first lineage, via A)", dNode.Depth, dNode.ParentKey)
	}
	if len(result.Nodes) != 3 {
		t.Fatalf("len(Nodes) = %d, want 3 (A, B, D)", len(result.Nodes))
	}
}
