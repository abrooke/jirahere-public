package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

type createServerConfig struct {
	metadata      string
	metaStatus    int
	parents       map[string]bool
	parentStatus  map[string]int
	failCreate    int
	failSetParent int
	createdKey    string
}

func newCreateServer(t *testing.T, cfg createServerConfig) (*httptest.Server, *[]string, *map[string]any, *map[string]any) {
	t.Helper()
	calls := []string{}
	createBody := map[string]any{}
	setParentBody := map[string]any{}
	if cfg.createdKey == "" {
		cfg.createdKey = "PROJ-101"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/createmeta":
			calls = append(calls, "createmeta")
			if cfg.metaStatus != 0 {
				w.WriteHeader(cfg.metaStatus)
				_, _ = w.Write([]byte(`{"errorMessages":["boom"]}`))
				return
			}
			body := cfg.metadata
			if body == "" {

				body = `{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"5","name":"Task","subtask":false},{"id":"6","name":"Epic"}]}]}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			calls = append(calls, "get:"+key)
			if st := cfg.parentStatus[key]; st != 0 {
				w.WriteHeader(st)
				_, _ = w.Write([]byte(`{"errorMessages":["boom"]}`))
				return
			}
			if !cfg.parents[key] {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","key":"` + key + `","fields":{}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			calls = append(calls, "create")
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Fatalf("decoding create body %s: %v", data, err)
			}
			if cfg.failCreate != 0 {
				w.WriteHeader(cfg.failCreate)
				_, _ = w.Write([]byte(`{"errorMessages":["SENSITIVE-Jira-detail"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"999","key":"` + cfg.createdKey + `"}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/"):
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			calls = append(calls, "setparent:"+key)
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &setParentBody); err != nil {
				t.Fatalf("decoding set-parent body %s: %v", data, err)
			}
			if cfg.failSetParent != 0 {
				w.WriteHeader(cfg.failSetParent)
				_, _ = w.Write([]byte(`{"errorMessages":["SENSITIVE-set-parent-detail"]}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	return srv, &calls, &createBody, &setParentBody
}

func createClient(srv *httptest.Server) *jira.Client { return jira.NewClient(srv.URL, "Bearer tok") }

func baseCreateInput() CreateInput {
	return CreateInput{ProjectKey: "PROJ", IssueTypeName: "Task", Summary: "Widget rollout", Labels: []string{"team-widgets"}}
}

func TestCreate_ResolveProjectUnreachable(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{metaStatus: http.StatusInternalServerError})
	defer srv.Close()

	_, err := Create(context.Background(), createClient(srv), baseCreateInput())

	var want *ErrCreateResolveProject
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateResolveProject", err)
	}

	if want.Key != "PROJ" {
		t.Errorf("Key = %q, want PROJ", want.Key)
	}
	if want.Operation != CreatePreflightResolveProject {
		t.Errorf("Operation = %d, want CreatePreflightResolveProject", want.Operation)
	}
	if want.Subject != `project "PROJ"` {
		t.Errorf("Subject = %q, want %q", want.Subject, `project "PROJ"`)
	}
	if want.Unwrap() == nil {
		t.Error("Unwrap() = nil, want the wrapped Jira error")
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta"}) {
		t.Errorf("calls = %v, want [createmeta] (nothing after the metadata failure)", *calls)
	}
}

func TestCreate_ProjectUnavailable(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{metadata: `{"projects":[]}`})
	defer srv.Close()

	_, err := Create(context.Background(), createClient(srv), baseCreateInput())

	var want *ErrCreateProjectUnavailable
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateProjectUnavailable", err)
	}
	if want.ProjectKey != "PROJ" {
		t.Errorf("ProjectKey = %q, want PROJ", want.ProjectKey)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta"}) {
		t.Errorf("calls = %v, want [createmeta]", *calls)
	}
}

func TestCreate_IssueTypeUnavailable(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{
		metadata: `{"projects":[{"id":"10000","key":"PROJ","issuetypes":[{"id":"6","name":"Epic"},{"id":"5","name":"Task"}]}]}`,
		parents:  map[string]bool{"PROJ-1": true},
	})
	defer srv.Close()

	in := baseCreateInput()
	in.IssueTypeName = "Bug"
	in.HasParent = true
	in.ParentKey = "PROJ-1"

	_, err := Create(context.Background(), createClient(srv), in)

	var want *ErrCreateIssueTypeUnavailable
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateIssueTypeUnavailable", err)
	}
	if want.IssueTypeName != "Bug" || want.ProjectKey != "PROJ" {
		t.Errorf("unexpected fields: %+v", want)
	}
	if !reflect.DeepEqual(want.Available, []string{"Epic", "Task"}) {
		t.Errorf("Available = %v, want [Epic Task] (sorted)", want.Available)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta"}) {
		t.Errorf("calls = %v, want [createmeta] (no parent GET, no create)", *calls)
	}
}

func TestCreate_ParentNotFound(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{})
	defer srv.Close()

	in := baseCreateInput()
	in.HasParent = true
	in.ParentKey = "PROJ-9"

	_, err := Create(context.Background(), createClient(srv), in)

	var want *ErrCreateParentNotFound
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateParentNotFound", err)
	}
	if want.Key != "PROJ-9" {
		t.Errorf("Key = %q, want PROJ-9", want.Key)
	}

	if got, exp := want.Error(), `parent "PROJ-9" not found (404)`; got != exp {
		t.Errorf("Error() = %q, want %q", got, exp)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "get:PROJ-9"}) {
		t.Errorf("calls = %v, want [createmeta get:PROJ-9] (no create)", *calls)
	}
}

func TestCreate_ValidateParentUnreachable(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{
		parentStatus: map[string]int{"PROJ-9": http.StatusInternalServerError},
	})
	defer srv.Close()

	in := baseCreateInput()
	in.HasParent = true
	in.ParentKey = "PROJ-9"

	_, err := Create(context.Background(), createClient(srv), in)

	var want *ErrCreateValidateParent
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateValidateParent", err)
	}
	if want.Key != "PROJ-9" || want.Unwrap() == nil {
		t.Errorf("unexpected fields: %+v", want)
	}

	if want.Operation == CreatePreflightResolveProject {
		t.Errorf("Operation = CreatePreflightResolveProject, want it unset for the --parent check")
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "get:PROJ-9"}) {
		t.Errorf("calls = %v, want [createmeta get:PROJ-9] (no create)", *calls)
	}
}

func TestCreate_CreateCallFails(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{
		parents:    map[string]bool{"PROJ-1": true},
		failCreate: http.StatusBadRequest,
	})
	defer srv.Close()

	in := baseCreateInput()
	in.HasParent = true
	in.ParentKey = "PROJ-1"

	res, err := Create(context.Background(), createClient(srv), in)

	var want *ErrCreateCallFailed
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateCallFailed", err)
	}
	if res != nil {
		t.Errorf("result = %v, want nil (nothing was created)", res)
	}
	if want.Unwrap() == nil {
		t.Error("Unwrap() = nil, want the wrapped Jira error")
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "get:PROJ-1", "create"}) {
		t.Errorf("calls = %v, want [createmeta get:PROJ-1 create]", *calls)
	}
}

func TestCreate_SetParentFails_ItemNotRolledBack(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{
		parents:       map[string]bool{"PROJ-1": true},
		failSetParent: http.StatusBadRequest,
	})
	defer srv.Close()

	in := baseCreateInput()
	in.HasParent = true
	in.ParentKey = "PROJ-1"

	res, err := Create(context.Background(), createClient(srv), in)
	if err != nil {
		t.Fatalf("Create returned error, want a result with ParentErr set instead: %v", err)
	}
	if res == nil || res.Key != "PROJ-101" {
		t.Fatalf("res = %+v, want a result with Key PROJ-101 (the item is not rolled back)", res)
	}
	if !res.ParentAttempted {
		t.Error("ParentAttempted = false, want true")
	}
	if res.ParentErr == nil {
		t.Fatal("ParentErr = nil, want the wrapped Jira error")
	}
	var statusErr *jira.StatusError
	if !errors.As(res.ParentErr, &statusErr) || statusErr.StatusCode != http.StatusBadRequest {
		t.Errorf("ParentErr = %v, want a *jira.StatusError carrying 400", res.ParentErr)
	}
	if res.ParentKey != "PROJ-1" {
		t.Errorf("ParentKey = %q, want PROJ-1", res.ParentKey)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "get:PROJ-1", "create", "setparent:PROJ-101"}) {
		t.Errorf("calls = %v, want [createmeta get:PROJ-1 create setparent:PROJ-101]", *calls)
	}
}

func TestCreate_SuccessResolvedPayload_NoParentNoDescription(t *testing.T) {
	srv, calls, body, _ := newCreateServer(t, createServerConfig{createdKey: "PROJ-9"})
	defer srv.Close()

	in := baseCreateInput()
	in.Labels = []string{"team-widgets", "FY26-Q1"}

	res, err := Create(context.Background(), createClient(srv), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Key != "PROJ-9" {
		t.Errorf("Key = %q, want PROJ-9", res.Key)
	}
	if res.ParentAttempted || res.ParentErr != nil {
		t.Errorf("ParentAttempted=%v ParentErr=%v, want false/nil (no --parent given)", res.ParentAttempted, res.ParentErr)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "create"}) {
		t.Errorf("calls = %v, want [createmeta create] (no parent GET, no set-parent PUT)", *calls)
	}

	fields, _ := (*body)["fields"].(map[string]any)
	if fields == nil {
		t.Fatal("create body has no \"fields\" object")
	}
	if got := fields["project"]; !reflect.DeepEqual(got, map[string]any{"id": "10000"}) {
		t.Errorf("fields[project] = %#v, want the resolved project ID 10000", got)
	}
	if got := fields["issuetype"]; !reflect.DeepEqual(got, map[string]any{"id": "5"}) {
		t.Errorf("fields[issuetype] = %#v, want the resolved issue-type ID 5", got)
	}
	if got := fields["summary"]; got != "Widget rollout" {
		t.Errorf("fields[summary] = %#v, want %q", got, "Widget rollout")
	}
	labels, _ := fields["labels"].([]any)
	got := make([]string, len(labels))
	for i, l := range labels {
		got[i], _ = l.(string)
	}
	if !reflect.DeepEqual(got, []string{"team-widgets", "FY26-Q1"}) {
		t.Errorf("fields[labels] = %#v, want [team-widgets FY26-Q1] verbatim", got)
	}
	if _, ok := fields["description"]; ok {
		t.Errorf("fields = %v, want no description key when HasDescription is false", fields)
	}
}

func TestCreate_SubtaskType_AtomicParentInSingleCreateCall(t *testing.T) {
	srv, calls, body, _ := newCreateServer(t, createServerConfig{
		metadata: `{"projects":[{"id":"10000","key":"PROJ","issuetypes":[
			{"id":"5","name":"Task","subtask":false},
			{"id":"8","name":"Subtask","subtask":true}
		]}]}`,
		parents: map[string]bool{"PROJ-1": true},
	})
	defer srv.Close()

	in := baseCreateInput()
	in.IssueTypeName = "Subtask"
	in.HasParent = true
	in.ParentKey = "PROJ-1"

	res, err := Create(context.Background(), createClient(srv), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "get:PROJ-1", "create"}) {
		t.Errorf("calls = %v, want [createmeta get:PROJ-1 create] (no setparent call)", *calls)
	}
	if !res.ParentAttempted || res.ParentErr != nil || res.ParentKey != "PROJ-1" {
		t.Errorf("ParentAttempted=%v ParentErr=%v ParentKey=%q, want true/nil/PROJ-1", res.ParentAttempted, res.ParentErr, res.ParentKey)
	}

	fields, _ := (*body)["fields"].(map[string]any)
	if got := fields["issuetype"]; !reflect.DeepEqual(got, map[string]any{"id": "8"}) {
		t.Errorf("fields[issuetype] = %#v, want the resolved subtask issue-type ID 8", got)
	}
	if got, want := fields["parent"], map[string]any{"key": "PROJ-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fields[parent] = %#v, want %#v (parent inside the create call)", got, want)
	}
}

func TestCreate_SubtaskUndetermined_WithParentAborts(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{parents: map[string]bool{"PROJ-1": true}})
	defer srv.Close()

	in := baseCreateInput()
	in.IssueTypeName = "Epic"
	in.HasParent = true
	in.ParentKey = "PROJ-1"

	res, err := Create(context.Background(), createClient(srv), in)

	var want *ErrCreateSubtaskUndetermined
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateSubtaskUndetermined", err)
	}
	if want.IssueTypeName != "Epic" || want.ProjectKey != "PROJ" {
		t.Errorf("unexpected fields: %+v", want)
	}
	if res != nil {
		t.Errorf("result = %v, want nil (nothing was created)", res)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "get:PROJ-1"}) {
		t.Errorf("calls = %v, want [createmeta get:PROJ-1] (no create, no setparent)", *calls)
	}
}

func TestCreate_SubtaskUndetermined_NoParentCreatesNormally(t *testing.T) {
	srv, calls, _, _ := newCreateServer(t, createServerConfig{createdKey: "PROJ-42"})
	defer srv.Close()

	in := baseCreateInput()
	in.IssueTypeName = "Epic"

	res, err := Create(context.Background(), createClient(srv), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Key != "PROJ-42" {
		t.Errorf("Key = %q, want PROJ-42", res.Key)
	}
	if res.ParentAttempted || res.ParentErr != nil {
		t.Errorf("ParentAttempted=%v ParentErr=%v, want false/nil (no --parent given)", res.ParentAttempted, res.ParentErr)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "create"}) {
		t.Errorf("calls = %v, want [createmeta create]", *calls)
	}
}

func TestCreate_SuccessWithParentAndDescriptionADF(t *testing.T) {
	srv, calls, body, parentBody := newCreateServer(t, createServerConfig{parents: map[string]bool{"PROJ-1": true}})
	defer srv.Close()

	in := baseCreateInput()
	in.HasParent = true
	in.ParentKey = "PROJ-1"
	in.HasDescription = true
	in.Description = "para one\nstill one\n\n*para* two"

	res, err := Create(context.Background(), createClient(srv), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !reflect.DeepEqual(*calls, []string{"createmeta", "get:PROJ-1", "create", "setparent:PROJ-101"}) {
		t.Errorf("calls = %v, want [createmeta get:PROJ-1 create setparent:PROJ-101]", *calls)
	}
	if !res.ParentAttempted || res.ParentErr != nil || res.ParentKey != "PROJ-1" {
		t.Errorf("ParentAttempted=%v ParentErr=%v ParentKey=%q, want true/nil/PROJ-1", res.ParentAttempted, res.ParentErr, res.ParentKey)
	}
	if pf, _ := (*parentBody)["fields"].(map[string]any); pf == nil || pf["parent"] == nil {
		t.Errorf("set-parent body = %v, want a fields.parent object", *parentBody)
	}

	fields, _ := (*body)["fields"].(map[string]any)
	desc, ok := fields["description"]
	if !ok {
		t.Fatalf("fields = %v, want a description key", fields)
	}
	descJSON, _ := json.Marshal(desc)
	for _, want := range []string{
		`"type":"doc"`, `"type":"paragraph"`, `"type":"hardBreak"`,
		`"text":"para one"`, `"text":"still one"`, `"text":"*para* two"`,
	} {
		if !strings.Contains(string(descJSON), want) {
			t.Errorf("description ADF = %s, want it to contain %s", descJSON, want)
		}
	}
}

func TestCreate_EmptyDescriptionStillSendsDocument(t *testing.T) {
	srv, _, body, _ := newCreateServer(t, createServerConfig{})
	defer srv.Close()

	in := baseCreateInput()
	in.HasDescription = true
	in.Description = ""

	if _, err := Create(context.Background(), createClient(srv), in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	fields, _ := (*body)["fields"].(map[string]any)
	desc, ok := fields["description"]
	if !ok {
		t.Fatalf("fields = %v, want a description key even for an empty --description", fields)
	}
	descJSON, _ := json.Marshal(desc)
	if !strings.Contains(string(descJSON), `"type":"doc"`) {
		t.Errorf("description = %s, want a well-formed ADF doc", descJSON)
	}
}

func TestSortedIssueTypeNames(t *testing.T) {
	got := sortedIssueTypeNames(map[string]jira.IssueType{
		"Task": {ID: "5"}, "Epic": {ID: "6"}, "Bug": {ID: "7"},
	})
	if !reflect.DeepEqual(got, []string{"Bug", "Epic", "Task"}) {
		t.Errorf("sortedIssueTypeNames = %v, want [Bug Epic Task]", got)
	}
	if got := sortedIssueTypeNames(map[string]jira.IssueType{}); len(got) != 0 {
		t.Errorf("sortedIssueTypeNames(empty) = %v, want []", got)
	}
}
