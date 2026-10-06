package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

type fakeIssue struct {
	key         string
	typeID      string
	typeName    string
	projID      string
	summary     string
	labels      []string
	description json.RawMessage
}

func issueJSON(fi fakeIssue) string {
	labels, _ := json.Marshal(fi.labels)
	out := fmt.Sprintf(`{"id":"1","key":%q,"fields":{"summary":%q,"labels":%s,"issuetype":{"id":%q,"name":%q},"project":{"id":%q,"key":"PROJ"}`,
		fi.key, fi.summary, labels, fi.typeID, fi.typeName, fi.projID)
	if fi.description != nil {
		out += fmt.Sprintf(`,"description":%s`, fi.description)
	}
	return out + "}}"
}

type recorder struct {
	calls             []string
	createBody        map[string]any
	linkBody          map[string]any
	parentBody        map[string]any
	sourceSummaryBody map[string]any
}

func (r *recorder) record(name string) {
	if r == nil {
		return
	}
	r.calls = append(r.calls, name)
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading request body: %v", err)
	}
	var body map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatalf("decoding request body %s: %v", data, err)
		}
	}
	return body
}

type serverConfig struct {
	issues            map[string]fakeIssue
	linkTypeNames     []string
	failLink          bool
	failParent        bool
	failCreate        bool
	failSourceSummary bool

	errorGetKeys map[string]bool

	errorLinkTypes bool
	createdKey     string
	rec            *recorder
}

func defaultConfig() serverConfig {
	return serverConfig{
		issues: map[string]fakeIssue{
			"PROJ-123": {key: "PROJ-123", typeID: "5", typeName: "Epic", projID: "10000", summary: "Widget rollout Q3'26", labels: []string{"FY26-Q3", "team-widgets"}},
			"PROJ-001": {key: "PROJ-001", typeID: "1", typeName: "Initiative", projID: "10000", summary: "Parent initiative", labels: nil},
		},
		linkTypeNames: []string{"Blocks", "Cloners"},
		createdKey:    "PROJ-456",
	}
}

func newTestServer(t *testing.T, cfg serverConfig) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/rest/api/3/issue/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		switch r.Method {
		case http.MethodGet:
			cfg.rec.record("get:" + key)
			if cfg.errorGetKeys[key] {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errorMessages":["internal error"]}`))
				return
			}
			fi, ok := cfg.issues[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(issueJSON(fi)))
		case http.MethodPut:

			if key == cfg.createdKey {
				cfg.rec.record("parent")
				if cfg.rec != nil {
					cfg.rec.parentBody = decodeBody(t, r)
				}
				if cfg.failParent {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"errorMessages":["issue type cannot be a child of this parent"]}`))
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			cfg.rec.record("sourceSummary")
			if cfg.rec != nil {
				cfg.rec.sourceSummaryBody = decodeBody(t, r)
			}
			if cfg.failSourceSummary {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errorMessages":["could not update summary"]}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s on %s", r.Method, r.URL.Path)
		}
	})

	mux.HandleFunc("/rest/api/3/issue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s on /rest/api/3/issue", r.Method)
		}
		cfg.rec.record("create")
		if cfg.rec != nil {
			cfg.rec.createBody = decodeBody(t, r)
		}
		if cfg.failCreate {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errorMessages":["could not create issue"]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"999","key":%q}`, cfg.createdKey)
	})

	mux.HandleFunc("/rest/api/3/issueLinkType", func(w http.ResponseWriter, r *http.Request) {
		cfg.rec.record("linktypes")
		if cfg.errorLinkTypes {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errorMessages":["internal error"]}`))
			return
		}
		types := make([]map[string]string, 0, len(cfg.linkTypeNames))
		for _, n := range cfg.linkTypeNames {
			types = append(types, map[string]string{"id": "1", "name": n, "inward": "is cloned by", "outward": "is a clone of"})
		}
		body, _ := json.Marshal(map[string]any{"issueLinkTypes": types})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	mux.HandleFunc("/rest/api/3/issueLink", func(w http.ResponseWriter, r *http.Request) {
		cfg.rec.record("link")
		if cfg.rec != nil {
			cfg.rec.linkBody = decodeBody(t, r)
		}
		if cfg.failLink {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errorMessages":["could not create link"]}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
	})

	return httptest.NewServer(mux)
}

func TestClone_SourceNotFound(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-999"})

	var want *ErrSourceNotFound
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrSourceNotFound", err)
	}
	if want.Key != "PROJ-999" {
		t.Errorf("Key = %q, want PROJ-999", want.Key)
	}

	if want.Subject != "clone source" {
		t.Errorf("Subject = %q, want %q", want.Subject, "clone source")
	}
	if got, exp := want.Error(), `clone source "PROJ-999" not found (404)`; got != exp {
		t.Errorf("Error() = %q, want %q", got, exp)
	}
}

func TestClone_SummaryOldNotFound(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasSummary: true, SummaryOld: "Q2'26", SummaryNew: "Q4'26",
	})

	var want *ErrSummaryNotFound
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrSummaryNotFound", err)
	}
	if want.SourceSummary != "Widget rollout Q3'26" || want.Substr != "Q2'26" {
		t.Errorf("unexpected fields: %+v", want)
	}
}

func TestClone_LabelOldNotFound(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q2", LabelNew: "FY26-Q4",
	})

	var want *ErrLabelNotFound
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrLabelNotFound", err)
	}
	if len(want.Labels) != 2 {
		t.Errorf("Labels = %v, unexpected", want.Labels)
	}
}

func TestClone_LabelNewInvalid(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "New Label",
	})

	var want *ErrInvalidLabel
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrInvalidLabel", err)
	}
	if want.Label != "New Label" {
		t.Errorf("Label = %q, want %q", want.Label, "New Label")
	}
}

func TestClone_LabelNewInvalid_UnicodeWhitespace(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "New\u00A0Label",
	})

	var want *ErrInvalidLabel
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrInvalidLabel", err)
	}
}

func TestClone_LabelNewEmpty(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "",
	})

	if !errors.Is(err, ErrLabelNewEmpty) {
		t.Fatalf("err = %v, want ErrLabelNewEmpty", err)
	}
}

func TestClone_LabelNewAllowsFormerlyDisallowedCharacters(t *testing.T) {
	cfg := defaultConfig()
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "urgent!+café",
	})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if len(res.Labels) != 2 || res.Labels[0] != "urgent!+café" {
		t.Errorf("Labels = %v, want [urgent!+café team-widgets]", res.Labels)
	}
}

func TestClone_ParentNotFound(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-999",
	})

	var want *ErrParentNotFound
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrParentNotFound", err)
	}
	if want.Key != "PROJ-999" {
		t.Errorf("Key = %q, want PROJ-999", want.Key)
	}

	if want.Subject != "parent" {
		t.Errorf("Subject = %q, want %q", want.Subject, "parent")
	}
	if got, exp := want.Error(), `parent "PROJ-999" not found (404)`; got != exp {
		t.Errorf("Error() = %q, want %q", got, exp)
	}
}

func TestClone_ClonersLinkTypeMissing(t *testing.T) {
	cfg := defaultConfig()
	cfg.linkTypeNames = []string{"Blocks"}
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-123"})

	if !errors.Is(err, ErrClonersLinkTypeMissing) {
		t.Fatalf("err = %v, want ErrClonersLinkTypeMissing", err)
	}
}

func TestClone_ClonersLinkDirection(t *testing.T) {
	cfg := defaultConfig()
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	if _, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-123"}); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	inwardIssue, _ := rec.linkBody["inwardIssue"].(map[string]any)
	outwardIssue, _ := rec.linkBody["outwardIssue"].(map[string]any)
	if got, want := inwardIssue["key"], "PROJ-456"; got != want {
		t.Errorf("link body inwardIssue.key = %v, want %q (the created clone)", got, want)
	}
	if got, want := outwardIssue["key"], "PROJ-123"; got != want {
		t.Errorf("link body outwardIssue.key = %v, want %q (the source)", got, want)
	}
}

func TestClone_FullSuccess(t *testing.T) {
	cfg := defaultConfig()
	wantDescription := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Original description text."}]}]}`)
	src := cfg.issues["PROJ-123"]
	src.description = wantDescription
	cfg.issues["PROJ-123"] = src
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey:  "PROJ-123",
		HasSummary: true, SummaryOld: "Q3'26", SummaryNew: "Q4'26",
		HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
		HasParent: true, ParentKey: "PROJ-001",
	})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if res.NewKey != "PROJ-456" {
		t.Errorf("NewKey = %q, want PROJ-456", res.NewKey)
	}
	if res.NewSummary != "Widget rollout Q4'26" {
		t.Errorf("NewSummary = %q, unexpected", res.NewSummary)
	}
	if res.SourceSummary != "Widget rollout Q3'26" {
		t.Errorf("SourceSummary = %q, unexpected (should be unmodified)", res.SourceSummary)
	}
	if len(res.Labels) != 2 || res.Labels[0] != "FY26-Q4" || res.Labels[1] != "team-widgets" {
		t.Errorf("Labels = %v, unexpected", res.Labels)
	}
	if len(res.OtherLabels) != 1 || res.OtherLabels[0] != "team-widgets" {
		t.Errorf("OtherLabels = %v, want [team-widgets]", res.OtherLabels)
	}
	if res.LinkErr != nil {
		t.Errorf("LinkErr = %v, want nil", res.LinkErr)
	}
	if !res.ParentAttempted || res.ParentErr != nil {
		t.Errorf("ParentAttempted = %v, ParentErr = %v, want true/nil", res.ParentAttempted, res.ParentErr)
	}
	if res.ParentType != "Initiative" {
		t.Errorf("ParentType = %q, want Initiative", res.ParentType)
	}

	wantDecoded := map[string]any{}
	if err := json.Unmarshal(wantDescription, &wantDecoded); err != nil {
		t.Fatalf("unmarshal wantDescription: %v", err)
	}
	fields, _ := rec.createBody["fields"].(map[string]any)
	if fields == nil {
		t.Fatal("create body has no \"fields\" object")
	}
	gotDescription, ok := fields["description"]
	if !ok {
		t.Fatal("create body has no \"description\" field")
	}
	if !reflect.DeepEqual(gotDescription, any(wantDecoded)) {
		t.Errorf("create body description = %v, want %v", gotDescription, wantDecoded)
	}
	if got, want := fields["summary"], "Widget rollout Q4'26"; got != want {
		t.Errorf("create body summary = %v, want %q", got, want)
	}
	gotLabels, _ := fields["labels"].([]any)
	if len(gotLabels) != 2 || gotLabels[0] != "FY26-Q4" || gotLabels[1] != "team-widgets" {
		t.Errorf("create body labels = %v, want [FY26-Q4 team-widgets]", gotLabels)
	}

	wantOrder := []string{"get:PROJ-123", "get:PROJ-001", "linktypes", "create", "link", "parent"}
	if !reflect.DeepEqual(rec.calls, wantOrder) {
		t.Errorf("call order = %v, want %v", rec.calls, wantOrder)
	}
}

func TestClone_LabelSwapOntoExistingLabel(t *testing.T) {
	cfg := defaultConfig()
	cfg.issues["PROJ-123"] = fakeIssue{
		key: "PROJ-123", typeID: "5", typeName: "Epic", projID: "10000",
		summary: "Widget rollout Q3'26", labels: []string{"FY26-Q3", "FY26-Q4"},
	}
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasLabel: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
	})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if len(res.Labels) != 1 || res.Labels[0] != "FY26-Q4" {
		t.Errorf("Labels = %v, want [FY26-Q4] (set semantics, no duplicate)", res.Labels)
	}
	if len(res.OtherLabels) != 0 {
		t.Errorf("OtherLabels = %v, want empty (FY26-Q4 is the swap target, not an unchanged label)", res.OtherLabels)
	}
}

func TestClone_MinimalSuccess_NoFlags(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-123"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if res.NewSummary != "Widget rollout Q3'26" {
		t.Errorf("NewSummary = %q, want source summary unmodified", res.NewSummary)
	}
	if len(res.Labels) != 2 {
		t.Errorf("Labels = %v, want both source labels carried over", res.Labels)
	}
	if res.LabelSwapped {
		t.Error("LabelSwapped = true, want false")
	}
	if res.ParentAttempted {
		t.Error("ParentAttempted = true, want false (no --parent given)")
	}
}

func TestClone_LinkFails_NoParent(t *testing.T) {
	cfg := defaultConfig()
	cfg.failLink = true
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-123"})
	if err != nil {
		t.Fatalf("Clone returned error (should return a result with LinkErr set instead): %v", err)
	}
	if res.NewKey != "PROJ-456" {
		t.Errorf("NewKey = %q, want PROJ-456 (the clone must still exist)", res.NewKey)
	}
	if res.LinkErr == nil {
		t.Fatal("LinkErr = nil, want non-nil")
	}
	if res.ParentAttempted {
		t.Error("ParentAttempted = true, want false: no --parent was given")
	}
}

func TestClone_LinkFails_ParentSucceeds(t *testing.T) {
	cfg := defaultConfig()
	cfg.failLink = true
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
	})
	if err != nil {
		t.Fatalf("Clone returned error (should return a result with LinkErr set instead): %v", err)
	}
	if res.NewKey != "PROJ-456" {
		t.Errorf("NewKey = %q, want PROJ-456 (the clone must still exist)", res.NewKey)
	}
	if res.LinkErr == nil {
		t.Fatal("LinkErr = nil, want non-nil")
	}
	if !res.ParentAttempted {
		t.Error("ParentAttempted = false, want true: the parent step must run regardless of the link outcome")
	}
	if res.ParentErr != nil {
		t.Errorf("ParentErr = %v, want nil (the parent step succeeds independently of the failed link)", res.ParentErr)
	}
	wantCalls := []string{"get:PROJ-123", "get:PROJ-001", "linktypes", "create", "link", "parent"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("call order = %v, want %v (parent must still be attempted after link fails)", rec.calls, wantCalls)
	}
}

func TestClone_LinkFailsAndParentFails(t *testing.T) {
	cfg := defaultConfig()
	cfg.failLink = true
	cfg.failParent = true
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
	})
	if err != nil {
		t.Fatalf("Clone returned error (should return a result with LinkErr/ParentErr set instead): %v", err)
	}
	if res.NewKey != "PROJ-456" {
		t.Errorf("NewKey = %q, want PROJ-456 (the clone must still exist)", res.NewKey)
	}
	if res.LinkErr == nil {
		t.Error("LinkErr = nil, want non-nil")
	}
	if !res.ParentAttempted {
		t.Error("ParentAttempted = false, want true")
	}
	if res.ParentErr == nil {
		t.Error("ParentErr = nil, want non-nil")
	}
}

func TestClone_ParentRejectedAfterCreateAndLink(t *testing.T) {
	cfg := defaultConfig()
	cfg.failParent = true
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
	})
	if err != nil {
		t.Fatalf("Clone returned error (should return a result with ParentErr set instead): %v", err)
	}
	if res.NewKey != "PROJ-456" {
		t.Errorf("NewKey = %q, want PROJ-456 (the clone must still exist)", res.NewKey)
	}
	if res.LinkErr != nil {
		t.Errorf("LinkErr = %v, want nil (link must have succeeded)", res.LinkErr)
	}
	if !res.ParentAttempted {
		t.Error("ParentAttempted = false, want true")
	}
	if res.ParentErr == nil {
		t.Fatal("ParentErr = nil, want non-nil")
	}
}

func TestClone_PreflightUnreachable_SourceGet(t *testing.T) {
	cfg := defaultConfig()
	cfg.errorGetKeys = map[string]bool{"PROJ-123": true}
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
	})

	var want *ErrPreflightUnreachable
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrPreflightUnreachable", err)
	}

	if !strings.HasPrefix(want.Error(), "could not reach Jira to validate the clone: ") {
		t.Errorf("Error() = %q, want the clone's own preflight phrasing", want.Error())
	}
	wantCalls := []string{"get:PROJ-123"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("calls = %v, want %v (parent/linktypes/create must not run once source GET fails)", rec.calls, wantCalls)
	}
}

func TestClone_PreflightUnreachable_ParentGet(t *testing.T) {
	cfg := defaultConfig()
	cfg.errorGetKeys = map[string]bool{"PROJ-001": true}
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
	})

	var want *ErrPreflightUnreachable
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrPreflightUnreachable", err)
	}
	wantCalls := []string{"get:PROJ-123", "get:PROJ-001"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("calls = %v, want %v (linktypes/create must not run once parent GET fails)", rec.calls, wantCalls)
	}
}

func TestClone_PreflightUnreachable_LinkTypes(t *testing.T) {
	cfg := defaultConfig()
	cfg.errorLinkTypes = true
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	_, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
	})

	var want *ErrPreflightUnreachable
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrPreflightUnreachable", err)
	}
	wantCalls := []string{"get:PROJ-123", "get:PROJ-001", "linktypes"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("calls = %v, want %v (create must not run once the link-type check fails)", rec.calls, wantCalls)
	}
}

func TestClone_CreateFails(t *testing.T) {
	cfg := defaultConfig()
	cfg.failCreate = true
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasParent: true, ParentKey: "PROJ-001",
	})

	var want *ErrCreateFailed
	if !errors.As(err, &want) {
		t.Fatalf("err = %v, want *ErrCreateFailed", err)
	}
	if res != nil {
		t.Errorf("result = %v, want nil (nothing was created)", res)
	}
	wantCalls := []string{"get:PROJ-123", "get:PROJ-001", "linktypes", "create"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("calls = %v, want %v (link/parent must not run once create fails)", rec.calls, wantCalls)
	}
}

func TestClone_Part_FoundBranch_IncrementsMarker(t *testing.T) {
	cfg := defaultConfig()
	cfg.issues["PROJ-123"] = fakeIssue{
		key: "PROJ-123", typeID: "5", typeName: "Epic", projID: "10000",
		summary: "Widget rollout [Part 2]", labels: []string{"FY26-Q3"},
	}
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-123", HasPart: true})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if got, want := res.NewSummary, "Widget rollout [Part 3]"; got != want {
		t.Errorf("NewSummary = %q, want %q", got, want)
	}
	if !res.PartFound {
		t.Error("PartFound = false, want true")
	}
	if got, want := res.PartFoundMarker, "[Part 2]"; got != want {
		t.Errorf("PartFoundMarker = %q, want %q", got, want)
	}
	if got, want := res.PartCloneMarker, "[Part 3]"; got != want {
		t.Errorf("PartCloneMarker = %q, want %q", got, want)
	}
	if res.PartSourceErr != nil {
		t.Errorf("PartSourceErr = %v, want nil", res.PartSourceErr)
	}

	for _, c := range rec.calls {
		if c == "sourceSummary" {
			t.Fatalf("calls = %v, want no sourceSummary write (found branch must not touch the source)", rec.calls)
		}
	}
	wantCalls := []string{"get:PROJ-123", "linktypes", "create", "link"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", rec.calls, wantCalls)
	}
}

func TestClone_Part_NotFoundBranch_AppendsToSourceAndDerivesCloneFromPreMutation(t *testing.T) {
	cfg := defaultConfig()
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-123", HasPart: true})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if got, want := res.NewSummary, "Widget rollout Q3'26 [Part 2]"; got != want {
		t.Errorf("NewSummary = %q, want %q", got, want)
	}
	if res.PartFound {
		t.Error("PartFound = true, want false")
	}
	if got, want := res.PartSourceMarker, "[Part 1]"; got != want {
		t.Errorf("PartSourceMarker = %q, want %q", got, want)
	}
	if got, want := res.PartCloneMarker, "[Part 2]"; got != want {
		t.Errorf("PartCloneMarker = %q, want %q", got, want)
	}
	if res.PartSourceErr != nil {
		t.Errorf("PartSourceErr = %v, want nil", res.PartSourceErr)
	}

	fields, _ := rec.sourceSummaryBody["fields"].(map[string]any)
	if fields == nil {
		t.Fatal("source SetSummary body has no \"fields\" object")
	}
	if got, want := fields["summary"], "Widget rollout Q3'26 [Part 1]"; got != want {
		t.Errorf("source SetSummary body summary = %v, want %q", got, want)
	}

	wantCalls := []string{"get:PROJ-123", "linktypes", "create", "link", "sourceSummary"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", rec.calls, wantCalls)
	}
}

func TestClone_Part_NotFoundBranch_SourceSetSummaryFails(t *testing.T) {
	cfg := defaultConfig()
	cfg.failSourceSummary = true
	rec := &recorder{}
	cfg.rec = rec
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{
		SourceKey: "PROJ-123", HasPart: true, HasParent: true, ParentKey: "PROJ-001",
	})
	if err != nil {
		t.Fatalf("Clone returned error (should return a result with PartSourceErr set instead): %v", err)
	}
	if res.NewKey != "PROJ-456" {
		t.Errorf("NewKey = %q, want PROJ-456 (the clone must still exist)", res.NewKey)
	}
	if res.PartSourceErr == nil {
		t.Fatal("PartSourceErr = nil, want non-nil")
	}
	if res.LinkErr != nil {
		t.Errorf("LinkErr = %v, want nil (link must still be attempted and succeed)", res.LinkErr)
	}
	if !res.ParentAttempted || res.ParentErr != nil {
		t.Errorf("ParentAttempted = %v, ParentErr = %v, want true/nil (parent step must still run)", res.ParentAttempted, res.ParentErr)
	}
	if got, want := res.NewSummary, "Widget rollout Q3'26 [Part 2]"; got != want {
		t.Errorf("NewSummary = %q, want %q (clone summary unaffected by the source write's failure)", got, want)
	}
	wantCalls := []string{"get:PROJ-123", "get:PROJ-001", "linktypes", "create", "link", "parent", "sourceSummary"}
	if !reflect.DeepEqual(rec.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", rec.calls, wantCalls)
	}
}

func TestClone_Part_Absent_NoExtraFields(t *testing.T) {
	cfg := defaultConfig()
	srv := newTestServer(t, cfg)
	defer srv.Close()
	client := jira.NewClient(srv.URL, "Bearer tok")

	res, err := Clone(context.Background(), client, CloneInput{SourceKey: "PROJ-123"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if res.PartRequested || res.PartFound || res.PartSourceErr != nil {
		t.Errorf("PartRequested=%v PartFound=%v PartSourceErr=%v, want all zero when --part wasn't given", res.PartRequested, res.PartFound, res.PartSourceErr)
	}
	if got, want := res.NewSummary, "Widget rollout Q3'26"; got != want {
		t.Errorf("NewSummary = %q, want %q (unchanged behavior without --part)", got, want)
	}
}

func TestDerivePartNumbering(t *testing.T) {
	tests := []struct {
		name          string
		summary       string
		wantFound     bool
		wantCloneSum  string
		wantSourceSum string
	}{
		{
			name: "found, increments", summary: "Widget rollout [Part 2]",
			wantFound: true, wantCloneSum: "Widget rollout [Part 3]",
		},
		{
			name: "not found, plain summary", summary: "Widget rollout",
			wantFound: false, wantCloneSum: "Widget rollout [Part 2]", wantSourceSum: "Widget rollout [Part 1]",
		},
		{
			name: "case-sensitive: lowercase 'part' does not match", summary: "Widget rollout [part 2]",
			wantFound: false, wantCloneSum: "Widget rollout [part 2] [Part 2]", wantSourceSum: "Widget rollout [part 2] [Part 1]",
		},
		{
			name: "malformed: no digits at all", summary: "Widget rollout [Part]",
			wantFound: false, wantCloneSum: "Widget rollout [Part] [Part 2]", wantSourceSum: "Widget rollout [Part] [Part 1]",
		},
		{
			name: "malformed: non-digit word instead of digits", summary: "Widget rollout [Part two]",
			wantFound: false, wantCloneSum: "Widget rollout [Part two] [Part 2]", wantSourceSum: "Widget rollout [Part two] [Part 1]",
		},
		{
			name: "multiple markers: leftmost wins", summary: "Foo [Part 2] Bar [Part 5]",
			wantFound: true, wantCloneSum: "Foo [Part 3] Bar [Part 5]",
		},
		{
			name: "Unicode digit does not match: Go's [0-9] is ASCII-only", summary: "Widget rollout [Part ٢]",
			wantFound: false, wantCloneSum: "Widget rollout [Part ٢] [Part 2]", wantSourceSum: "Widget rollout [Part ٢] [Part 1]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pn := derivePartNumbering(tc.summary)
			if pn.found != tc.wantFound {
				t.Errorf("found = %v, want %v", pn.found, tc.wantFound)
			}
			if pn.cloneSummary != tc.wantCloneSum {
				t.Errorf("cloneSummary = %q, want %q", pn.cloneSummary, tc.wantCloneSum)
			}
			if !tc.wantFound && pn.sourceSummary != tc.wantSourceSum {
				t.Errorf("sourceSummary = %q, want %q", pn.sourceSummary, tc.wantSourceSum)
			}
		})
	}
}
