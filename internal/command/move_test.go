package command

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

type moveTestIssue struct {
	summary string
	labels  []string
}

func newMovePreflightServer(t *testing.T, issues map[string]moveTestIssue) (*httptest.Server, *[]string) {
	t.Helper()
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		calls = append(calls, r.Method+":"+key)
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected Jira write during pre-flight: %s %s", r.Method, r.URL.Path)
		}
		issue, ok := issues[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":` + labelsJSON(issue.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
	}))
	return srv, &calls
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
func labelsJSON(labels []string) string {
	b, _ := json.Marshal(labels)
	return string(b)
}

func moveClient(srv *httptest.Server) *jira.Client { return jira.NewClient(srv.URL, "Bearer token") }

func TestPreflightMove_OrderedFailuresAndNoWrites(t *testing.T) {
	base := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget Q3", labels: []string{"FY26-Q3"}},
		"PROJ-001": {summary: "Parent", labels: nil},
	}
	cases := []struct {
		name      string
		issues    map[string]moveTestIssue
		in        MoveInput
		wantType  any
		wantCalls []string
	}{
		{"source", base, MoveInput{SourceKey: "MISSING", HasParent: true, ParentKey: "PROJ-001"}, &ErrMoveSourceNotFound{Subject: "move source"}, []string{"GET:MISSING"}},
		{"summary", base, MoveInput{SourceKey: "PROJ-123", HasSummary: true, SummaryOld: "Q2", SummaryNew: "Q4", HasParent: true, ParentKey: "PROJ-001"}, &ErrMoveSummaryNotFound{}, []string{"GET:PROJ-123"}},
		{"missing label", base, MoveInput{SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q2", LabelNew: "FY26-Q4", HasParent: true, ParentKey: "PROJ-001"}, &ErrMoveLabelNotFound{}, []string{"GET:PROJ-123"}},
		{"empty label", base, MoveInput{SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "", HasParent: true, ParentKey: "PROJ-001"}, ErrMoveLabelNewEmpty, []string{"GET:PROJ-123"}},
		{"whitespace label", base, MoveInput{SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26 Q4", HasParent: true, ParentKey: "PROJ-001"}, &ErrMoveInvalidLabel{}, []string{"GET:PROJ-123"}},
		{"parent", base, MoveInput{SourceKey: "PROJ-123", HasParent: true, ParentKey: "MISSING"}, &ErrMoveParentNotFound{Subject: "parent"}, []string{"GET:PROJ-123", "GET:MISSING"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := newMovePreflightServer(t, tt.issues)
			defer srv.Close()
			_, err := PreflightMove(context.Background(), moveClient(srv), tt.in)
			if err == nil {
				t.Fatal("PreflightMove returned nil error")
			}
			assertMovePreflightError(t, err, tt.wantType)
			if !reflect.DeepEqual(*calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", *calls, tt.wantCalls)
			}
		})
	}
}

func TestPreflightMove_LabelNewInvalid_UnicodeWhitespace(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget Q3", labels: []string{"FY26-Q3"}},
	}
	srv, calls := newMovePreflightServer(t, issues)
	defer srv.Close()

	_, err := PreflightMove(context.Background(), moveClient(srv), MoveInput{
		SourceKey: "PROJ-123",
		HasLabel:  true,
		LabelOld:  "FY26-Q3",
		LabelNew:  "FY26\u00a0Q4",
		HasParent: true,
		ParentKey: "MISSING",
	})
	var invalidLabel *ErrMoveInvalidLabel
	if !errors.As(err, &invalidLabel) {
		t.Fatalf("error = %T (%v), want *ErrMoveInvalidLabel", err, err)
	}
	if invalidLabel.Label != "FY26\u00a0Q4" {
		t.Errorf("Label = %q, want %q", invalidLabel.Label, "FY26\u00a0Q4")
	}
	if want := []string{"GET:PROJ-123"}; !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

func assertMovePreflightError(t *testing.T, err error, want any) {
	t.Helper()
	switch want := want.(type) {
	case *IssueNotFoundError:

		var target *IssueNotFoundError
		if !errors.As(err, &target) {
			t.Fatalf("error = %T (%v), want *IssueNotFoundError", err, err)
		}
		if target.Subject != want.Subject {
			t.Fatalf("Subject = %q, want %q", target.Subject, want.Subject)
		}
		switch want.Subject {
		case "move source":
			if got, exp := target.Error(), `move source "MISSING" not found (404)`; got != exp {
				t.Errorf("Error() = %q, want %q", got, exp)
			}
		case "parent":
			if got, exp := target.Error(), `parent "MISSING" not found (404)`; got != exp {
				t.Errorf("Error() = %q, want %q", got, exp)
			}
		default:
			t.Fatalf("unsupported expected Subject %q", want.Subject)
		}
	case *ErrMoveSummaryNotFound:
		var target *ErrMoveSummaryNotFound
		if !errors.As(err, &target) {
			t.Fatalf("error = %T (%v), want *ErrMoveSummaryNotFound", err, err)
		}
	case *ErrMoveLabelNotFound:
		var target *ErrMoveLabelNotFound
		if !errors.As(err, &target) {
			t.Fatalf("error = %T (%v), want *ErrMoveLabelNotFound", err, err)
		}
	case *ErrMoveInvalidLabel:
		var target *ErrMoveInvalidLabel
		if !errors.As(err, &target) {
			t.Fatalf("error = %T (%v), want *ErrMoveInvalidLabel", err, err)
		}
	case error:
		if !errors.Is(err, want) {
			t.Fatalf("error = %T (%v), want %v", err, err, want)
		}
	default:
		t.Fatalf("unsupported expected error %T", want)
	}
}

type moveWriteCall struct {
	method string
	key    string
	body   map[string]any
}

func newMoveWriteServer(t *testing.T, issues map[string]moveTestIssue, statusFor map[string]struct {
	code int
	body string
}) (*httptest.Server, *[]moveWriteCall) {
	t.Helper()
	calls := []moveWriteCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		switch r.Method {
		case http.MethodGet:
			issue, ok := issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":` + labelsJSON(issue.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
		case http.MethodPut:
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			calls = append(calls, moveWriteCall{method: r.Method, key: key, body: decoded.Fields})
			if sf, ok := statusFor[key]; ok {
				w.WriteHeader(sf.code)
				_, _ = w.Write([]byte(sf.body))
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method during ApplyMove: %s %s", r.Method, r.URL.Path)
		}
	}))
	return srv, &calls
}

func TestApplyMove_ParentOnly(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget Q3", labels: []string{"FY26-Q3", "team-widgets"}},
		"PROJ-001": {summary: "Parent", labels: nil},
	}
	srv, calls := newMoveWriteServer(t, issues, nil)
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001"}
	pf, err := PreflightMove(context.Background(), client, in)
	if err != nil {
		t.Fatalf("PreflightMove: %v", err)
	}

	res := ApplyMove(context.Background(), client, in, pf)

	if !res.ParentAttempted || res.ParentErr != nil || res.ParentKey != "PROJ-001" {
		t.Errorf("ParentAttempted/ParentErr/ParentKey = %v/%v/%v, want true/nil/PROJ-001", res.ParentAttempted, res.ParentErr, res.ParentKey)
	}
	if res.SummaryAttempted || res.LabelsAttempted {
		t.Errorf("SummaryAttempted/LabelsAttempted = %v/%v, want false/false", res.SummaryAttempted, res.LabelsAttempted)
	}

	if len(*calls) != 1 {
		t.Fatalf("write calls = %+v, want exactly one (SetParent)", *calls)
	}
	c := (*calls)[0]
	if c.key != "PROJ-123" {
		t.Errorf("write call key = %q, want PROJ-123", c.key)
	}
	if _, hasSummary := c.body["summary"]; hasSummary {
		t.Errorf("unexpected summary field in SetParent call body: %+v", c.body)
	}
	if _, hasLabels := c.body["labels"]; hasLabels {
		t.Errorf("unexpected labels field in SetParent call body: %+v", c.body)
	}
	parentField, _ := c.body["parent"].(map[string]any)
	if parentField["key"] != "PROJ-001" {
		t.Errorf("parent field = %+v, want key PROJ-001", c.body["parent"])
	}
}

func TestApplyMove_RewritePairsOnly_NoParent(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Q3 work for Q3 team", labels: []string{"FY26-Q3", "team-widgets"}},
	}
	srv, calls := newMoveWriteServer(t, issues, nil)
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{
		SourceKey:  "PROJ-123",
		HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4",
		HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
	}
	pf, err := PreflightMove(context.Background(), client, in)
	if err != nil {
		t.Fatalf("PreflightMove: %v", err)
	}

	res := ApplyMove(context.Background(), client, in, pf)

	if res.ParentAttempted {
		t.Errorf("ParentAttempted = true, want false (no --parent given must never touch the parent)")
	}
	if !res.SummaryAttempted || res.SummaryErr != nil {
		t.Errorf("SummaryAttempted/SummaryErr = %v/%v, want true/nil", res.SummaryAttempted, res.SummaryErr)
	}
	if want := "Q4 work for Q4 team"; res.NewSummary != want {
		t.Errorf("NewSummary = %q, want %q (multiple-occurrence substring replace)", res.NewSummary, want)
	}
	if !res.LabelsAttempted || res.LabelsErr != nil {
		t.Errorf("LabelsAttempted/LabelsErr = %v/%v, want true/nil", res.LabelsAttempted, res.LabelsErr)
	}
	if want := []string{"FY26-Q4", "team-widgets"}; !reflect.DeepEqual(res.Labels, want) {
		t.Errorf("Labels = %v, want %v", res.Labels, want)
	}
	if want := []string{"team-widgets"}; !reflect.DeepEqual(res.OtherLabels, want) {
		t.Errorf("OtherLabels = %v, want %v", res.OtherLabels, want)
	}

	if len(*calls) != 2 {
		t.Fatalf("write calls = %+v, want exactly two (SetSummary, SetLabels; no SetParent)", *calls)
	}
	for _, c := range *calls {
		if _, hasParent := c.body["parent"]; hasParent {
			t.Errorf("unexpected parent field in write call body: %+v", c.body)
		}
	}
}

func TestApplyMove_ParentAndRewrite(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget Q3", labels: []string{"FY26-Q3", "team-widgets"}},
		"PROJ-001": {summary: "Parent", labels: nil},
	}
	srv, calls := newMoveWriteServer(t, issues, nil)
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
		HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4",
		HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
	}
	pf, err := PreflightMove(context.Background(), client, in)
	if err != nil {
		t.Fatalf("PreflightMove: %v", err)
	}

	res := ApplyMove(context.Background(), client, in, pf)

	if !res.ParentAttempted || !res.SummaryAttempted || !res.LabelsAttempted {
		t.Errorf("Attempted flags = %v/%v/%v, want true/true/true", res.ParentAttempted, res.SummaryAttempted, res.LabelsAttempted)
	}
	if res.ParentErr != nil || res.SummaryErr != nil || res.LabelsErr != nil {
		t.Errorf("errors = %v/%v/%v, want all nil", res.ParentErr, res.SummaryErr, res.LabelsErr)
	}
	if len(*calls) != 3 {
		t.Fatalf("write calls = %+v, want exactly three", *calls)
	}
}

func TestApplyMove_LabelSwapCollapsesOntoExistingLabel(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget", labels: []string{"FY26-Q3", "FY26-Q4", "team-widgets"}},
	}
	srv, _ := newMoveWriteServer(t, issues, nil)
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4"}
	pf, err := PreflightMove(context.Background(), client, in)
	if err != nil {
		t.Fatalf("PreflightMove: %v", err)
	}

	res := ApplyMove(context.Background(), client, in, pf)

	if want := []string{"FY26-Q4", "team-widgets"}; !reflect.DeepEqual(res.Labels, want) {
		t.Errorf("Labels = %v, want %v (collapsed, no duplicate)", res.Labels, want)
	}
	if want := []string{"team-widgets"}; !reflect.DeepEqual(res.OtherLabels, want) {
		t.Errorf("OtherLabels = %v, want %v", res.OtherLabels, want)
	}
}

func TestApplyMove_WritesAreIndependent(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-123": {summary: "Widget Q3", labels: []string{"FY26-Q3", "team-widgets"}},
		"PROJ-001": {summary: "Parent", labels: nil},
	}
	const poison = "Bearer reflected-authorization secret-payload token=abc123"
	srv, calls := newMoveWriteServer(t, issues, map[string]struct {
		code int
		body string
	}{
		"PROJ-123": {code: http.StatusBadGateway, body: poison},
	})
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
		HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4",
		HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
	}
	pf, err := PreflightMove(context.Background(), client, in)
	if err != nil {
		t.Fatalf("PreflightMove: %v", err)
	}

	res := ApplyMove(context.Background(), client, in, pf)

	if !res.ParentAttempted || !res.SummaryAttempted || !res.LabelsAttempted {
		t.Fatalf("Attempted flags = %v/%v/%v, want all true", res.ParentAttempted, res.SummaryAttempted, res.LabelsAttempted)
	}
	if res.ParentErr == nil || res.SummaryErr == nil || res.LabelsErr == nil {
		t.Fatalf("errors = %v/%v/%v, want all non-nil", res.ParentErr, res.SummaryErr, res.LabelsErr)
	}
	var statusErr *jira.StatusError
	for _, err := range []error{res.ParentErr, res.SummaryErr, res.LabelsErr} {
		if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusBadGateway {
			t.Errorf("err = %v, want *jira.StatusError{StatusCode: 502}", err)
		}
	}
	if len(*calls) != 3 {
		t.Fatalf("write calls = %+v, want exactly three (each write still attempted despite failures)", *calls)
	}
}

func TestPreflightMove_SkipsUnrequestedChecks(t *testing.T) {
	issues := map[string]moveTestIssue{"PROJ-123": {summary: "anything", labels: nil}}
	srv, calls := newMovePreflightServer(t, issues)
	defer srv.Close()
	preflight, err := PreflightMove(context.Background(), moveClient(srv), MoveInput{SourceKey: "PROJ-123"})
	if err != nil {
		t.Fatalf("PreflightMove: %v", err)
	}
	if preflight.Source.Key != "PROJ-123" || preflight.Parent != nil {
		t.Errorf("preflight = %+v, want source only", preflight)
	}
	if want := []string{"GET:PROJ-123"}; !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

var descendantParentKeyPattern = regexp.MustCompile(`\Aparent = "(.*)"\z`)

type descendantTreeNode struct {
	summary  string
	labels   []string
	children []string
}

type descendantServerOpts struct {
	childrenOfFailStatus map[string]int
	writeFailStatus      map[string]int
	writeFailBody        map[string]string
	onChildrenOfRequest  map[string]func()
}

func newDescendantTreeServer(t *testing.T, tree map[string]descendantTreeNode, opts descendantServerOpts) (*httptest.Server, *[]moveWriteCall) {
	t.Helper()
	calls := []moveWriteCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search/jql":
			jql := r.URL.Query().Get("jql")
			m := descendantParentKeyPattern.FindStringSubmatch(jql)
			if m == nil {
				t.Fatalf("could not extract parent key from jql %q", jql)
			}
			key := m[1]
			if hook, ok := opts.onChildrenOfRequest[key]; ok {
				hook()
			}
			if status, ok := opts.childrenOfFailStatus[key]; ok {
				w.WriteHeader(status)
				return
			}
			node, ok := tree[key]
			var b strings.Builder
			b.WriteString(`{"issues":[`)
			if ok {
				for i, childKey := range node.children {
					child := tree[childKey]
					if i > 0 {
						b.WriteString(",")
					}
					b.WriteString(`{"id":"1","key":"` + childKey + `","fields":{"summary":` + quoteJSON(child.summary) + `,"labels":` + labelsJSON(child.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`)
				}
			}
			b.WriteString(`]}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(b.String()))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			node, ok := tree[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(node.summary) + `,"labels":` + labelsJSON(node.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))

		case r.Method == http.MethodPut:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			calls = append(calls, moveWriteCall{method: r.Method, key: key, body: decoded.Fields})
			if status, ok := opts.writeFailStatus[key]; ok {
				w.WriteHeader(status)
				if body, ok := opts.writeFailBody[key]; ok {
					_, _ = w.Write([]byte(body))
				}
				return
			}
			w.WriteHeader(http.StatusOK)

		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	return srv, &calls
}

func TestApplyDescendants_MultiBranchMultiDepth_RewritesAll(t *testing.T) {
	tree := map[string]descendantTreeNode{
		"ROOT": {summary: "Q3 root", labels: []string{"FY26-Q3"}, children: []string{"A", "B"}},
		"A":    {summary: "Q3 child A", labels: []string{"FY26-Q3"}, children: []string{"A1", "A2"}},
		"B":    {summary: "Q3 child B", labels: []string{"FY26-Q3"}, children: nil},
		"A1":   {summary: "Q3 grandchild A1", labels: []string{"FY26-Q3"}, children: []string{"A1a"}},
		"A2":   {summary: "Q3 grandchild A2", labels: []string{"FY26-Q3"}, children: nil},
		"A1a":  {summary: "Q3 great-grandchild A1a", labels: []string{"FY26-Q3"}, children: nil},
	}
	srv, calls := newDescendantTreeServer(t, tree, descendantServerOpts{})
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{
		SourceKey:  "ROOT",
		HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4",
		HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
	}
	res := ApplyDescendants(context.Background(), client, in)

	if !res.Attempted || res.WalkErr != nil || len(res.ChildrenOfFailures) != 0 {
		t.Fatalf("Attempted/WalkErr/ChildrenOfFailures = %v/%v/%v, want true/nil/none", res.Attempted, res.WalkErr, res.ChildrenOfFailures)
	}
	if len(res.Descendants) != 5 {
		t.Fatalf("len(Descendants) = %d, want 5 (A, B, A1, A2, A1a); got %+v", len(res.Descendants), res.Descendants)
	}

	byKey := map[string]DescendantWriteResult{}
	for _, d := range res.Descendants {
		byKey[d.Key] = d
	}
	wantDepth := map[string]int{"A": 1, "B": 1, "A1": 2, "A2": 2, "A1a": 3}
	for key, wantDepth := range wantDepth {
		d, ok := byKey[key]
		if !ok {
			t.Errorf("missing descendant %q", key)
			continue
		}
		if d.Depth != wantDepth {
			t.Errorf("%s.Depth = %d, want %d", key, d.Depth, wantDepth)
		}
		if !d.SummaryAttempted || d.SummaryErr != nil {
			t.Errorf("%s: SummaryAttempted/SummaryErr = %v/%v, want true/nil", key, d.SummaryAttempted, d.SummaryErr)
		}
		if !d.LabelsAttempted || d.LabelsErr != nil {
			t.Errorf("%s: LabelsAttempted/LabelsErr = %v/%v, want true/nil", key, d.LabelsAttempted, d.LabelsErr)
		}
	}
	if got, want := byKey["A1a"].NewSummary, "Q4 great-grandchild A1a"; got != want {
		t.Errorf("A1a.NewSummary = %q, want %q (grandchildren-and-deeper must be rewritten)", got, want)
	}

	putCount := 0
	for _, c := range *calls {
		if c.method == http.MethodPut {
			putCount++
		}
	}
	if want := 10; putCount != want {
		t.Errorf("PUT calls = %d, want %d", putCount, want)
	}
}

func TestApplyDescendants_NeverWritesParentField(t *testing.T) {
	tree := map[string]descendantTreeNode{
		"ROOT": {summary: "Q3 root", labels: []string{"FY26-Q3"}, children: []string{"A"}},
		"A":    {summary: "Q3 child A", labels: []string{"FY26-Q3"}, children: []string{"A1"}},
		"A1":   {summary: "Q3 grandchild A1", labels: []string{"FY26-Q3"}, children: nil},
	}
	srv, calls := newDescendantTreeServer(t, tree, descendantServerOpts{})
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{
		SourceKey:  "ROOT",
		HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4",
		HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
	}
	res := ApplyDescendants(context.Background(), client, in)
	if res.WalkErr != nil || len(res.ChildrenOfFailures) != 0 {
		t.Fatalf("WalkErr/ChildrenOfFailures = %v/%v, want nil/none", res.WalkErr, res.ChildrenOfFailures)
	}
	if len(*calls) == 0 {
		t.Fatal("no write calls recorded")
	}
	for _, c := range *calls {
		if _, hasParent := c.body["parent"]; hasParent {
			t.Errorf("descendant write to %s carried a parent field: %+v", c.key, c.body)
		}
	}
}

func TestApplyDescendants_BestEffortMatrix(t *testing.T) {
	tree := map[string]descendantTreeNode{
		"ROOT":         {summary: "root", labels: nil, children: []string{"BOTH", "SUMMARY_ONLY", "LABEL_ONLY", "NEITHER"}},
		"BOTH":         {summary: "Q3 item", labels: []string{"FY26-Q3"}},
		"SUMMARY_ONLY": {summary: "Q3 item", labels: []string{"team-widgets"}},
		"LABEL_ONLY":   {summary: "unrelated summary", labels: []string{"FY26-Q3"}},
		"NEITHER":      {summary: "unrelated summary", labels: []string{"team-widgets"}},
	}
	srv, _ := newDescendantTreeServer(t, tree, descendantServerOpts{})
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{
		SourceKey:  "ROOT",
		HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4",
		HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
	}
	res := ApplyDescendants(context.Background(), client, in)
	if res.WalkErr != nil || len(res.ChildrenOfFailures) != 0 {
		t.Fatalf("WalkErr/ChildrenOfFailures = %v/%v, want nil/none", res.WalkErr, res.ChildrenOfFailures)
	}
	byKey := map[string]DescendantWriteResult{}
	for _, d := range res.Descendants {
		byKey[d.Key] = d
	}

	both := byKey["BOTH"]
	if !both.SummaryAttempted || !both.LabelsAttempted {
		t.Errorf("BOTH: SummaryAttempted/LabelsAttempted = %v/%v, want true/true", both.SummaryAttempted, both.LabelsAttempted)
	}

	summaryOnly := byKey["SUMMARY_ONLY"]
	if !summaryOnly.SummaryAttempted || summaryOnly.LabelsAttempted {
		t.Errorf("SUMMARY_ONLY: SummaryAttempted/LabelsAttempted = %v/%v, want true/false", summaryOnly.SummaryAttempted, summaryOnly.LabelsAttempted)
	}

	labelOnly := byKey["LABEL_ONLY"]
	if labelOnly.SummaryAttempted || !labelOnly.LabelsAttempted {
		t.Errorf("LABEL_ONLY: SummaryAttempted/LabelsAttempted = %v/%v, want false/true", labelOnly.SummaryAttempted, labelOnly.LabelsAttempted)
	}

	neither := byKey["NEITHER"]
	if neither.SummaryAttempted || neither.LabelsAttempted {
		t.Errorf("NEITHER: SummaryAttempted/LabelsAttempted = %v/%v, want false/false", neither.SummaryAttempted, neither.LabelsAttempted)
	}
}

func TestApplyDescendants_ChildrenOfFailure_OtherBranchesContinue(t *testing.T) {
	tree := map[string]descendantTreeNode{
		"ROOT": {summary: "Q3 root", children: []string{"A", "B"}},
		"A":    {summary: "Q3 A"},
		"B":    {summary: "Q3 B", children: []string{"B1"}},
		"B1":   {summary: "Q3 B1"},
	}
	srv, _ := newDescendantTreeServer(t, tree, descendantServerOpts{
		childrenOfFailStatus: map[string]int{"A": http.StatusInternalServerError},
	})
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{SourceKey: "ROOT", HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4"}
	res := ApplyDescendants(context.Background(), client, in)

	if res.WalkErr != nil {
		t.Fatalf("WalkErr = %v, want nil (a per-node ChildrenOf failure must not surface as a whole-walk error)", res.WalkErr)
	}
	if len(res.ChildrenOfFailures) != 1 {
		t.Fatalf("ChildrenOfFailures = %+v, want exactly 1", res.ChildrenOfFailures)
	}
	f := res.ChildrenOfFailures[0]
	if f.Key != "A" || f.ParentKey != "ROOT" || f.Depth != 1 || f.Err == nil {
		t.Errorf("ChildrenOfFailures[0] = %+v, want Key=A ParentKey=ROOT Depth=1 with a non-nil Err", f)
	}

	byKey := map[string]DescendantWriteResult{}
	for _, d := range res.Descendants {
		byKey[d.Key] = d
	}
	for _, want := range []string{"A", "B", "B1"} {
		d, ok := byKey[want]
		if !ok {
			t.Errorf("missing descendant %q; B's branch must still be walked despite A's ChildrenOf failure", want)
			continue
		}
		if want != "A" && (!d.SummaryAttempted || d.SummaryErr != nil) {
			t.Errorf("%s: SummaryAttempted/SummaryErr = %v/%v, want true/nil", want, d.SummaryAttempted, d.SummaryErr)
		}
	}
}

func TestApplyDescendants_WalkLevelError_PartialResultStillWritten(t *testing.T) {
	tree := map[string]descendantTreeNode{
		"ROOT": {summary: "Q3 root", children: []string{"A", "B"}},
		"A":    {summary: "Q3 A"},
		"B":    {summary: "Q3 B"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reachedA := make(chan struct{})
	proceed := make(chan struct{})
	srv, calls := newDescendantTreeServer(t, tree, descendantServerOpts{
		onChildrenOfRequest: map[string]func(){
			"A": func() {
				close(reachedA)
				<-proceed
			},
		},
	})
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{SourceKey: "ROOT", HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4"}
	resCh := make(chan *DescendantMoveResult, 1)
	go func() { resCh <- ApplyDescendants(ctx, client, in) }()

	<-reachedA
	cancel()
	close(proceed)
	res := <-resCh

	if res.WalkErr == nil {
		t.Fatal("WalkErr = nil, want a non-nil whole-walk error after cancellation")
	}
	if len(res.ChildrenOfFailures) != 0 {
		t.Errorf("ChildrenOfFailures = %+v, want none (this is a whole-walk failure, not a per-node one)", res.ChildrenOfFailures)
	}

	if len(res.Descendants) != 2 {
		t.Fatalf("len(Descendants) = %d, want 2 (A, B discovered before cancellation)", len(res.Descendants))
	}
	for _, d := range res.Descendants {
		if !d.SummaryAttempted {
			t.Errorf("%s: SummaryAttempted = false, want true (a partial-result node's rewrite must still be attempted, not skipped, even though it's expected to fail here)", d.Key)
		}
		if d.SummaryErr == nil {
			t.Errorf("%s: SummaryErr = nil, want non-nil (the shared ctx was already canceled)", d.Key)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("write calls = %+v, want none (the canceled ctx must prevent the attempted writes from ever reaching the server)", *calls)
	}
}

func TestApplyDescendants_IndividualWriteFailure_OthersStillAttempted(t *testing.T) {
	const poison = "Bearer reflected-authorization secret-payload token=abc123"
	tree := map[string]descendantTreeNode{
		"ROOT": {summary: "Q3 root", children: []string{"A", "B"}},
		"A":    {summary: "Q3 A"},
		"B":    {summary: "Q3 B"},
	}
	srv, calls := newDescendantTreeServer(t, tree, descendantServerOpts{
		writeFailStatus: map[string]int{"A": http.StatusBadGateway},
		writeFailBody:   map[string]string{"A": poison},
	})
	defer srv.Close()
	client := moveClient(srv)

	in := MoveInput{SourceKey: "ROOT", HasSummary: true, SummaryOld: "Q3", SummaryNew: "Q4"}
	res := ApplyDescendants(context.Background(), client, in)

	byKey := map[string]DescendantWriteResult{}
	for _, d := range res.Descendants {
		byKey[d.Key] = d
	}
	a, b := byKey["A"], byKey["B"]
	if !a.SummaryAttempted || a.SummaryErr == nil {
		t.Errorf("A: SummaryAttempted/SummaryErr = %v/%v, want true/non-nil", a.SummaryAttempted, a.SummaryErr)
	}
	var statusErr *jira.StatusError
	if !errors.As(a.SummaryErr, &statusErr) || statusErr.StatusCode != http.StatusBadGateway {
		t.Errorf("A.SummaryErr = %v, want *jira.StatusError{StatusCode: 502}", a.SummaryErr)
	}
	if !b.SummaryAttempted || b.SummaryErr != nil {
		t.Errorf("B: SummaryAttempted/SummaryErr = %v/%v, want true/nil (A's failure must not block B's write)", b.SummaryAttempted, b.SummaryErr)
	}
	if len(*calls) != 2 {
		t.Errorf("write calls = %+v, want exactly 2 (both A and B attempted despite A's failure)", *calls)
	}
}

func TestApplyDescendants_NoRewriteRequested_NoWalkNoWrites(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := moveClient(srv)

	res := ApplyDescendants(context.Background(), client, MoveInput{SourceKey: "ROOT", HasParent: true, ParentKey: "PROJ-001"})

	if res.Attempted {
		t.Error("Attempted = true, want false for a --parent-only move")
	}
	if len(res.Descendants) != 0 || len(res.ChildrenOfFailures) != 0 || res.WalkErr != nil {
		t.Errorf("res = %+v, want a fully empty result", res)
	}
	if requests != 0 {
		t.Errorf("Jira requests made = %d, want 0 (a --parent-only move must not walk descendants at all)", requests)
	}
}
