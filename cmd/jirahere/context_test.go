package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	contextoutput "github.com/aslanbrooke/jirahere/internal/context"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

func TestParseContextGetOptions_DefaultsAndFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantKey    string
		wantLevel  int
		wantFormat string
	}{
		{"defaults", []string{"PROJ-123"}, "PROJ-123", 0, "md"},
		{"json level three", []string{"PROJ-123", "--level", "3", "--json"}, "PROJ-123", 3, "json"},
		{"flags before id", []string{"--level=1", "--md", "PROJ-123"}, "PROJ-123", 1, "md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, got, err := parseContextGetOptions(tt.args)
			if err != nil {
				t.Fatalf("parseContextGetOptions(%v): %v", tt.args, err)
			}
			if key != tt.wantKey || got.level != tt.wantLevel || got.format != tt.wantFormat {
				t.Errorf("got key=%q options=%+v, want key=%q level=%d format=%q", key, got, tt.wantKey, tt.wantLevel, tt.wantFormat)
			}
		})
	}
}

func TestRunContextGet_LocalPreflightOrder(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"id before level", []string{"bad", "--level", "9", "--json", "--md"}, "<id> must be a well-formed Jira issue key"},
		{"id before malformed level flag", []string{"bad", "--level"}, "<id> must be a well-formed Jira issue key"},
		{"id before unknown flag and value", []string{"bad", "--unknown", "value"}, "<id> must be a well-formed Jira issue key"},
		{"level before format", []string{"PROJ-123", "--level", "nine", "--json", "--md"}, "--level must be one of 0, 1, 2, or 3"},
		{"format after valid level", []string{"PROJ-123", "--level", "2", "--json", "--md"}, "--json and --md may not be used together"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runContextGet(tt.args) })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
		})
	}
}

func TestRunContextGet_ArgumentAndUnknownFlagFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{"--json"}, "usage: jirahere context get"},
		{"surplus positional", []string{"PROJ-123", "extra"}, "usage: jirahere context get"},
		{"flag value", []string{"PROJ-123", "--json", "value"}, "usage: jirahere context get"},
		{"unknown flag", []string{"PROJ-123", "--unknown", "value"}, "flag provided but not defined: --unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			stderr := captureStderr(t, func() { err = runContextGet(tt.args) })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one body-free error line", stderr)
			}
		})
	}
}

func TestRunContextGet_EmptyIDDoesNotReachAuthOrGetIssue(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	contextGetTestLogin(t)

	requests := 0
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("GetIssue must not be reached")
	})

	var err error
	stderr := captureStderr(t, func() { err = runContextGet([]string{""}) })
	if err == nil || !strings.Contains(err.Error(), "<id> must be a well-formed Jira issue key") {
		t.Fatalf("err = %v, want malformed id failure", err)
	}
	if requests != 0 {
		t.Errorf("GetIssue requests = %d, want 0", requests)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one body-free error line", stderr)
	}
}

func TestRunContextGet_AllLevelsAssembleNoPlaceholder(t *testing.T) {

	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	contextGetTestLogin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123","fields":{"summary":"Context target"}}`))
	}))
	defer srv.Close()
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}

	for _, level := range []string{"0", "1", "2", "3"} {
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", level, "--json"}) })
		if err != nil {
			t.Fatalf("level %s: runContextGet returned %v, want success", level, err)
		}
		if strings.Contains(out, "isn't implemented yet") {
			t.Errorf("level %s: output still contains the L0010 placeholder: %q", level, out)
		}
	}
}

func TestRunContextGet_GetIssueFailureIsBodyFree(t *testing.T) {
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	contextGetTestLogin(t)
	const secret = "response-body-secret-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(secret))
	}))
	defer srv.Close()
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}

	var err error
	stderr := captureStderr(t, func() { err = runContextGet([]string{"PROJ-123"}) })
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(stderr, secret) {
		t.Fatalf("context GetIssue failure leaked response body: err=%v stderr=%q", err, stderr)
	}
	if !strings.Contains(stderr, "Jira returned an unexpected response (500)") {
		t.Errorf("stderr = %q, want safe status", stderr)
	}
}

func contextGetIssueServer(t *testing.T, key, fieldsJSON string) {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":` + fieldsJSON + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
}

func TestRunContextGet_Level0_JSON(t *testing.T) {
	tests := []struct {
		name   string
		fields string
		want   string
	}{
		{
			name: "labels and description populated",
			fields: `{"summary":"Ship the widget","labels":["urgent","fy26-q3"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Widget details here."}]}]}}`,
			want: `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [
      "urgent",
      "fy26-q3"
    ],
    "description": "Widget details here.",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{
			name: "no labels",
			fields: `{"summary":"Ship the widget","labels":[],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Widget details here."}]}]}}`,
			want: `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [],
    "description": "Widget details here.",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{
			name: "empty description",
			fields: `{"summary":"Ship the widget","labels":["urgent"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"}}`,
			want: `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [
      "urgent"
    ],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{

			name: "labels key absent",
			fields: `{"summary":"Ship the widget",
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"}}`,
			want: `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{
			name: "labels explicit null",
			fields: `{"summary":"Ship the widget","labels":null,
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"}}`,
			want: `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{

			name: "description ADF has no paragraphs",
			fields: `{"summary":"Ship the widget","labels":["urgent"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title only"}]}]}}`,
			want: `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [
      "urgent"
    ],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{

			name: "description extracts to whitespace only",
			fields: `{"summary":"Ship the widget","labels":["urgent"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":" "}]}]}}`,
			want: `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [
      "urgent"
    ],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextGetIssueServer(t, "PROJ-123", tt.fields)
			var err error
			out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--json"}) })
			if err != nil {
				t.Fatalf("runContextGet: %v", err)
			}
			if strings.TrimRight(out, "\n") != tt.want {
				t.Errorf("stdout = %s, want %s", out, tt.want)
			}

			var parsed map[string]any
			if err := json.Unmarshal([]byte(out), &parsed); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			if _, ok := parsed["parent"]; !ok {
				t.Error(`"parent" key missing from output, want present with explicit null`)
			}
			if parsed["parent"] != nil {
				t.Errorf(`parent = %#v, want explicit null`, parsed["parent"])
			}
			if _, ok := parsed["children"]; !ok {
				t.Error(`"children" key missing from output, want present with explicit null`)
			}
			if parsed["children"] != nil {
				t.Errorf(`children = %#v, want explicit null`, parsed["children"])
			}
			if _, ok := parsed["ancestors"]; !ok {
				t.Error(`"ancestors" key missing from output, want present with explicit null`)
			}
			if parsed["ancestors"] != nil {
				t.Errorf(`ancestors = %#v, want explicit null below level 2`, parsed["ancestors"])
			}
		})
	}
}

func TestRunContextGet_Level0_MD(t *testing.T) {
	tests := []struct {
		name   string
		fields string
		want   string
	}{
		{
			name: "labels and description populated",
			fields: `{"summary":"Ship the widget","labels":["urgent","fy26-q3"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Widget details here."}]}]}}`,
			want: "# PROJ-123 — Ship the widget (Task, In Progress)\n\nAssignee: (unassigned)\n\nLabels: urgent, fy26-q3\n\n## Description\n\nWidget details here.",
		},
		{
			name: "no labels",
			fields: `{"summary":"Ship the widget","labels":[],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Widget details here."}]}]}}`,
			want: "# PROJ-123 — Ship the widget (Task, In Progress)\n\nAssignee: (unassigned)\n\n## Description\n\nWidget details here.",
		},
		{
			name: "empty description",
			fields: `{"summary":"Ship the widget","labels":["urgent"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"}}`,
			want: "# PROJ-123 — Ship the widget (Task, In Progress)\n\nAssignee: (unassigned)\n\nLabels: urgent",
		},
		{
			name: "no labels and empty description",
			fields: `{"summary":"Ship the widget","labels":[],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"}}`,
			want: "# PROJ-123 — Ship the widget (Task, In Progress)\n\nAssignee: (unassigned)",
		},
		{

			name: "description ADF has no paragraphs",
			fields: `{"summary":"Ship the widget","labels":["urgent"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title only"}]}]}}`,
			want: "# PROJ-123 — Ship the widget (Task, In Progress)\n\nAssignee: (unassigned)\n\nLabels: urgent",
		},
		{

			name: "description extracts to whitespace only",
			fields: `{"summary":"Ship the widget","labels":["urgent"],
				"issuetype":{"id":"10001","name":"Task"},
				"project":{"id":"1","key":"PROJ"},
				"status":{"id":"1","name":"In Progress"},
				"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":" "}]}]}}`,
			want: "# PROJ-123 — Ship the widget (Task, In Progress)\n\nAssignee: (unassigned)\n\nLabels: urgent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextGetIssueServer(t, "PROJ-123", tt.fields)
			var err error
			out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123"}) })
			if err != nil {
				t.Fatalf("runContextGet: %v", err)
			}
			if strings.TrimRight(out, "\n") != tt.want {
				t.Errorf("stdout = %q, want %q", out, tt.want)
			}
			if strings.Contains(out, "\n\nParent") || strings.Contains(out, "\n\nChildren") {
				t.Errorf("stdout = %q, want no Parent/Children sections at level 0", out)
			}
		})
	}
}

func TestRunContextGet_Level0_ResolvedDate(t *testing.T) {
	const resolvedFields = `{"summary":"Ship the widget","labels":[],
		"issuetype":{"id":"10001","name":"Task"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"Done"},
		"resolutiondate":"2026-01-02T15:04:05.000-0700"}`
	const unresolvedFields = `{"summary":"Ship the widget","labels":[],
		"issuetype":{"id":"10001","name":"Task"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"In Progress"}}`

	t.Run("resolved md", func(t *testing.T) {
		contextGetIssueServer(t, "PROJ-123", resolvedFields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		want := "# PROJ-123 — Ship the widget (Task, Done)\n\nAssignee: (unassigned)\n\nResolved: 2026-01-02T15:04:05-07:00"
		if strings.TrimRight(out, "\n") != want {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	})
	t.Run("unresolved md omits line", func(t *testing.T) {
		contextGetIssueServer(t, "PROJ-123", unresolvedFields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		if strings.Contains(out, "Resolved:") {
			t.Errorf("stdout = %q, want no Resolved line when unresolved", out)
		}
	})
	t.Run("resolved json", func(t *testing.T) {
		contextGetIssueServer(t, "PROJ-123", resolvedFields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		var parsed struct {
			Item struct {
				Resolved *string `json:"resolved"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if parsed.Item.Resolved == nil || *parsed.Item.Resolved != "2026-01-02T15:04:05-07:00" {
			t.Errorf("item.resolved = %v, want 2026-01-02T15:04:05-07:00", parsed.Item.Resolved)
		}
	})
	t.Run("unresolved json is null", func(t *testing.T) {
		contextGetIssueServer(t, "PROJ-123", unresolvedFields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		item := parsed["item"].(map[string]any)
		if _, ok := item["resolved"]; !ok {
			t.Error(`"resolved" key missing from item, want present with explicit null`)
		}
		if item["resolved"] != nil {
			t.Errorf("item.resolved = %#v, want explicit null", item["resolved"])
		}
	})
}

func TestRunContextGet_Level1_ResolvedDateNotOnRelations(t *testing.T) {
	contextGetTestLogin(t)
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search/jql") {
			_, _ = w.Write([]byte(`{"issues":[{"id":"124","key":"PROJ-124","fields":{"summary":"Child","issuetype":{"name":"Task"},"status":{"name":"Done"},"resolutiondate":"2026-01-02T15:04:05.000-0700"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"123","key":"PROJ-123","fields":{"summary":"Parent item","issuetype":{"name":"Epic"},"status":{"name":"Done"},"resolutiondate":"2026-01-02T15:04:05.000-0700"}}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1", "--json"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	item := parsed["item"].(map[string]any)
	if item["resolved"] != "2026-01-02T15:04:05-07:00" {
		t.Errorf("target item.resolved = %#v, want the parsed ISO-8601 string", item["resolved"])
	}
	children, ok := parsed["children"].([]any)
	if !ok || len(children) != 1 {
		t.Fatalf("children = %#v, want one shallow relation", parsed["children"])
	}
	child := children[0].(map[string]any)
	if _, hasResolved := child["resolved"]; hasResolved {
		t.Errorf("child relation = %#v, want no resolved key on a shallow relation", child)
	}
}

const contextGetPoisonedText = `before\u0000X\u001fafter0\rafter1\nafter2\u0080Y\u009fafter3\u202aZ\u202eafter4\u2066W\u2069after5\u2028V\u2029after6`

const contextGetSanitizedText = "beforeXafter0after1after2Yafter3Zafter4Wafter5Vafter6"

const contextGetSurvivingText = `before\u200dX\u200cafter1\u200eY\u200fafter2\u061cZafter3`

const contextGetSurvivingTextWant = "before\u200dX\u200cafter1\u200eY\u200fafter2\u061cZafter3"

func TestRunContextGet_Level0_SanitizesJiraControlledText(t *testing.T) {
	fields := `{"summary":"` + contextGetPoisonedText + `","labels":["clean","` + contextGetPoisonedText + `"],
		"issuetype":{"id":"10001","name":"` + contextGetPoisonedText + `"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"` + contextGetPoisonedText + `"}}`

	t.Run("json", func(t *testing.T) {
		contextGetIssueServer(t, contextGetPoisonedText, fields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		want := `{
  "level": 0,
  "item": {
    "key": "` + contextGetSanitizedText + `",
    "type": "` + contextGetSanitizedText + `",
    "summary": "` + contextGetSanitizedText + `",
    "status": "` + contextGetSanitizedText + `",
    "labels": [
      "clean",
      "` + contextGetSanitizedText + `"
    ],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`
		if strings.TrimRight(out, "\n") != want {
			t.Errorf("stdout = %s, want %s", out, want)
		}
	})

	t.Run("md", func(t *testing.T) {
		contextGetIssueServer(t, contextGetPoisonedText, fields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		want := "# " + contextGetSanitizedText + " — " + contextGetSanitizedText + " (" + contextGetSanitizedText + ", " + contextGetSanitizedText + ")" +
			"\n\nAssignee: (unassigned)" +
			"\n\nLabels: clean, " + contextGetSanitizedText
		if strings.TrimRight(out, "\n") != want {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	})
}

func TestRunContextGet_Level0_PreservesFormatCharacters(t *testing.T) {
	fields := `{"summary":"` + contextGetSurvivingText + `","labels":["clean","` + contextGetSurvivingText + `"],
		"issuetype":{"id":"10001","name":"` + contextGetSurvivingText + `"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"` + contextGetSurvivingText + `"}}`

	t.Run("json", func(t *testing.T) {
		contextGetIssueServer(t, contextGetSurvivingText, fields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		want := `{
  "level": 0,
  "item": {
    "key": "` + contextGetSurvivingTextWant + `",
    "type": "` + contextGetSurvivingTextWant + `",
    "summary": "` + contextGetSurvivingTextWant + `",
    "status": "` + contextGetSurvivingTextWant + `",
    "labels": [
      "clean",
      "` + contextGetSurvivingTextWant + `"
    ],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`
		if strings.TrimRight(out, "\n") != want {
			t.Errorf("stdout = %s, want %s", out, want)
		}
	})

	t.Run("md", func(t *testing.T) {
		contextGetIssueServer(t, contextGetSurvivingText, fields)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		want := "# " + contextGetSurvivingTextWant + " — " + contextGetSurvivingTextWant + " (" + contextGetSurvivingTextWant + ", " + contextGetSurvivingTextWant + ")" +
			"\n\nAssignee: (unassigned)" +
			"\n\nLabels: clean, " + contextGetSurvivingTextWant
		if strings.TrimRight(out, "\n") != want {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	})
}

const contextGetZWJEmoji = "Launch \U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466 plan"

func TestRunContextGet_Level0_ZWJEmojiSummaryMatchesDescription(t *testing.T) {
	fields := `{"summary":"` + contextGetZWJEmoji + `","labels":["urgent"],
		"issuetype":{"id":"10001","name":"Task"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"In Progress"},
		"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"` + contextGetZWJEmoji + `"}]}]}}`
	contextGetIssueServer(t, "PROJ-123", fields)

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--json"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}
	want := `{
  "level": 0,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "` + contextGetZWJEmoji + `",
    "status": "In Progress",
    "labels": [
      "urgent"
    ],
    "description": "` + contextGetZWJEmoji + `",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": null,
  "descendants": null,
  "descendantFailures": null
}`
	if strings.TrimRight(out, "\n") != want {
		t.Errorf("stdout = %s, want %s", out, want)
	}
}

func contextGetLevel1Server(t *testing.T, key, fieldsJSON, childrenBody string) {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(childrenBody))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":` + fieldsJSON + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
}

const (
	contextLevel1ParentAndChildrenFields = `{"summary":"Ship the widget","labels":["urgent"],
		"issuetype":{"id":"10001","name":"Task"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"In Progress"},
		"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Widget details."}]}]},
		"parent":{"id":"9","key":"PROJ-1","fields":{"summary":"Parent epic","status":{"name":"Open"},"issuetype":{"name":"Epic"}}}}`

	contextLevel1TwoChildrenBody = `{"issues":[
		{"id":"2","key":"PROJ-124","fields":{"summary":"Sub one","issuetype":{"name":"Sub-task"},"status":{"name":"Done"}}},
		{"id":"3","key":"PROJ-125","fields":{"summary":"Sub two","issuetype":{"name":"Sub-task"},"status":{"name":"To Do"}}}
	]}`

	contextLevel1NeitherFields = `{"summary":"Top epic","labels":[],
		"issuetype":{"id":"6","name":"Epic"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"To Do"}}`

	contextLevel1NoChildrenBody = `{"issues":[]}`

	contextLevel1ChildrenNoParentFields = `{"summary":"Mid epic","labels":["urgent"],
		"issuetype":{"id":"6","name":"Epic"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"In Progress"}}`

	contextLevel1OneChildBody = `{"issues":[
		{"id":"2","key":"PROJ-200","fields":{"summary":"Only child","issuetype":{"name":"Story"},"status":{"name":"To Do"}}}
	]}`
)

func TestRunContextGet_Level1_JSON(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		fields       string
		childrenBody string
		want         string
	}{
		{
			name:         "parent and children",
			key:          "PROJ-123",
			fields:       contextLevel1ParentAndChildrenFields,
			childrenBody: contextLevel1TwoChildrenBody,
			want: `{
  "level": 1,
  "item": {
    "key": "PROJ-123",
    "type": "Task",
    "summary": "Ship the widget",
    "status": "In Progress",
    "labels": [
      "urgent"
    ],
    "description": "Widget details.",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": {
    "key": "PROJ-1",
    "type": "Epic",
    "summary": "Parent epic",
    "status": "Open",
    "assignee": "(unassigned)"
  },
  "ancestors": null,
  "children": [
    {
      "key": "PROJ-124",
      "type": "Sub-task",
      "summary": "Sub one",
      "status": "Done",
      "assignee": "(unassigned)"
    },
    {
      "key": "PROJ-125",
      "type": "Sub-task",
      "summary": "Sub two",
      "status": "To Do",
      "assignee": "(unassigned)"
    }
  ],
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{
			name:         "neither parent nor children",
			key:          "PROJ-1",
			fields:       contextLevel1NeitherFields,
			childrenBody: contextLevel1NoChildrenBody,
			want: `{
  "level": 1,
  "item": {
    "key": "PROJ-1",
    "type": "Epic",
    "summary": "Top epic",
    "status": "To Do",
    "labels": [],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": [],
  "descendants": null,
  "descendantFailures": null
}`,
		},
		{
			name:         "children but no parent",
			key:          "PROJ-123",
			fields:       contextLevel1ChildrenNoParentFields,
			childrenBody: contextLevel1OneChildBody,
			want: `{
  "level": 1,
  "item": {
    "key": "PROJ-123",
    "type": "Epic",
    "summary": "Mid epic",
    "status": "In Progress",
    "labels": [
      "urgent"
    ],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": null,
  "ancestors": null,
  "children": [
    {
      "key": "PROJ-200",
      "type": "Story",
      "summary": "Only child",
      "status": "To Do",
      "assignee": "(unassigned)"
    }
  ],
  "descendants": null,
  "descendantFailures": null
}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextGetLevel1Server(t, tt.key, tt.fields, tt.childrenBody)
			var err error
			out := captureStdout(t, func() { err = runContextGet([]string{tt.key, "--level", "1", "--json"}) })
			if err != nil {
				t.Fatalf("runContextGet: %v", err)
			}
			if strings.TrimRight(out, "\n") != tt.want {
				t.Errorf("stdout = %s, want %s", out, tt.want)
			}

			var parsed map[string]any
			if err := json.Unmarshal([]byte(out), &parsed); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			if _, ok := parsed["children"]; !ok || parsed["children"] == nil {
				t.Errorf(`children = %#v, want a present, non-null array at level 1`, parsed["children"])
			}
			if _, ok := parsed["parent"]; !ok {
				t.Error(`"parent" key missing from output`)
			}
			if _, ok := parsed["ancestors"]; !ok || parsed["ancestors"] != nil {
				t.Errorf(`ancestors = %#v, want present with explicit null below level 2`, parsed["ancestors"])
			}
			if parsed["descendants"] != nil {
				t.Errorf(`descendants = %#v, want explicit null below level 3`, parsed["descendants"])
			}
		})
	}
}

func TestRunContextGet_Level1_MD(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		fields       string
		childrenBody string
		want         string
	}{
		{
			name:         "parent and children",
			key:          "PROJ-123",
			fields:       contextLevel1ParentAndChildrenFields,
			childrenBody: contextLevel1TwoChildrenBody,
			want: "# PROJ-123 — Ship the widget (Task, In Progress)\n\n" +
				"Assignee: (unassigned)\n\n" +
				"Labels: urgent\n\n" +
				"## Description\n\nWidget details.\n\n" +
				"Parent: PROJ-1 — Parent epic (Epic, Open, (unassigned))\n\n" +
				"## Children\n\n" +
				"- PROJ-124 — Sub one (Sub-task, Done, (unassigned))\n" +
				"- PROJ-125 — Sub two (Sub-task, To Do, (unassigned))",
		},
		{
			name:         "neither parent nor children",
			key:          "PROJ-1",
			fields:       contextLevel1NeitherFields,
			childrenBody: contextLevel1NoChildrenBody,
			want:         "# PROJ-1 — Top epic (Epic, To Do)\n\nAssignee: (unassigned)",
		},
		{
			name:         "children but no parent",
			key:          "PROJ-123",
			fields:       contextLevel1ChildrenNoParentFields,
			childrenBody: contextLevel1OneChildBody,
			want: "# PROJ-123 — Mid epic (Epic, In Progress)\n\n" +
				"Assignee: (unassigned)\n\n" +
				"Labels: urgent\n\n" +
				"## Children\n\n" +
				"- PROJ-200 — Only child (Story, To Do, (unassigned))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextGetLevel1Server(t, tt.key, tt.fields, tt.childrenBody)
			var err error
			out := captureStdout(t, func() { err = runContextGet([]string{tt.key, "--level", "1"}) })
			if err != nil {
				t.Fatalf("runContextGet: %v", err)
			}
			if strings.TrimRight(out, "\n") != tt.want {
				t.Errorf("stdout = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestRunContextGet_Level1_SanitizesParentAndChildText(t *testing.T) {
	fields := `{"summary":"clean","labels":[],
		"issuetype":{"id":"1","name":"Task"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"In Progress"},
		"parent":{"key":"PROJ-1","fields":{"summary":"` + contextGetPoisonedText + `","status":{"name":"Open"},"issuetype":{"name":"Epic"}}}}`
	childrenBody := `{"issues":[{"id":"2","key":"PROJ-2","fields":{"summary":"` + contextGetPoisonedText + `","issuetype":{"name":"Story"},"status":{"name":"To Do"}}}]}`

	contextGetLevel1Server(t, "PROJ-123", fields, childrenBody)
	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1", "--json"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}

	if got := strings.Count(out, `"summary": "`+contextGetSanitizedText+`"`); got != 2 {
		t.Errorf("sanitized relation summary count = %d, want 2 (parent + child); out = %s", got, out)
	}
	for _, bad := range []rune{0x00, 0x1f, '\r', 0x85, 0x2028, 0x2029, 0x202a, 0x2069} {
		if strings.ContainsRune(out, bad) {
			t.Errorf("output retains poisoned rune %U inside a relation field: %q", bad, out)
		}
	}
}

func contextGetParentAccuracyServer(t *testing.T, targetFields, parentKey, parentFields string, parentStatus int) {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			_, _ = w.Write([]byte(`{"issues":[]}`))
		case "/rest/api/3/issue/PROJ-123":
			_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123","fields":` + targetFields + `}`))
		case "/rest/api/3/issue/" + parentKey:
			if parentStatus != 0 {
				w.WriteHeader(parentStatus)
				return
			}
			_, _ = w.Write([]byte(`{"id":"9","key":"` + parentKey + `","fields":` + parentFields + `}`))
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
}

const contextLevel1NestedParentOmitsAssigneeFields = `{"summary":"Target","issuetype":{"name":"Task"},"status":{"name":"In Progress"},
	"parent":{"key":"PROJ-1","fields":{"summary":"Parent epic","status":{"name":"Open"},"issuetype":{"name":"Epic"}}}}`

func TestRunContextGet_ParentAssigneeUsesAccurateFetchWhenNestedOmitsIt(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		contextGetParentAccuracyServer(t, contextLevel1NestedParentOmitsAssigneeFields, "PROJ-1",
			`{"summary":"Parent epic","status":{"name":"Open"},"issuetype":{"name":"Epic"},"assignee":{"displayName":"Jamie Rivera"}}`, 0)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		var parsed struct {
			Parent struct {
				Assignee string `json:"assignee"`
			} `json:"parent"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if parsed.Parent.Assignee != "Jamie Rivera" {
			t.Errorf("parent.assignee = %q, want the accurately-fetched name rather than a false (unassigned)", parsed.Parent.Assignee)
		}
	})

	t.Run("md", func(t *testing.T) {
		contextGetParentAccuracyServer(t, contextLevel1NestedParentOmitsAssigneeFields, "PROJ-1",
			`{"summary":"Parent epic","status":{"name":"Open"},"issuetype":{"name":"Epic"},"assignee":{"displayName":"Jamie Rivera"}}`, 0)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		if !strings.Contains(out, "Parent: PROJ-1 — Parent epic (Epic, Open, Jamie Rivera)") {
			t.Errorf("stdout = %q, want the parent line to carry the accurately-fetched assignee", out)
		}
	})

	t.Run("accurate fetch also empty is genuinely unassigned", func(t *testing.T) {
		contextGetParentAccuracyServer(t, contextLevel1NestedParentOmitsAssigneeFields, "PROJ-1",
			`{"summary":"Parent epic","status":{"name":"Open"},"issuetype":{"name":"Epic"}}`, 0)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		if !strings.Contains(out, "Parent: PROJ-1 — Parent epic (Epic, Open, (unassigned))") {
			t.Errorf("stdout = %q, want (unassigned) when the accurate fetch also reports none", out)
		}
	})
}

func TestRunContextGet_ParentAssigneeSkipsAccurateFetchWhenNestedAlreadyPresent(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		if r.URL.Path != "/rest/api/3/issue/PROJ-123" {
			t.Fatalf("unexpected extra fetch at %q; nested assignee was already present", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123","fields":{"summary":"Target","issuetype":{"name":"Task"},"status":{"name":"In Progress"},
			"parent":{"key":"PROJ-1","fields":{"summary":"Parent epic","status":{"name":"Open"},"issuetype":{"name":"Epic"},"assignee":{"displayName":"Jamie Rivera"}}}}}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}
	if !strings.Contains(out, "Parent: PROJ-1 — Parent epic (Epic, Open, Jamie Rivera)") {
		t.Errorf("stdout = %q, want the nested assignee rendered", out)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2 (target + children, no extra parent-accuracy fetch)", requests)
	}
}

func TestRunContextGet_ParentKeyEmptyAndNoAssigneeRendersWithoutExtraFetch(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		if r.URL.Path != "/rest/api/3/issue/PROJ-123" {
			t.Fatalf("unexpected extra fetch at %q; the parent has no key to fetch", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123","fields":{"summary":"Target","issuetype":{"name":"Task"},"status":{"name":"In Progress"},
			"parent":{"fields":{"summary":"Parent epic","status":{"name":"Open"},"issuetype":{"name":"Epic"}}}}}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}
	if !strings.Contains(out, "Parent:  — Parent epic (Epic, Open, (unassigned))") {
		t.Errorf("stdout = %q, want the keyless parent to render with (unassigned)", out)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2 (target + children, no doomed GetIssueRelation(\"\"))", requests)
	}
}

func TestRunContextGet_ParentAssigneeAccurateFetchFailureIsBodyFree(t *testing.T) {
	contextGetParentAccuracyServer(t, contextLevel1NestedParentOmitsAssigneeFields, "PROJ-1", "", http.StatusInternalServerError)

	var err error
	stderr := captureStderr(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "1"}) })
	if err == nil || !strings.Contains(err.Error(), "could not read parent") {
		t.Fatalf("err = %v, want a could-not-read-parent failure", err)
	}
	if !strings.Contains(stderr, "Jira returned an unexpected response (500)") {
		t.Errorf("stderr = %q, want the safe status wording", stderr)
	}
}

func contextGetTestLogin(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.Save("", &auth.Config{Provider: auth.ProviderAPIToken, Site: "acme.atlassian.net", APIToken: &auth.APITokenConfig{Email: "test@example.com", Token: "test-token"}}); err != nil {
		t.Fatalf("auth.Save: %v", err)
	}
}

type contextGetServerTransport struct {
	srv  *httptest.Server
	next http.RoundTripper
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (rt contextGetServerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(rt.srv.URL)
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme, cloned.URL.Host, cloned.Host = target.Scheme, target.Host, target.Host
	return rt.next.RoundTrip(cloned)
}

func TestRunContextGet_Level2_RendersFullAncestorChainInBothFormats(t *testing.T) {
	for _, format := range []string{"json", "md"} {
		t.Run(format, func(t *testing.T) {
			requests := contextGetLevel2Server(t, map[string]string{
				"PROJ-123": `{"summary":"Target","labels":["urgent"],"issuetype":{"name":"Task"},"status":{"name":"In Progress"},"parent":{"key":"PROJ-20","fields":{"summary":"stale shallow parent","issuetype":{"name":"Story"},"status":{"name":"To Do"}}}}`,
				"PROJ-20":  `{"summary":"Story parent","issuetype":{"name":"Story"},"status":{"name":"Open"},"parent":{"key":"PROJ-1","fields":{"summary":"stale shallow root","issuetype":{"name":"Epic"},"status":{"name":"To Do"}}}}`,
				"PROJ-1":   `{"summary":"Epic root","issuetype":{"name":"Epic"},"status":{"name":"Backlog"}}`,
			}, `{"issues":[{"key":"PROJ-124","fields":{"summary":"Direct child","issuetype":{"name":"Sub-task"},"status":{"name":"Done"}}}]}`)

			args := []string{"PROJ-123", "--level", "2"}
			if format == "json" {
				args = append(args, "--json")
			}
			var err error
			out := captureStdout(t, func() { err = runContextGet(args) })
			if err != nil {
				t.Fatalf("runContextGet: %v", err)
			}
			if got := requests(); got != 5 {
				t.Errorf("requests = %d, want 5", got)
			}

			if format == "json" {
				var got struct {
					Level     int              `json:"level"`
					Parent    map[string]any   `json:"parent"`
					Children  []map[string]any `json:"children"`
					Ancestors []map[string]any `json:"ancestors"`
				}
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if got.Level != 2 || got.Parent["key"] != "PROJ-20" || got.Parent["assignee"] != "(unassigned)" || len(got.Children) != 1 {
					t.Errorf("level-1 relations = %#v, want parent and child retained", got)
				}
				if len(got.Ancestors) != 2 || got.Ancestors[0]["key"] != "PROJ-20" || got.Ancestors[0]["summary"] != "Story parent" || got.Ancestors[1]["key"] != "PROJ-1" {
					t.Errorf("ancestors = %#v, want fetched nearest-parent-first chain", got.Ancestors)
				}
				return
			}
			want := "# PROJ-123 — Target (Task, In Progress)\n\n" +
				"Assignee: (unassigned)\n\n" +
				"Labels: urgent\n\n" +
				"Parent: PROJ-20 — stale shallow parent (Story, To Do, (unassigned))\n\n" +
				"## Ancestors\n\n" +
				"- PROJ-20 — Story parent (Story, Open, (unassigned))\n" +
				"- PROJ-1 — Epic root (Epic, Backlog, (unassigned))\n\n" +
				"## Children\n\n" +
				"- PROJ-124 — Direct child (Sub-task, Done, (unassigned))"
			if strings.TrimRight(out, "\n") != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestRunContextGet_Level2_EmptyAndCyclicAncestorChainsTerminate(t *testing.T) {
	t.Run("target has no parent", func(t *testing.T) {
		contextGetLevel2Server(t, map[string]string{
			"PROJ-1": `{"summary":"Root","issuetype":{"name":"Epic"},"status":{"name":"To Do"}}`,
		}, `{"issues":[]}`)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-1", "--level", "2", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		var got struct {
			Ancestors json.RawMessage `json:"ancestors"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if string(got.Ancestors) != "[]" {
			t.Errorf("ancestors = %s, want []", got.Ancestors)
		}
	})

	t.Run("cycle is not repeated", func(t *testing.T) {
		requests := contextGetLevel2Server(t, map[string]string{
			"PROJ-123": `{"summary":"Target","issuetype":{"name":"Task"},"status":{"name":"To Do"},"parent":{"key":"PROJ-20"}}`,
			"PROJ-20":  `{"summary":"A","issuetype":{"name":"Story"},"status":{"name":"To Do"},"parent":{"key":"PROJ-30"}}`,
			"PROJ-30":  `{"summary":"B","issuetype":{"name":"Epic"},"status":{"name":"To Do"},"parent":{"key":"PROJ-20"}}`,
		}, `{"issues":[]}`)
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "2", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		if got := requests(); got != 5 {
			t.Errorf("requests = %d, want 5 (the cycle itself is not repeated, but the parent-accuracy fetch re-reads PROJ-20 once)", got)
		}
		if !strings.Contains(out, `"ancestors": [`) || strings.Count(out, `"key": "PROJ-20"`) != 2 {
			t.Errorf("stdout = %s, want a finite chain containing PROJ-20 once as an ancestor", out)
		}
	})
}

func TestRunContextGet_Level2_UsesOneDeadlineForSupplementaryReads(t *testing.T) {
	originalTimeout := contextGetLevel2Timeout
	contextGetLevel2Timeout = 20 * time.Second
	t.Cleanup(func() { contextGetLevel2Timeout = originalTimeout })

	requests := contextGetLevel2Server(t, map[string]string{
		"PROJ-123": `{"summary":"Target","issuetype":{"name":"Task"},"status":{"name":"To Do"},"parent":{"key":"PROJ-20"}}`,
		"PROJ-20":  `{"summary":"Parent","issuetype":{"name":"Story"},"status":{"name":"To Do"},"parent":{"key":"PROJ-1"}}`,
		"PROJ-1":   `{"summary":"Root","issuetype":{"name":"Epic"},"status":{"name":"To Do"}}`,
	}, `{"issues":[]}`)

	type observedRequest struct {
		path        string
		deadline    time.Time
		hasDeadline bool
	}
	var observed []observedRequest
	next := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		deadline, hasDeadline := req.Context().Deadline()
		observed = append(observed, observedRequest{path: req.URL.Path, deadline: deadline, hasDeadline: hasDeadline})
		return next.RoundTrip(req)
	})

	var err error
	captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "2", "--json"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}
	if got := requests(); got != 5 {
		t.Fatalf("requests = %d, want target, children, two ancestors, and the parent-accuracy fetch", got)
	}

	var operationDeadline, targetDeadline time.Time
	for _, request := range observed {
		switch request.path {
		case "/rest/api/3/issue/PROJ-123":
			if !request.hasDeadline {
				t.Error("target pre-flight request has no client deadline")
			}
			targetDeadline = request.deadline
		case "/rest/api/3/search/jql", "/rest/api/3/issue/PROJ-20", "/rest/api/3/issue/PROJ-1":
			if !request.hasDeadline {
				t.Errorf("%s has no operation deadline", request.path)
				continue
			}
			if !request.deadline.After(time.Now()) {
				t.Errorf("%s deadline = %v, want a future deadline", request.path, request.deadline)
			}
			if operationDeadline.IsZero() {
				operationDeadline = request.deadline
			} else if !request.deadline.Equal(operationDeadline) {
				t.Errorf("%s deadline = %v, want shared operation deadline %v", request.path, request.deadline, operationDeadline)
			}
		default:
			t.Errorf("unexpected request path %q", request.path)
		}
	}
	if targetDeadline.Equal(operationDeadline) {
		t.Errorf("target pre-flight deadline = %v, want it separate from the level-2 operation deadline", targetDeadline)
	}
	if len(observed) != 5 {
		t.Errorf("observed %d requests, want 5", len(observed))
	}
}

func TestWalkContextAncestors_EnforcesCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PROJ-20" {
			t.Fatalf("path = %q, want parent fetch", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"key":"PROJ-20","fields":{"summary":"A","parent":{"key":"PROJ-30"}}}`))
	}))
	defer srv.Close()
	target := &jira.Issue{Key: "PROJ-123", Fields: jira.IssueFields{Parent: &jira.ParentRef{Key: "PROJ-20"}}}
	_, err := walkContextAncestors(context.Background(), jira.NewClient(srv.URL, ""), target, 1, maxContextAncestorOutputBytes, func(contextoutput.Relation, bool) int { return 0 })
	if err == nil || !strings.Contains(err.Error(), "exceeded 1-node ceiling") {
		t.Fatalf("walkContextAncestors error = %v, want ceiling failure", err)
	}
}

func TestWalkContextAncestors_KeylessParentEndsChain(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected fetch for keyless parent: %s", r.URL.Path)
	}))
	defer srv.Close()
	target := &jira.Issue{Key: "PROJ-123", Fields: jira.IssueFields{Parent: &jira.ParentRef{Key: ""}}}
	got, err := walkContextAncestors(context.Background(), jira.NewClient(srv.URL, ""), target, maxContextAncestors, maxContextAncestorOutputBytes, func(contextoutput.Relation, bool) int { return 0 })
	if err != nil {
		t.Fatalf("walkContextAncestors error = %v, want nil for a keyless parent", err)
	}
	if len(got) != 0 {
		t.Errorf("ancestors = %#v, want empty chain", got)
	}
}

func TestWalkContextAncestors_UsesExactSelectedFormatBudget(t *testing.T) {
	for _, format := range []string{"json", "md"} {
		t.Run(format, func(t *testing.T) {

			parent := contextoutput.Relation{Key: "PROJ-20", Type: "Story", Summary: `quoted "summary" <b>&`, Status: "To Do", Assignee: "(unassigned)"}

			base := contextoutput.Output{
				Level: 2,
				Item:  contextoutput.Item{Key: "PROJ-123", Type: "Task", Summary: "Target", Status: "Open", Labels: []string{}},
			}
			renderFull := func(rels []contextoutput.Relation) int {
				out := base
				out.Ancestors = &rels
				if format == "json" {
					return len(renderContextGetJSON(out))
				}
				return len(renderContextGetMD(out))
			}
			empty := []contextoutput.Relation{}
			wantFirst := renderFull([]contextoutput.Relation{parent}) - renderFull(empty)
			if got := ancestorRenderedContribution(format, parent, true); got != wantFirst {
				t.Fatalf("first-ancestor contribution = %d, want %d (full-document diff)", got, wantFirst)
			}
			wantNext := renderFull([]contextoutput.Relation{parent, parent}) - renderFull([]contextoutput.Relation{parent})
			if got := ancestorRenderedContribution(format, parent, false); got != wantNext {
				t.Fatalf("subsequent-ancestor contribution = %d, want %d (full-document diff)", got, wantNext)
			}

			contrib := func(rel contextoutput.Relation, first bool) int {
				return ancestorRenderedContribution(format, rel, first)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("fields"); got != "parent,summary,issuetype,status,assignee" {
					t.Errorf("fields = %q, want relation-only selection", got)
				}
				_, _ = w.Write([]byte(`{"key":"PROJ-20","fields":{"summary":"quoted \"summary\" <b>&","issuetype":{"name":"Story"},"status":{"name":"To Do"}}}`))
			}))
			defer srv.Close()
			target := &jira.Issue{Key: "PROJ-123", Fields: jira.IssueFields{Parent: &jira.ParentRef{Key: "PROJ-20"}}}

			got, err := walkContextAncestors(context.Background(), jira.NewClient(srv.URL, ""), target, 1, wantFirst, contrib)
			if err != nil || len(got) != 1 {
				t.Fatalf("walk at exact %s budget: got %#v, err %v", format, got, err)
			}
			_, err = walkContextAncestors(context.Background(), jira.NewClient(srv.URL, ""), target, 1, wantFirst-1, contrib)
			if err == nil || !strings.Contains(err.Error(), "rendered ancestor-output budget") {
				t.Fatalf("walk below exact %s budget error = %v, want budget failure", format, err)
			}
		})
	}
}

func TestRunContextGet_Level2_BudgetFailureWritesNoPartialStdout(t *testing.T) {
	for _, format := range []string{"json", "md"} {
		t.Run(format, func(t *testing.T) {
			largeSummary := strings.Repeat("x", 520*1024)
			contextGetLevel2Server(t, map[string]string{
				"PROJ-123": `{"summary":"Target","issuetype":{"name":"Task"},"status":{"name":"To Do"},"parent":{"key":"PROJ-20"}}`,
				"PROJ-20":  `{"summary":"` + largeSummary + `","issuetype":{"name":"Story"},"status":{"name":"To Do"},"parent":{"key":"PROJ-1"}}`,
				"PROJ-1":   `{"summary":"` + largeSummary + `","issuetype":{"name":"Epic"},"status":{"name":"To Do"}}`,
			}, `{"issues":[]}`)

			args := []string{"PROJ-123", "--level", "2"}
			if format == "json" {
				args = append(args, "--json")
			}
			var err error
			out := captureStdout(t, func() { err = runContextGet(args) })
			if err == nil || !strings.Contains(err.Error(), "rendered ancestor-output budget") {
				t.Fatalf("runContextGet error = %v, want budget failure", err)
			}
			if out != "" {
				t.Errorf("stdout = %q, want no partial output", out)
			}
		})
	}
}

func contextGetLevel2Server(t *testing.T, fieldsByKey map[string]string, childrenBody string) func() int {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(childrenBody))
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		fields, ok := fieldsByKey[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":` + fields + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
	return func() int { return requests }
}

var contextParentJQLPattern = regexp.MustCompile(`\Aparent = "(.*)"\z`)

func contextGetLevel3Server(t *testing.T, key, fieldsJSON string, childrenByParent map[string]string, failParent map[string]int) {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/search/jql" {
			jql := r.URL.Query().Get("jql")
			m := contextParentJQLPattern.FindStringSubmatch(jql)
			if m == nil {
				t.Fatalf("could not extract parent key from jql %q", jql)
			}
			parent := m[1]
			if status, ok := failParent[parent]; ok {
				w.WriteHeader(status)
				return
			}
			issues := "[]"
			if body, ok := childrenByParent[parent]; ok {
				issues = body
			}
			_, _ = w.Write([]byte(`{"issues":` + issues + `}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":` + fieldsJSON + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
}

const (
	contextLevel3RootFields = `{"summary":"Epic root","labels":[],
		"issuetype":{"id":"6","name":"Epic"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"In Progress"},
		"parent":{"key":"PROJ-1","fields":{"summary":"Parent portfolio","status":{"name":"Open"},"issuetype":{"name":"Epic"}}}}`

	contextLevel3ChildPROJ2 = `{"id":"2","key":"PROJ-2","fields":{"summary":"Story two","issuetype":{"name":"Story"},"status":{"name":"To Do"}}}`
	contextLevel3ChildPROJ3 = `{"id":"3","key":"PROJ-3","fields":{"summary":"Story three","issuetype":{"name":"Story"},"status":{"name":"Done"}}}`
	contextLevel3ChildPROJ4 = `{"id":"4","key":"PROJ-4","fields":{"summary":"Task four","issuetype":{"name":"Task"},"status":{"name":"To Do"}}}`
	contextLevel3ChildPROJ5 = `{"id":"5","key":"PROJ-5","fields":{"summary":"Subtask five","issuetype":{"name":"Sub-task"},"status":{"name":"To Do"}}}`
)

func contextLevel3Tree() map[string]string {
	return map[string]string{
		"PROJ-123": "[" + contextLevel3ChildPROJ2 + "," + contextLevel3ChildPROJ3 + "]",
		"PROJ-2":   "[" + contextLevel3ChildPROJ4 + "]",
		"PROJ-4":   "[" + contextLevel3ChildPROJ5 + "]",
	}
}

func TestRunContextGet_Level3_JSON_MultiLevelSubtree(t *testing.T) {
	contextGetLevel3Server(t, "PROJ-123", contextLevel3RootFields, contextLevel3Tree(), nil)

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3", "--json"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}

	want := `{
  "level": 3,
  "item": {
    "key": "PROJ-123",
    "type": "Epic",
    "summary": "Epic root",
    "status": "In Progress",
    "labels": [],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": {
    "key": "PROJ-1",
    "type": "Epic",
    "summary": "Parent portfolio",
    "status": "Open",
    "assignee": "(unassigned)"
  },
  "ancestors": null,
  "children": [
    {
      "key": "PROJ-2",
      "type": "Story",
      "summary": "Story two",
      "status": "To Do",
      "assignee": "(unassigned)"
    },
    {
      "key": "PROJ-3",
      "type": "Story",
      "summary": "Story three",
      "status": "Done",
      "assignee": "(unassigned)"
    }
  ],
  "descendants": [
    {
      "node": {
        "key": "PROJ-2",
        "type": "Story",
        "summary": "Story two",
        "status": "To Do",
        "assignee": "(unassigned)"
      },
      "depth": 1,
      "parentKey": "PROJ-123"
    },
    {
      "node": {
        "key": "PROJ-3",
        "type": "Story",
        "summary": "Story three",
        "status": "Done",
        "assignee": "(unassigned)"
      },
      "depth": 1,
      "parentKey": "PROJ-123"
    },
    {
      "node": {
        "key": "PROJ-4",
        "type": "Task",
        "summary": "Task four",
        "status": "To Do",
        "assignee": "(unassigned)"
      },
      "depth": 2,
      "parentKey": "PROJ-2"
    },
    {
      "node": {
        "key": "PROJ-5",
        "type": "Sub-task",
        "summary": "Subtask five",
        "status": "To Do",
        "assignee": "(unassigned)"
      },
      "depth": 3,
      "parentKey": "PROJ-4"
    }
  ],
  "descendantFailures": []
}`
	if strings.TrimRight(out, "\n") != want {
		t.Errorf("stdout = %s, want %s", out, want)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := parsed["descendantFailures"]; !ok || parsed["descendantFailures"] == nil {
		t.Errorf(`descendantFailures = %#v, want a present, non-null array at level 3`, parsed["descendantFailures"])
	}
}

func TestRunContextGet_Level3_MD_MultiLevelSubtree(t *testing.T) {
	contextGetLevel3Server(t, "PROJ-123", contextLevel3RootFields, contextLevel3Tree(), nil)

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}

	want := "# PROJ-123 — Epic root (Epic, In Progress)\n\n" +
		"Assignee: (unassigned)\n\n" +
		"Parent: PROJ-1 — Parent portfolio (Epic, Open, (unassigned))\n\n" +
		"## Children\n\n" +
		"- PROJ-2 — Story two (Story, To Do, (unassigned))\n" +
		"  - PROJ-4 — Task four (Task, To Do, (unassigned))\n" +
		"    - PROJ-5 — Subtask five (Sub-task, To Do, (unassigned))\n" +
		"- PROJ-3 — Story three (Story, Done, (unassigned))"
	if strings.TrimRight(out, "\n") != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestRunContextGet_Level3_JSON_FailedBranchReportedDistinctly(t *testing.T) {

	children := map[string]string{
		"PROJ-123": "[" + contextLevel3ChildPROJ2 + "," + contextLevel3ChildPROJ3 + "]",
		"PROJ-2":   "[" + contextLevel3ChildPROJ4 + "]",
	}
	contextGetLevel3Server(t, "PROJ-123", contextLevel3RootFields, children, map[string]int{"PROJ-3": http.StatusInternalServerError})

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3", "--json"}) })
	if err != nil {
		t.Fatalf("runContextGet must not abort on a per-branch walk failure: %v", err)
	}

	want := `{
  "level": 3,
  "item": {
    "key": "PROJ-123",
    "type": "Epic",
    "summary": "Epic root",
    "status": "In Progress",
    "labels": [],
    "description": "",
    "resolved": null,
    "assignee": "(unassigned)"
  },
  "parent": {
    "key": "PROJ-1",
    "type": "Epic",
    "summary": "Parent portfolio",
    "status": "Open",
    "assignee": "(unassigned)"
  },
  "ancestors": null,
  "children": [
    {
      "key": "PROJ-2",
      "type": "Story",
      "summary": "Story two",
      "status": "To Do",
      "assignee": "(unassigned)"
    },
    {
      "key": "PROJ-3",
      "type": "Story",
      "summary": "Story three",
      "status": "Done",
      "assignee": "(unassigned)"
    }
  ],
  "descendants": [
    {
      "node": {
        "key": "PROJ-2",
        "type": "Story",
        "summary": "Story two",
        "status": "To Do",
        "assignee": "(unassigned)"
      },
      "depth": 1,
      "parentKey": "PROJ-123"
    },
    {
      "node": {
        "key": "PROJ-3",
        "type": "Story",
        "summary": "Story three",
        "status": "Done",
        "assignee": "(unassigned)"
      },
      "depth": 1,
      "parentKey": "PROJ-123"
    },
    {
      "node": {
        "key": "PROJ-4",
        "type": "Task",
        "summary": "Task four",
        "status": "To Do",
        "assignee": "(unassigned)"
      },
      "depth": 2,
      "parentKey": "PROJ-2"
    }
  ],
  "descendantFailures": [
    {
      "key": "PROJ-3",
      "parentKey": "PROJ-123",
      "depth": 1,
      "reason": "Jira returned an unexpected response (500)"
    }
  ]
}`
	if strings.TrimRight(out, "\n") != want {
		t.Errorf("stdout = %s, want %s", out, want)
	}

	var parsed struct {
		Descendants []struct {
			Node struct {
				Key string `json:"key"`
			} `json:"node"`
		} `json:"descendants"`
		DescendantFailures []struct {
			Key   string `json:"key"`
			Depth int    `json:"depth"`
		} `json:"descendantFailures"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	var walkedPROJ3 bool
	for _, d := range parsed.Descendants {
		if d.Node.Key == "PROJ-3" {
			walkedPROJ3 = true
		}
	}
	if !walkedPROJ3 {
		t.Error("PROJ-3 missing from descendants; a node whose child-fetch failed is still a reached node")
	}
	if len(parsed.DescendantFailures) != 1 || parsed.DescendantFailures[0].Key != "PROJ-3" || parsed.DescendantFailures[0].Depth != 1 {
		t.Errorf("descendantFailures = %#v, want exactly one entry for PROJ-3 at depth 1", parsed.DescendantFailures)
	}
}

func TestRunContextGet_Level3_MD_FailedBranchReportedDistinctly(t *testing.T) {
	children := map[string]string{
		"PROJ-123": "[" + contextLevel3ChildPROJ2 + "," + contextLevel3ChildPROJ3 + "]",
		"PROJ-2":   "[" + contextLevel3ChildPROJ4 + "]",
	}
	contextGetLevel3Server(t, "PROJ-123", contextLevel3RootFields, children, map[string]int{"PROJ-3": http.StatusInternalServerError})

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3"}) })
	if err != nil {
		t.Fatalf("runContextGet must not abort on a per-branch walk failure: %v", err)
	}

	want := "# PROJ-123 — Epic root (Epic, In Progress)\n\n" +
		"Assignee: (unassigned)\n\n" +
		"Parent: PROJ-1 — Parent portfolio (Epic, Open, (unassigned))\n\n" +
		"## Children\n\n" +
		"- PROJ-2 — Story two (Story, To Do, (unassigned))\n" +
		"  - PROJ-4 — Task four (Task, To Do, (unassigned))\n" +
		"- PROJ-3 — Story three (Story, Done, (unassigned))\n\n" +
		"## Descendant walk failures\n\n" +
		"- PROJ-3 (depth 1, parent PROJ-123): Jira returned an unexpected response (500)"
	if strings.TrimRight(out, "\n") != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

const contextLevel3ChildPROJ6 = `{"id":"6","key":"PROJ-6","fields":{"summary":"Task six","issuetype":{"name":"Task"},"status":{"name":"To Do"}}}`

func TestRunContextGet_Level3_MD_DeepSiblings(t *testing.T) {
	children := map[string]string{
		"PROJ-123": "[" + contextLevel3ChildPROJ2 + "]",
		"PROJ-2":   "[" + contextLevel3ChildPROJ4 + "," + contextLevel3ChildPROJ6 + "]",
		"PROJ-4":   "[" + contextLevel3ChildPROJ5 + "]",
	}
	contextGetLevel3Server(t, "PROJ-123", contextLevel3RootFields, children, nil)

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}

	want := "# PROJ-123 — Epic root (Epic, In Progress)\n\n" +
		"Assignee: (unassigned)\n\n" +
		"Parent: PROJ-1 — Parent portfolio (Epic, Open, (unassigned))\n\n" +
		"## Children\n\n" +
		"- PROJ-2 — Story two (Story, To Do, (unassigned))\n" +
		"  - PROJ-4 — Task four (Task, To Do, (unassigned))\n" +
		"    - PROJ-5 — Subtask five (Sub-task, To Do, (unassigned))\n" +
		"  - PROJ-6 — Task six (Task, To Do, (unassigned))"
	if strings.TrimRight(out, "\n") != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestRunContextGet_Level3_MD_MultipleRootsEachWithSubtree(t *testing.T) {
	children := map[string]string{
		"PROJ-123": "[" + contextLevel3ChildPROJ2 + "," + contextLevel3ChildPROJ3 + "]",
		"PROJ-2":   "[" + contextLevel3ChildPROJ4 + "]",
		"PROJ-3":   "[" + contextLevel3ChildPROJ5 + "]",
	}
	contextGetLevel3Server(t, "PROJ-123", contextLevel3RootFields, children, nil)

	var err error
	out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3"}) })
	if err != nil {
		t.Fatalf("runContextGet: %v", err)
	}

	want := "# PROJ-123 — Epic root (Epic, In Progress)\n\n" +
		"Assignee: (unassigned)\n\n" +
		"Parent: PROJ-1 — Parent portfolio (Epic, Open, (unassigned))\n\n" +
		"## Children\n\n" +
		"- PROJ-2 — Story two (Story, To Do, (unassigned))\n" +
		"  - PROJ-4 — Task four (Task, To Do, (unassigned))\n" +
		"- PROJ-3 — Story three (Story, Done, (unassigned))\n" +
		"  - PROJ-5 — Subtask five (Sub-task, To Do, (unassigned))"
	if strings.TrimRight(out, "\n") != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func contextGetLevel3DivergenceServer(t *testing.T, firstHop, secondHop string, childrenByParent map[string]string) func() int {
	t.Helper()
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)

	var rootChildCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/search/jql" {
			parent := contextParentJQLPattern.FindStringSubmatch(r.URL.Query().Get("jql"))[1]
			if parent == "PROJ-123" {
				rootChildCalls++
				if rootChildCalls == 1 {
					_, _ = w.Write([]byte(`{"issues":` + firstHop + `}`))
					return
				}
				_, _ = w.Write([]byte(`{"issues":` + secondHop + `}`))
				return
			}
			issues := "[]"
			if body, ok := childrenByParent[parent]; ok {
				issues = body
			}
			_, _ = w.Write([]byte(`{"issues":` + issues + `}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123","fields":` + contextLevel3RootFields + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}
	return func() int { return rootChildCalls }
}

func TestRunContextGet_Level3_SharedFirstHop(t *testing.T) {
	firstHop := "[" + contextLevel3ChildPROJ2 + "," + contextLevel3ChildPROJ3 + "]"
	secondHop := "[" + contextLevel3ChildPROJ2 + "]"
	subtree := map[string]string{"PROJ-2": "[" + contextLevel3ChildPROJ4 + "]"}
	for _, format := range []string{"--json", "--md"} {
		t.Run(format, func(t *testing.T) {
			rootCalls := contextGetLevel3DivergenceServer(t, firstHop, secondHop, subtree)
			var err error
			out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3", format}) })
			if err != nil {
				t.Fatalf("runContextGet: %v", err)
			}
			if got := rootCalls(); got != 1 {
				t.Fatalf("ChildrenOf(PROJ-123) requests = %d, want 1", got)
			}
			if strings.Contains(out, "subtree not explored") || strings.Contains(out, "Descendant walk incomplete") {
				t.Errorf("stdout retained obsolete reconciliation output: %q", out)
			}
			if format == "--md" {
				if !strings.Contains(out, "- PROJ-2 — Story two (Story, To Do, (unassigned))\n  - PROJ-4 — Task four (Task, To Do, (unassigned))\n- PROJ-3 — Story three (Story, Done, (unassigned))") {
					t.Errorf("stdout = %q, want both first-hop children in the rendered tree", out)
				}
				return
			}
			var parsed struct {
				Children []struct {
					Key string `json:"key"`
				} `json:"children"`
				Descendants []struct {
					Node struct {
						Key string `json:"key"`
					} `json:"node"`
					Depth int `json:"depth"`
				} `json:"descendants"`
			}
			if err := json.Unmarshal([]byte(out), &parsed); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			children, depthOne := map[string]bool{}, map[string]bool{}
			for _, child := range parsed.Children {
				children[child.Key] = true
			}
			for _, descendant := range parsed.Descendants {
				if descendant.Depth == 1 {
					depthOne[descendant.Node.Key] = true
				}
			}
			if len(children) != len(depthOne) {
				t.Fatalf("children = %#v, depth-1 descendants = %#v", children, depthOne)
			}
			for key := range children {
				if !depthOne[key] {
					t.Errorf("child %q missing from depth-1 descendants: %#v", key, depthOne)
				}
			}
		})
	}
}

func TestRunContextGet_Level3_FailedBranchReasonIsBodyFree(t *testing.T) {
	const secret = "descendant-branch-body-secret-must-not-leak"
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	contextGetTestLogin(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/search/jql" {
			parent := contextParentJQLPattern.FindStringSubmatch(r.URL.Query().Get("jql"))[1]
			if parent == "PROJ-2" {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(secret))
				return
			}
			if parent == "PROJ-123" {
				_, _ = w.Write([]byte(`{"issues":[` + contextLevel3ChildPROJ2 + `]}`))
				return
			}
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","key":"PROJ-123","fields":` + contextLevel3RootFields + `}`))
	}))
	t.Cleanup(srv.Close)
	http.DefaultTransport = contextGetServerTransport{srv: srv, next: originalTransport}

	for _, format := range []string{"--json", "--md"} {
		t.Run(format, func(t *testing.T) {
			var err error
			out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-123", "--level", "3", format}) })
			if err != nil {
				t.Fatalf("runContextGet: %v", err)
			}
			if strings.Contains(out, secret) {
				t.Errorf("output leaked the Jira response body: %q", out)
			}
			if !strings.Contains(out, "502") {
				t.Errorf("output = %q, want the bare 502 status in the failure reason", out)
			}
		})
	}
}

func TestRunContextGet_Level3_NoDescendants(t *testing.T) {
	fields := `{"summary":"Lonely epic","labels":[],
		"issuetype":{"id":"6","name":"Epic"},
		"project":{"id":"1","key":"PROJ"},
		"status":{"id":"1","name":"To Do"}}`
	contextGetLevel3Server(t, "PROJ-9", fields, nil, nil)

	t.Run("json", func(t *testing.T) {
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-9", "--level", "3", "--json"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		var got struct {
			Descendants        json.RawMessage `json:"descendants"`
			DescendantFailures json.RawMessage `json:"descendantFailures"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if string(got.Descendants) != "[]" || string(got.DescendantFailures) != "[]" {
			t.Errorf("descendants = %s, descendantFailures = %s, want [] and []", got.Descendants, got.DescendantFailures)
		}
	})

	t.Run("md", func(t *testing.T) {
		var err error
		out := captureStdout(t, func() { err = runContextGet([]string{"PROJ-9", "--level", "3"}) })
		if err != nil {
			t.Fatalf("runContextGet: %v", err)
		}
		if strings.TrimRight(out, "\n") != "# PROJ-9 — Lonely epic (Epic, To Do)\n\nAssignee: (unassigned)" {
			t.Errorf("stdout = %q, want title and assignee only with no Children section", out)
		}
	})
}
