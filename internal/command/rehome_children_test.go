package command

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func newRehomeChildrenPreflightServer(t *testing.T, issues map[string]moveTestIssue) (*httptest.Server, *[]string) {
	t.Helper()
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		calls = append(calls, r.Method+":"+key)
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected Jira write during rehome-children pre-flight: %s %s", r.Method, r.URL.Path)
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

func TestPreflightRehomeChildren_OrderedFailuresAndNoWrites(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent", labels: nil},
		"PROJ-200": {summary: "New parent", labels: nil},
	}
	cases := []struct {
		name      string
		in        RehomeChildrenInput
		wantCalls []string
		check     func(*testing.T, error)
	}{
		{
			name:      "old parent missing short-circuits before new parent is ever read",
			in:        RehomeChildrenInput{OldParentKey: "MISSING", NewParentKey: "PROJ-200"},
			wantCalls: []string{"GET:MISSING"},
			check: func(t *testing.T, err error) {

				var want *ErrRehomeOldParentNotFound
				if !errors.As(err, &want) || want.Subject != "rehome-children old parent" || want.Key != "MISSING" {
					t.Fatalf("error = %T (%v), want *IssueNotFoundError{Subject: rehome-children old parent, Key: MISSING}", err, err)
				}
			},
		},
		{
			name:      "new parent missing",
			in:        RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "MISSING"},
			wantCalls: []string{"GET:PROJ-100", "GET:MISSING"},
			check: func(t *testing.T, err error) {
				var want *ErrRehomeNewParentNotFound
				if !errors.As(err, &want) || want.Subject != "rehome-children new parent" || want.Key != "MISSING" {
					t.Fatalf("error = %T (%v), want *IssueNotFoundError{Subject: rehome-children new parent, Key: MISSING}", err, err)
				}
			},
		},
		{
			name: "summary pair incomplete: old only, checked after both parents are read",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				SummaryOldGiven: true, SummaryOld: "Q3",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrRehomeSummaryPairIncomplete) {
					t.Fatalf("error = %v, want ErrRehomeSummaryPairIncomplete", err)
				}
			},
		},
		{
			name: "summary pair incomplete: new only",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				SummaryNewGiven: true, SummaryNew: "Q4",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrRehomeSummaryPairIncomplete) {
					t.Fatalf("error = %v, want ErrRehomeSummaryPairIncomplete", err)
				}
			},
		},
		{
			name: "label pair incomplete: old only, checked after summary pair",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				LabelOldGiven: true, LabelOld: "FY26-Q3",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrRehomeLabelPairIncomplete) {
					t.Fatalf("error = %v, want ErrRehomeLabelPairIncomplete", err)
				}
			},
		},
		{
			name: "label pair incomplete: new only",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				LabelNewGiven: true, LabelNew: "FY26-Q4",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrRehomeLabelPairIncomplete) {
					t.Fatalf("error = %v, want ErrRehomeLabelPairIncomplete", err)
				}
			},
		},
		{
			name: "summary and label pairs both incomplete: summary check wins",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				SummaryOldGiven: true, SummaryOld: "Q3",
				LabelOldGiven: true, LabelOld: "FY26-Q3",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrRehomeSummaryPairIncomplete) {
					t.Fatalf("error = %v, want ErrRehomeSummaryPairIncomplete", err)
				}
			},
		},
		{
			name: "label pair incomplete and skip-status malformed: label check wins",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				LabelOldGiven:   true,
				LabelOld:        "FY26-Q3",
				SkipStatusGiven: true, SkipStatusRaw: "Done,,Blocked",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrRehomeLabelPairIncomplete) {
					t.Fatalf("error = %v, want ErrRehomeLabelPairIncomplete", err)
				}
			},
		},
		{
			name: "skip-status malformed: empty entry",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				SkipStatusGiven: true, SkipStatusRaw: "Done,,Blocked",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				var want *ErrRehomeSkipStatusInvalid
				if !errors.As(err, &want) {
					t.Fatalf("error = %T (%v), want *ErrRehomeSkipStatusInvalid", err, err)
				}
			},
		},
		{
			name: "skip-status malformed: whitespace-only entry",
			in: RehomeChildrenInput{
				OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
				SkipStatusGiven: true, SkipStatusRaw: "Done, ,Blocked",
			},
			wantCalls: []string{"GET:PROJ-100", "GET:PROJ-200"},
			check: func(t *testing.T, err error) {
				var want *ErrRehomeSkipStatusInvalid
				if !errors.As(err, &want) {
					t.Fatalf("error = %T (%v), want *ErrRehomeSkipStatusInvalid", err, err)
				}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := newRehomeChildrenPreflightServer(t, issues)
			defer srv.Close()
			_, err := PreflightRehomeChildren(context.Background(), moveClient(srv), tt.in)
			if err == nil {
				t.Fatal("PreflightRehomeChildren returned nil error")
			}
			tt.check(t, err)
			if !reflect.DeepEqual(*calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", *calls, tt.wantCalls)
			}
		})
	}
}

func TestPreflightRehomeChildren_ValidInputPassesThroughUntouched(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent", labels: nil},
		"PROJ-200": {summary: "New parent", labels: nil},
	}
	srv, calls := newRehomeChildrenPreflightServer(t, issues)
	defer srv.Close()

	in := RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SummaryOldGiven: true, SummaryOld: "Q3",
		SummaryNewGiven: true, SummaryNew: "Q4",
		LabelOldGiven: true, LabelOld: "FY26-Q3",
		LabelNewGiven: true, LabelNew: "FY26-Q4",
		SkipStatusGiven: true, SkipStatusRaw: " Done , Blocked ",
	}
	pf, err := PreflightRehomeChildren(context.Background(), moveClient(srv), in)
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	if pf.OldParent.Key != "PROJ-100" || pf.NewParent.Key != "PROJ-200" {
		t.Errorf("OldParent/NewParent = %v/%v, want PROJ-100/PROJ-200", pf.OldParent.Key, pf.NewParent.Key)
	}
	if !pf.HasSummary || pf.SummaryOld != "Q3" || pf.SummaryNew != "Q4" {
		t.Errorf("summary pair = %v/%q/%q, want true/Q3/Q4", pf.HasSummary, pf.SummaryOld, pf.SummaryNew)
	}
	if !pf.HasLabel || pf.LabelOld != "FY26-Q3" || pf.LabelNew != "FY26-Q4" {
		t.Errorf("label pair = %v/%q/%q, want true/FY26-Q3/FY26-Q4", pf.HasLabel, pf.LabelOld, pf.LabelNew)
	}
	if want := []string{"Done", "Blocked"}; !reflect.DeepEqual(pf.SkipStatus, want) {
		t.Errorf("SkipStatus = %v, want %v (split/trimmed)", pf.SkipStatus, want)
	}
	if want := []string{"GET:PROJ-100", "GET:PROJ-200"}; !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

func TestPreflightRehomeChildren_SkipsUnrequestedChecks(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent", labels: nil},
		"PROJ-200": {summary: "New parent", labels: nil},
	}
	srv, _ := newRehomeChildrenPreflightServer(t, issues)
	defer srv.Close()

	pf, err := PreflightRehomeChildren(context.Background(), moveClient(srv), RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	if pf.HasSummary || pf.HasLabel || pf.SkipStatus != nil {
		t.Errorf("pf = %+v, want no summary/label pair and nil SkipStatus", pf)
	}
}

func TestPreflightRehomeChildren_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("Bearer reflected-authorization secret-payload token=abc123"))
	}))
	defer srv.Close()

	_, err := PreflightRehomeChildren(context.Background(), moveClient(srv), RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
	})
	var unreachable *ErrRehomeChildrenPreflightUnreachable
	if !errors.As(err, &unreachable) {
		t.Fatalf("error = %T (%v), want *ErrRehomeChildrenPreflightUnreachable", err, err)
	}
	if unreachable.Operation != RehomeChildrenPreflightOldParent || unreachable.Key != "PROJ-100" {
		t.Errorf("Operation/Key = %v/%q, want RehomeChildrenPreflightOldParent/PROJ-100", unreachable.Operation, unreachable.Key)
	}

	if unreachable.Subject != "rehome-children" {
		t.Errorf("Subject = %q, want %q", unreachable.Subject, "rehome-children")
	}
	if got, want := unreachable.Error(), "could not reach Jira to validate rehome-children: "; !strings.HasPrefix(got, want) {
		t.Errorf("Error() = %q, want prefix %q", got, want)
	}
}

type rehomeChildrenWriteCall struct {
	key  string
	body map[string]any
}

func newRehomeChildrenWriteServer(t *testing.T, issues map[string]moveTestIssue, childrenOf map[string][]string, setParentFailStatus map[string]int) (*httptest.Server, *[]string, *[]rehomeChildrenWriteCall) {
	t.Helper()
	var jqlCalls []string
	var writeCalls []rehomeChildrenWriteCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search/jql":
			jql := r.URL.Query().Get("jql")
			m := descendantParentKeyPattern.FindStringSubmatch(jql)
			if m == nil {
				t.Fatalf("could not extract parent key from jql %q", jql)
			}
			key := m[1]
			jqlCalls = append(jqlCalls, key)
			var b strings.Builder
			b.WriteString(`{"issues":[`)
			for i, childKey := range childrenOf[key] {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"id":"1","key":"` + childKey + `","fields":{"summary":"","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`)
			}
			b.WriteString(`]}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(b.String()))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			issue, ok := issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":` + labelsJSON(issue.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))

		case r.Method == http.MethodPut:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writeCalls = append(writeCalls, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			if status, ok := setParentFailStatus[key]; ok {
				w.WriteHeader(status)
				return
			}
			w.WriteHeader(http.StatusOK)

		default:
			t.Fatalf("unexpected request during rehome-children execution: %s %s", r.Method, r.URL.Path)
		}
	}))
	return srv, &jqlCalls, &writeCalls
}

func TestApplyRehomeChildren_MultipleChildren_AllReparented(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]string{
		"PROJ-100": {"PROJ-101", "PROJ-102", "PROJ-103"},
	}
	srv, jqlCalls, writeCalls := newRehomeChildrenWriteServer(t, issues, childrenOf, nil)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}

	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if want := []string{"PROJ-101", "PROJ-102", "PROJ-103"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Errorf("ChildKeys = %v, want %v", res.ChildKeys, want)
	}
	if res.OldParentKey != "PROJ-100" || res.NewParentKey != "PROJ-200" {
		t.Errorf("OldParentKey/NewParentKey = %q/%q, want PROJ-100/PROJ-200", res.OldParentKey, res.NewParentKey)
	}

	wantWrites := []rehomeChildrenWriteCall{
		{key: "PROJ-101", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-102", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-103", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
	}
	if !reflect.DeepEqual(*writeCalls, wantWrites) {
		t.Errorf("write calls = %+v, want exactly %+v (no other item's parent may change)", *writeCalls, wantWrites)
	}
	if want := []string{"PROJ-100"}; !reflect.DeepEqual(*jqlCalls, want) {
		t.Errorf("ChildrenOf calls = %v, want %v (one level only, no descendant walk)", *jqlCalls, want)
	}
}

func TestApplyRehomeChildren_MultiPageChildrenOf_AllReparented(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	pages := [][]string{
		{"PROJ-101", "PROJ-102"},
		{"PROJ-103"},
	}
	var jqlTokens []string
	var writeCalls []rehomeChildrenWriteCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search/jql":
			jql := r.URL.Query().Get("jql")
			m := descendantParentKeyPattern.FindStringSubmatch(jql)
			if m == nil {
				t.Fatalf("could not extract parent key from jql %q", jql)
			}
			if m[1] != "PROJ-100" {
				t.Fatalf("unexpected ChildrenOf parent key %q, want PROJ-100", m[1])
			}
			token := r.URL.Query().Get("nextPageToken")
			jqlTokens = append(jqlTokens, token)
			pageIdx := 0
			if token == "page-2" {
				pageIdx = 1
			} else if token != "" {
				t.Fatalf("unexpected nextPageToken %q", token)
			}
			var b strings.Builder
			b.WriteString(`{"issues":[`)
			for i, childKey := range pages[pageIdx] {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"id":"1","key":"` + childKey + `","fields":{"summary":"","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`)
			}
			b.WriteString(`]`)
			if pageIdx == 0 {
				b.WriteString(`,"nextPageToken":"page-2"`)
			}
			b.WriteString(`}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(b.String()))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			issue, ok := issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":` + labelsJSON(issue.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))

		case r.Method == http.MethodPut:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writeCalls = append(writeCalls, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			w.WriteHeader(http.StatusOK)

		default:
			t.Fatalf("unexpected request during multi-page ChildrenOf execution: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}

	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if want := []string{"PROJ-101", "PROJ-102", "PROJ-103"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Errorf("ChildKeys = %v, want %v (every child from every page)", res.ChildKeys, want)
	}

	wantWrites := []rehomeChildrenWriteCall{
		{key: "PROJ-101", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-102", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-103", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
	}
	if !reflect.DeepEqual(writeCalls, wantWrites) {
		t.Errorf("write calls = %+v, want exactly %+v (every child from every page must be reparented, none skipped or duplicated)", writeCalls, wantWrites)
	}
	if want := []string{"", "page-2"}; !reflect.DeepEqual(jqlTokens, want) {
		t.Errorf("ChildrenOf page tokens = %v, want %v (one request per page)", jqlTokens, want)
	}
}

func TestApplyRehomeChildren_NoDirectChildren_NoOpSuccess(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	srv, jqlCalls, writeCalls := newRehomeChildrenWriteServer(t, issues, map[string][]string{}, nil)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}

	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v, want nil (zero direct children is a no-op success)", err)
	}
	if res.ChildKeys == nil || len(res.ChildKeys) != 0 {
		t.Errorf("ChildKeys = %v, want empty but non-nil", res.ChildKeys)
	}
	if len(*writeCalls) != 0 {
		t.Errorf("write calls = %+v, want none", *writeCalls)
	}
	if want := []string{"PROJ-100"}; !reflect.DeepEqual(*jqlCalls, want) {
		t.Errorf("ChildrenOf calls = %v, want %v", *jqlCalls, want)
	}
}

func TestApplyRehomeChildren_NoSummaryLabelOrDescendantWalk(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]string{
		"PROJ-100": {"PROJ-101", "PROJ-102"},

		"PROJ-101": {"PROJ-101-1"},
	}
	srv, jqlCalls, writeCalls := newRehomeChildrenWriteServer(t, issues, childrenOf, nil)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if want := []string{"PROJ-101", "PROJ-102"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Fatalf("ChildKeys = %v, want %v (only direct children, no grandchild)", res.ChildKeys, want)
	}
	if want := []string{"PROJ-100"}; !reflect.DeepEqual(*jqlCalls, want) {
		t.Errorf("ChildrenOf calls = %v, want %v (one level only -- no descendant walk)", *jqlCalls, want)
	}
	for _, c := range *writeCalls {
		if _, has := c.body["summary"]; has {
			t.Errorf("write call %+v unexpectedly carried a summary field", c)
		}
		if _, has := c.body["labels"]; has {
			t.Errorf("write call %+v unexpectedly carried a labels field", c)
		}
	}
}

func TestApplyRehomeChildren_ChildrenOfFails(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("secret-body"))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			issue, ok := issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
		default:
			t.Fatalf("unexpected request during ChildrenOf-failure test: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	_, err = ApplyRehomeChildren(context.Background(), client, pf)
	var listFailed *ErrRehomeChildrenListFailed
	if !errors.As(err, &listFailed) {
		t.Fatalf("error = %T (%v), want *ErrRehomeChildrenListFailed", err, err)
	}
	if listFailed.OldParentKey != "PROJ-100" {
		t.Errorf("OldParentKey = %q, want PROJ-100", listFailed.OldParentKey)
	}
}

func newRehomeChildrenDeletedParentServer(t *testing.T, oldParentKey, newParentKey string, childrenOfStatus int, childrenOfBody string) *httptest.Server {
	t.Helper()
	oldParentReads := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			if childrenOfStatus != 0 {
				w.WriteHeader(childrenOfStatus)
				_, _ = w.Write([]byte(childrenOfBody))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issues":[]}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			if key == oldParentKey {
				oldParentReads++
				if oldParentReads > 1 {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":"Old parent","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
				return
			}
			if key == newParentKey {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":"New parent","labels":[],"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request during deleted-old-parent test: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestApplyRehomeChildren_NoDirectChildren_OldParentDeleted(t *testing.T) {
	srv := newRehomeChildrenDeletedParentServer(t, "PROJ-100", "PROJ-200", 0, "")
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	var deleted *ErrRehomeOldParentDeleted
	if !errors.As(err, &deleted) {
		t.Fatalf("error = %T (%v), res = %+v, want *ErrRehomeOldParentDeleted", err, err, res)
	}
	if deleted.Key != "PROJ-100" {
		t.Errorf("Key = %q, want PROJ-100", deleted.Key)
	}
}

func TestApplyRehomeChildren_ChildrenOfFails_OldParentDeleted(t *testing.T) {
	srv := newRehomeChildrenDeletedParentServer(t, "PROJ-100", "PROJ-200", http.StatusBadGateway, "secret-body")
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	var deleted *ErrRehomeOldParentDeleted
	if !errors.As(err, &deleted) {
		t.Fatalf("error = %T (%v), res = %+v, want *ErrRehomeOldParentDeleted", err, err, res)
	}
	if deleted.Key != "PROJ-100" {
		t.Errorf("Key = %q, want PROJ-100", deleted.Key)
	}
}

func TestApplyRehomeChildren_SetParentFails_ContinuesRemaining(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]string{
		"PROJ-100": {"PROJ-101", "PROJ-102", "PROJ-103"},
	}
	srv, _, writeCalls := newRehomeChildrenWriteServer(t, issues, childrenOf, map[string]int{"PROJ-102": http.StatusBadGateway})
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v, want nil (a per-child SetParent failure must not abort the run)", err)
	}

	if want := []string{"PROJ-101", "PROJ-103"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Errorf("ChildKeys = %v, want %v (PROJ-103 must still be reparented despite PROJ-102's failure)", res.ChildKeys, want)
	}
	if len(res.Failed) != 1 || res.Failed[0].Key != "PROJ-102" || res.Failed[0].Err == nil {
		t.Fatalf("Failed = %+v, want exactly one entry for PROJ-102 with a non-nil Err", res.Failed)
	}

	wantKeys := []string{"PROJ-101", "PROJ-102", "PROJ-103"}
	if len(*writeCalls) != len(wantKeys) {
		t.Fatalf("write calls = %+v, want exactly one per key in %v (every direct child must still be attempted)", *writeCalls, wantKeys)
	}
	for i, want := range wantKeys {
		if (*writeCalls)[i].key != want {
			t.Errorf("write calls = %+v, want keys %v in order", *writeCalls, wantKeys)
		}
	}
}

func TestApplyRehomeChildren_ChildDescendantWalkFails_OtherChildrenContinue(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	tree := map[string]descendantTreeNode{
		"PROJ-100": {children: []string{"PROJ-101", "PROJ-102"}},

		"PROJ-101":  {summary: "Q3 child A"},
		"PROJ-102":  {summary: "Q3 child B", children: []string{"PROJ-102A"}},
		"PROJ-102A": {summary: "Q3 grandchild B1"},
	}
	var jqlCalls []string
	var writeCalls []rehomeChildrenWriteCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search/jql":
			jql := r.URL.Query().Get("jql")
			m := descendantParentKeyPattern.FindStringSubmatch(jql)
			if m == nil {
				t.Fatalf("could not extract parent key from jql %q", jql)
			}
			key := m[1]
			jqlCalls = append(jqlCalls, key)
			if key == "PROJ-101" {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("secret-body"))
				return
			}
			node := tree[key]
			var b strings.Builder
			b.WriteString(`{"issues":[`)
			for i, childKey := range node.children {
				child := tree[childKey]
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"id":"1","key":"` + childKey + `","fields":{"summary":` + quoteJSON(child.summary) + `,"labels":` + labelsJSON(child.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`)
			}
			b.WriteString(`]}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(b.String()))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			issue, ok := parents[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":` + labelsJSON(issue.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))

		case r.Method == http.MethodPut:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writeCalls = append(writeCalls, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			w.WriteHeader(http.StatusOK)

		default:
			t.Fatalf("unexpected request during descendant-walk-failure test: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SummaryOldGiven: true, SummaryOld: "Q3",
		SummaryNewGiven: true, SummaryNew: "Q4",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v, want nil (one child's descendant-walk failure must not abort the run)", err)
	}

	if want := []string{"PROJ-101", "PROJ-102"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Fatalf("ChildKeys = %v, want %v (both children must still be reparented)", res.ChildKeys, want)
	}
	if len(res.Rewrites) != 2 {
		t.Fatalf("len(Rewrites) = %d, want 2 (one pass per rehomed child, even for the one whose walk failed)", len(res.Rewrites))
	}
	byChild := map[string]ChildRewritePass{}
	for _, pass := range res.Rewrites {
		byChild[pass.ChildKey] = pass
	}

	p101 := byChild["PROJ-101"]
	if len(p101.ChildrenOfFailures) != 1 || p101.ChildrenOfFailures[0].Key != "PROJ-101" || p101.ChildrenOfFailures[0].Depth != 0 {
		t.Errorf("PROJ-101.ChildrenOfFailures = %+v, want one entry for PROJ-101 itself at depth 0", p101.ChildrenOfFailures)
	}
	if len(p101.Items) != 1 {
		t.Errorf("len(PROJ-101.Items) = %d, want 1 (itself only -- its subtree went unexplored)", len(p101.Items))
	}

	p102 := byChild["PROJ-102"]
	if len(p102.ChildrenOfFailures) != 0 {
		t.Errorf("PROJ-102.ChildrenOfFailures = %+v, want none (its own walk must be unaffected by PROJ-101's failure)", p102.ChildrenOfFailures)
	}
	if len(p102.Items) != 2 {
		t.Fatalf("len(PROJ-102.Items) = %d, want 2 (itself + PROJ-102A)", len(p102.Items))
	}
	if !p102.Items[1].SummaryAttempted || p102.Items[1].SummaryErr != nil {
		t.Errorf("PROJ-102A rewrite = %+v, want SummaryAttempted=true SummaryErr=nil", p102.Items[1])
	}

	wantWrites := []string{"PROJ-101", "PROJ-101", "PROJ-102", "PROJ-102", "PROJ-102A"}
	gotWrites := make([]string, len(writeCalls))
	for i, c := range writeCalls {
		gotWrites[i] = c.key
	}
	if !reflect.DeepEqual(gotWrites, wantWrites) {
		t.Fatalf("write call keys = %v, want %v", gotWrites, wantWrites)
	}
}

func newRehomeChildrenRewriteServer(t *testing.T, parents map[string]moveTestIssue, tree map[string]descendantTreeNode) (*httptest.Server, *[]string, *[]rehomeChildrenWriteCall) {
	t.Helper()
	var jqlCalls []string
	var writeCalls []rehomeChildrenWriteCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search/jql":
			jql := r.URL.Query().Get("jql")
			m := descendantParentKeyPattern.FindStringSubmatch(jql)
			if m == nil {
				t.Fatalf("could not extract parent key from jql %q", jql)
			}
			key := m[1]
			jqlCalls = append(jqlCalls, key)
			node := tree[key]
			var b strings.Builder
			b.WriteString(`{"issues":[`)
			for i, childKey := range node.children {
				child := tree[childKey]
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"id":"1","key":"` + childKey + `","fields":{"summary":` + quoteJSON(child.summary) + `,"labels":` + labelsJSON(child.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`)
			}
			b.WriteString(`]}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(b.String()))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			issue, ok := parents[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":` + labelsJSON(issue.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))

		case r.Method == http.MethodPut:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writeCalls = append(writeCalls, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			w.WriteHeader(http.StatusOK)

		default:
			t.Fatalf("unexpected request during rehome-children rewrite-pass execution: %s %s", r.Method, r.URL.Path)
		}
	}))
	return srv, &jqlCalls, &writeCalls
}

func TestApplyRehomeChildren_DescendantRewrite_MultiChildCoverage(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	tree := map[string]descendantTreeNode{
		"PROJ-100":  {children: []string{"PROJ-101", "PROJ-102", "PROJ-103"}},
		"PROJ-101":  {summary: "Q3 child A", labels: []string{"FY26-Q3"}, children: []string{"PROJ-101A"}},
		"PROJ-101A": {summary: "Q3 grandchild A1", labels: []string{"FY26-Q3"}, children: nil},
		"PROJ-102":  {summary: "Q3 child B", labels: []string{"other-label"}, children: nil},
		"PROJ-103":  {summary: "no match here", labels: []string{"other-label"}, children: nil},
	}
	srv, jqlCalls, writeCalls := newRehomeChildrenRewriteServer(t, parents, tree)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SummaryOldGiven: true, SummaryOld: "Q3",
		SummaryNewGiven: true, SummaryNew: "Q4",
		LabelOldGiven: true, LabelOld: "FY26-Q3",
		LabelNewGiven: true, LabelNew: "FY26-Q4",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}

	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}

	if !res.HasSummary || !res.HasLabel {
		t.Fatalf("HasSummary/HasLabel = %v/%v, want true/true", res.HasSummary, res.HasLabel)
	}
	if want := []string{"PROJ-101", "PROJ-102", "PROJ-103"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Fatalf("ChildKeys = %v, want %v", res.ChildKeys, want)
	}
	if len(res.Rewrites) != 3 {
		t.Fatalf("len(Rewrites) = %d, want 3 (one per rehomed child)", len(res.Rewrites))
	}
	byChild := map[string]ChildRewritePass{}
	for _, pass := range res.Rewrites {
		byChild[pass.ChildKey] = pass
	}

	p101 := byChild["PROJ-101"]
	if p101.WalkErr != nil || len(p101.ChildrenOfFailures) != 0 {
		t.Fatalf("PROJ-101 WalkErr/ChildrenOfFailures = %v/%v, want nil/none", p101.WalkErr, p101.ChildrenOfFailures)
	}
	if len(p101.Items) != 2 {
		t.Fatalf("len(PROJ-101.Items) = %d, want 2 (itself + PROJ-101A)", len(p101.Items))
	}
	self101, desc101A := p101.Items[0], p101.Items[1]
	if self101.Key != "PROJ-101" || self101.ParentKey != "PROJ-200" || self101.Depth != 0 {
		t.Errorf("PROJ-101 self = %+v, want Key=PROJ-101 ParentKey=PROJ-200 Depth=0", self101)
	}
	if !self101.SummaryAttempted || self101.NewSummary != "Q4 child A" {
		t.Errorf("PROJ-101 self summary = attempted=%v new=%q, want true/%q", self101.SummaryAttempted, self101.NewSummary, "Q4 child A")
	}
	if !self101.LabelsAttempted || !reflect.DeepEqual(self101.Labels, []string{"FY26-Q4"}) {
		t.Errorf("PROJ-101 self labels = attempted=%v labels=%v, want true/[FY26-Q4]", self101.LabelsAttempted, self101.Labels)
	}
	if desc101A.Key != "PROJ-101A" || desc101A.ParentKey != "PROJ-101" || desc101A.Depth != 1 {
		t.Errorf("PROJ-101A = %+v, want Key=PROJ-101A ParentKey=PROJ-101 Depth=1 (matches at depth)", desc101A)
	}
	if !desc101A.SummaryAttempted || desc101A.NewSummary != "Q4 grandchild A1" {
		t.Errorf("PROJ-101A summary = attempted=%v new=%q, want true/%q", desc101A.SummaryAttempted, desc101A.NewSummary, "Q4 grandchild A1")
	}
	if !desc101A.LabelsAttempted || !reflect.DeepEqual(desc101A.Labels, []string{"FY26-Q4"}) {
		t.Errorf("PROJ-101A labels = attempted=%v labels=%v, want true/[FY26-Q4]", desc101A.LabelsAttempted, desc101A.Labels)
	}

	p102 := byChild["PROJ-102"]
	if len(p102.Items) != 1 {
		t.Fatalf("len(PROJ-102.Items) = %d, want 1 (child with no descendants)", len(p102.Items))
	}
	self102 := p102.Items[0]
	if !self102.SummaryAttempted || self102.NewSummary != "Q4 child B" {
		t.Errorf("PROJ-102 summary = attempted=%v new=%q, want true/%q", self102.SummaryAttempted, self102.NewSummary, "Q4 child B")
	}
	if self102.LabelsAttempted {
		t.Errorf("PROJ-102 LabelsAttempted = true, want false (label doesn't match; left alone)")
	}

	p103 := byChild["PROJ-103"]
	if len(p103.Items) != 1 {
		t.Fatalf("len(PROJ-103.Items) = %d, want 1 (child with no descendants)", len(p103.Items))
	}
	self103 := p103.Items[0]
	if self103.SummaryAttempted || self103.LabelsAttempted {
		t.Errorf("PROJ-103 = %+v, want SummaryAttempted=false LabelsAttempted=false (no match)", self103)
	}

	wantJQL := []string{"PROJ-100", "PROJ-101", "PROJ-101A", "PROJ-102", "PROJ-103"}
	if !reflect.DeepEqual(*jqlCalls, wantJQL) {
		t.Errorf("ChildrenOf calls = %v, want %v (one old-parent listing, then one walk-root call per rehomed child, then its own descendants)", *jqlCalls, wantJQL)
	}

	wantWrites := []rehomeChildrenWriteCall{
		{key: "PROJ-101", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-101", body: map[string]any{"summary": "Q4 child A"}},
		{key: "PROJ-101", body: map[string]any{"labels": []any{"FY26-Q4"}}},
		{key: "PROJ-101A", body: map[string]any{"summary": "Q4 grandchild A1"}},
		{key: "PROJ-101A", body: map[string]any{"labels": []any{"FY26-Q4"}}},
		{key: "PROJ-102", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-102", body: map[string]any{"summary": "Q4 child B"}},
		{key: "PROJ-103", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
	}
	if !reflect.DeepEqual(*writeCalls, wantWrites) {
		t.Fatalf("write calls = %+v, want exactly %+v", *writeCalls, wantWrites)
	}

	for _, c := range *writeCalls {
		if _, has := c.body["parent"]; has && c.key != "PROJ-101" && c.key != "PROJ-102" && c.key != "PROJ-103" {
			t.Errorf("write call %+v set a parent field on a non-rehomed-child key -- no descendant's parent may ever change", c)
		}
	}
}

func TestApplyRehomeChildren_NoRewriteRequested_NoWalk(t *testing.T) {
	issues := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]string{
		"PROJ-100": {"PROJ-101"},
	}
	srv, jqlCalls, _ := newRehomeChildrenWriteServer(t, issues, childrenOf, nil)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{OldParentKey: "PROJ-100", NewParentKey: "PROJ-200"})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if res.HasSummary || res.HasLabel {
		t.Errorf("HasSummary/HasLabel = %v/%v, want false/false", res.HasSummary, res.HasLabel)
	}
	if len(res.Rewrites) != 0 {
		t.Errorf("Rewrites = %+v, want none", res.Rewrites)
	}
	if want := []string{"PROJ-100"}; !reflect.DeepEqual(*jqlCalls, want) {
		t.Errorf("ChildrenOf calls = %v, want %v (no descendant walk without rewrite flags)", *jqlCalls, want)
	}
}

type rehomeChildrenStatusChild struct {
	key     string
	status  string
	summary string
	labels  []string
}

func newRehomeChildrenSkipStatusServer(t *testing.T, parents map[string]moveTestIssue, childrenOf map[string][]rehomeChildrenStatusChild) (*httptest.Server, *[]string, *[]rehomeChildrenWriteCall) {
	t.Helper()
	var jqlCalls []string
	var writeCalls []rehomeChildrenWriteCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search/jql":
			jql := r.URL.Query().Get("jql")
			m := descendantParentKeyPattern.FindStringSubmatch(jql)
			if m == nil {
				t.Fatalf("could not extract parent key from jql %q", jql)
			}
			key := m[1]
			jqlCalls = append(jqlCalls, key)
			var b strings.Builder
			b.WriteString(`{"issues":[`)
			for i, child := range childrenOf[key] {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"id":"1","key":"` + child.key + `","fields":{"summary":` + quoteJSON(child.summary) + `,"labels":` + labelsJSON(child.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"},"status":{"id":"1","name":` + quoteJSON(child.status) + `}}}`)
			}
			b.WriteString(`]}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(b.String()))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			issue, ok := parents[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{"summary":` + quoteJSON(issue.summary) + `,"labels":` + labelsJSON(issue.labels) + `,"issuetype":{"id":"1","name":"Task"},"project":{"id":"1","key":"PROJ"}}}`))

		case r.Method == http.MethodPut:
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			var decoded struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			writeCalls = append(writeCalls, rehomeChildrenWriteCall{key: key, body: decoded.Fields})
			w.WriteHeader(http.StatusOK)

		default:
			t.Fatalf("unexpected request during rehome-children skip-status execution: %s %s", r.Method, r.URL.Path)
		}
	}))
	return srv, &jqlCalls, &writeCalls
}

func TestApplyRehomeChildren_SkipStatus_SkipsMatchingLeavesSiblingNormal(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {
			{key: "PROJ-101", status: "Done", summary: "Q3 done child"},
			{key: "PROJ-102", status: "In Progress", summary: "Q3 active child"},
		},
	}
	srv, jqlCalls, writeCalls := newRehomeChildrenSkipStatusServer(t, parents, childrenOf)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SummaryOldGiven: true, SummaryOld: "Q3",
		SummaryNewGiven: true, SummaryNew: "Q4",
		SkipStatusGiven: true, SkipStatusRaw: "Done",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}

	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}

	if want := []string{"PROJ-102"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Errorf("ChildKeys = %v, want %v", res.ChildKeys, want)
	}
	if want := []SkippedChild{{Key: "PROJ-101", StatusName: "Done"}}; !reflect.DeepEqual(res.Skipped, want) {
		t.Errorf("Skipped = %+v, want %+v", res.Skipped, want)
	}
	if len(res.Rewrites) != 1 || res.Rewrites[0].ChildKey != "PROJ-102" {
		t.Fatalf("Rewrites = %+v, want exactly one pass, for PROJ-102 only (a skipped child gets no rewrite pass)", res.Rewrites)
	}

	if want := []string{"PROJ-100", "PROJ-102"}; !reflect.DeepEqual(*jqlCalls, want) {
		t.Errorf("ChildrenOf calls = %v, want %v (old-parent listing, then only the non-skipped sibling's own descendant walk -- PROJ-101 must never appear)", *jqlCalls, want)
	}

	wantWrites := []rehomeChildrenWriteCall{
		{key: "PROJ-102", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}},
		{key: "PROJ-102", body: map[string]any{"summary": "Q4 active child"}},
	}
	if !reflect.DeepEqual(*writeCalls, wantWrites) {
		t.Fatalf("write calls = %+v, want exactly %+v (PROJ-101 must receive zero writes of any kind)", *writeCalls, wantWrites)
	}
}

func TestApplyRehomeChildren_SkipStatus_CaseInsensitive(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {{key: "PROJ-101", status: "Done"}},
	}
	srv, _, writeCalls := newRehomeChildrenSkipStatusServer(t, parents, childrenOf)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SkipStatusGiven: true, SkipStatusRaw: "done",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if want := []SkippedChild{{Key: "PROJ-101", StatusName: "Done"}}; !reflect.DeepEqual(res.Skipped, want) {
		t.Errorf("Skipped = %+v, want %+v (case-insensitive match, status name as Jira reported it -- not the --skip-status entry)", res.Skipped, want)
	}
	if len(res.ChildKeys) != 0 {
		t.Errorf("ChildKeys = %v, want none", res.ChildKeys)
	}
	if len(*writeCalls) != 0 {
		t.Errorf("write calls = %+v, want none", *writeCalls)
	}
}

func TestApplyRehomeChildren_SkipStatus_WholeStringNotSubstring(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {{key: "PROJ-101", status: "Not Done"}},
	}
	srv, _, writeCalls := newRehomeChildrenSkipStatusServer(t, parents, childrenOf)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SkipStatusGiven: true, SkipStatusRaw: "Done",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if want := []string{"PROJ-101"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Errorf(`ChildKeys = %v, want %v ("Done" must not match "Not Done" via substring)`, res.ChildKeys, want)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("Skipped = %+v, want none", res.Skipped)
	}
	want := []rehomeChildrenWriteCall{{key: "PROJ-101", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}}}
	if !reflect.DeepEqual(*writeCalls, want) {
		t.Errorf("write calls = %+v, want %+v", *writeCalls, want)
	}
}

func TestApplyRehomeChildren_SkipStatus_AbsentStatusProcessedNormally(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {{key: "PROJ-101", status: ""}},
	}
	srv, _, writeCalls := newRehomeChildrenSkipStatusServer(t, parents, childrenOf)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SkipStatusGiven: true, SkipStatusRaw: "Done",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if want := []string{"PROJ-101"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Errorf("ChildKeys = %v, want %v (absent/empty status matches nothing)", res.ChildKeys, want)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("Skipped = %+v, want none", res.Skipped)
	}
	want := []rehomeChildrenWriteCall{{key: "PROJ-101", body: map[string]any{"parent": map[string]any{"key": "PROJ-200"}}}}
	if !reflect.DeepEqual(*writeCalls, want) {
		t.Errorf("write calls = %+v, want %+v", *writeCalls, want)
	}
}

func TestApplyRehomeChildren_SkipStatus_NoMatch_AllProcessed(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {
			{key: "PROJ-101", status: "To Do"},
			{key: "PROJ-102", status: "In Progress"},
		},
	}
	srv, _, writeCalls := newRehomeChildrenSkipStatusServer(t, parents, childrenOf)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SkipStatusGiven: true, SkipStatusRaw: "Blocked",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}
	if want := []string{"PROJ-101", "PROJ-102"}; !reflect.DeepEqual(res.ChildKeys, want) {
		t.Errorf("ChildKeys = %v, want %v (a skip-status list matching nothing skips nobody)", res.ChildKeys, want)
	}
	if res.Skipped == nil || len(res.Skipped) != 0 {
		t.Errorf("Skipped = %+v, want empty but non-nil", res.Skipped)
	}
	if len(*writeCalls) != 2 {
		t.Errorf("write calls = %+v, want 2 (both children reparented)", *writeCalls)
	}
}

func TestApplyRehomeChildren_SkipStatus_AllSkipped_NoWalkNoWrites(t *testing.T) {
	parents := map[string]moveTestIssue{
		"PROJ-100": {summary: "Old parent"},
		"PROJ-200": {summary: "New parent"},
	}
	childrenOf := map[string][]rehomeChildrenStatusChild{
		"PROJ-100": {
			{key: "PROJ-101", status: "Done"},
			{key: "PROJ-102", status: "Blocked"},
		},
	}
	srv, jqlCalls, writeCalls := newRehomeChildrenSkipStatusServer(t, parents, childrenOf)
	defer srv.Close()
	client := moveClient(srv)

	pf, err := PreflightRehomeChildren(context.Background(), client, RehomeChildrenInput{
		OldParentKey: "PROJ-100", NewParentKey: "PROJ-200",
		SkipStatusGiven: true, SkipStatusRaw: "Done,Blocked",
	})
	if err != nil {
		t.Fatalf("PreflightRehomeChildren: %v", err)
	}
	res, err := ApplyRehomeChildren(context.Background(), client, pf)
	if err != nil {
		t.Fatalf("ApplyRehomeChildren: %v", err)
	}

	if res.ChildKeys == nil || len(res.ChildKeys) != 0 {
		t.Errorf("ChildKeys = %v, want empty but non-nil", res.ChildKeys)
	}
	want := []SkippedChild{
		{Key: "PROJ-101", StatusName: "Done"},
		{Key: "PROJ-102", StatusName: "Blocked"},
	}
	if !reflect.DeepEqual(res.Skipped, want) {
		t.Errorf("Skipped = %+v, want %+v", res.Skipped, want)
	}
	if want := []string{"PROJ-100"}; !reflect.DeepEqual(*jqlCalls, want) {
		t.Errorf("ChildrenOf calls = %v, want %v (only the old-parent listing -- no skipped child's own descendant walk may run)", *jqlCalls, want)
	}
	if len(*writeCalls) != 0 {
		t.Errorf("write calls = %+v, want none", *writeCalls)
	}
}

func TestMatchesSkipStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusName string
		skipStatus []string
		want       bool
	}{
		{"exact match", "Done", []string{"Done"}, true},
		{"case-insensitive, lowercase status", "done", []string{"Done"}, true},
		{"case-insensitive, uppercase status", "DONE", []string{"done"}, true},
		{"whole-string, not substring", "Not Done", []string{"Done"}, false},
		{"status name trimmed", "  Done  ", []string{"Done"}, true},
		{"empty status matches nothing", "", []string{"Done"}, false},
		{"whitespace-only status matches nothing", "   ", []string{"Done"}, false},
		{"nil skip list", "Done", nil, false},
		{"multiple entries, one matches", "Blocked", []string{"Done", "Blocked"}, true},
		{"multiple entries, none match", "In Review", []string{"Done", "Blocked"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesSkipStatus(tt.statusName, tt.skipStatus); got != tt.want {
				t.Errorf("matchesSkipStatus(%q, %v) = %v, want %v", tt.statusName, tt.skipStatus, got, tt.want)
			}
		})
	}
}

func TestSplitSkipStatus(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "single", raw: "Done", want: []string{"Done"}},
		{name: "multiple trimmed", raw: "Done, Blocked ,In Review", want: []string{"Done", "Blocked", "In Review"}},
		{name: "empty entry", raw: "Done,,Blocked", wantErr: true},
		{name: "whitespace-only entry", raw: "Done, ,Blocked", wantErr: true},
		{name: "non-ASCII whitespace-only entry", raw: "Done, ,Blocked", wantErr: true},
		{name: "leading comma", raw: ",Done", wantErr: true},
		{name: "trailing comma", raw: "Done,", wantErr: true},
		{name: "empty string", raw: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitSkipStatus(tt.raw)
			if tt.wantErr {
				var want *ErrRehomeSkipStatusInvalid
				if !errors.As(err, &want) {
					t.Fatalf("err = %v, want *ErrRehomeSkipStatusInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitSkipStatus(%q): unexpected error: %v", tt.raw, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitSkipStatus(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
