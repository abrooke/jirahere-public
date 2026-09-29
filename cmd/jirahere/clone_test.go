package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

type cloneRoundTripper func(*http.Request) (*http.Response, error)

func (f cloneRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type cloneFailingReadCloser struct{ err error }

func (b cloneFailingReadCloser) Read([]byte) (int, error) { return 0, b.err }
func (cloneFailingReadCloser) Close() error               { return nil }

func TestRunClone_NoSourceKey(t *testing.T) {
	err := runClone(nil)
	if err == nil {
		t.Fatal("runClone: expected error for missing source key, got nil")
	}
}

func TestSplitCloneArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantSource   string
		wantFlagArgs []string
		wantErr      bool
	}{
		{
			name:         "source before flags",
			args:         []string{"PROJ-123", "--summary-old", "Q3'26", "--summary-new", "Q4'26"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--summary-old", "Q3'26", "--summary-new", "Q4'26"},
		},
		{
			name:         "source after flags",
			args:         []string{"--summary-old", "Q3'26", "--summary-new", "Q4'26", "PROJ-123"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--summary-old", "Q3'26", "--summary-new", "Q4'26"},
		},
		{
			name:         "flag value that looks like a source key",
			args:         []string{"--parent", "PROJ-001", "PROJ-123"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--parent", "PROJ-001"},
		},
		{
			name:         "equals form",
			args:         []string{"PROJ-123", "--summary-old=Q3'26", "--summary-new=Q4'26"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--summary-old=Q3'26", "--summary-new=Q4'26"},
		},
		{
			name:         "-- terminator with only the source key after it",
			args:         []string{"--", "PROJ-123"},
			wantSource:   "PROJ-123",
			wantFlagArgs: nil,
		},
		{
			name:    "-- terminator does not silently drop a recognized flag after it",
			args:    []string{"PROJ-123", "--", "--parent", "PROJ-001"},
			wantErr: true,
		},
		{
			name:         "repeated flags are both preserved for the flag layer to reject",
			args:         []string{"PROJ-123", "--parent", "PROJ-001", "--parent", "PROJ-002"},
			wantSource:   "PROJ-123",
			wantFlagArgs: []string{"--parent", "PROJ-001", "--parent", "PROJ-002"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, flagArgs, err := splitCloneArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitCloneArgs(%v) = (%q, %v, nil), want an error", tt.args, source, flagArgs)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitCloneArgs(%v): unexpected error: %v", tt.args, err)
			}
			if source != tt.wantSource {
				t.Errorf("source = %q, want %q", source, tt.wantSource)
			}
			if !reflect.DeepEqual(flagArgs, tt.wantFlagArgs) {
				t.Errorf("flagArgs = %v, want %v", flagArgs, tt.wantFlagArgs)
			}
		})
	}
}

func TestRunClone_RepeatedFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "summary-old",
			args: []string{"PROJ-123", "--summary-old", "a", "--summary-new", "b", "--summary-old", "c"},
			want: "jirahere: --summary-old may not be given more than once.",
		},
		{
			name: "summary-new",
			args: []string{"PROJ-123", "--summary-old", "a", "--summary-new", "b", "--summary-new", "c"},
			want: "jirahere: --summary-new may not be given more than once.",
		},
		{
			name: "label-old",
			args: []string{"PROJ-123", "--label-old", "a", "--label-new", "b", "--label-old", "c"},
			want: "jirahere: --label-old may not be given more than once.",
		},
		{
			name: "label-new",
			args: []string{"PROJ-123", "--label-old", "a", "--label-new", "b", "--label-new", "c"},
			want: "jirahere: --label-new may not be given more than once.",
		},
		{
			name: "parent",
			args: []string{"PROJ-123", "--parent", "PROJ-001", "--parent", "PROJ-002"},
			want: "jirahere: --parent may not be given more than once.",
		},
		{

			name: "part",
			args: []string{"PROJ-123", "--part", "--part"},
			want: "jirahere: --part may not be given more than once.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runClone(tt.args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunClone_RepeatedPart_RejectedBeforeAnyJiraCall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &auth.APITokenConfig{Email: "user@example.com", Token: "token"},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var calls int
	originalTransport := http.DefaultTransport
	http.DefaultTransport = cloneRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("no Jira call should have been made")
	})
	defer func() { http.DefaultTransport = originalTransport }()

	var err error
	stderr := captureStderr(t, func() { err = runClone([]string{"PROJ-123", "--part", "--part"}) })

	want := "jirahere: --part may not be given more than once."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
	if calls != 0 {
		t.Errorf("Jira transport was called %d time(s); want 0 (validation must precede any Jira call)", calls)
	}
}

func TestRunClone_SummaryPairingError(t *testing.T) {
	err := runClone([]string{"PROJ-123", "--summary-old", "Q3'26"})
	want := "jirahere: --summary-old and --summary-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunClone_SummaryPairingError_OnlyNew(t *testing.T) {
	err := runClone([]string{"PROJ-123", "--summary-new", "Q4'26"})
	want := "jirahere: --summary-old and --summary-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunClone_LabelPairingError(t *testing.T) {
	err := runClone([]string{"PROJ-123", "--label-old", "FY26-Q3"})
	want := "jirahere: --label-old and --label-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunClone_LabelPairingError_OnlyNew(t *testing.T) {
	err := runClone([]string{"PROJ-123", "--label-new", "FY26-Q4"})
	want := "jirahere: --label-old and --label-new must be given together."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRunClone_PartAndSummaryMutualExclusion(t *testing.T) {
	want := "jirahere: --part may not be combined with --summary-old or --summary-new."
	tests := []struct {
		name string
		args []string
	}{
		{"summary flags before --part", []string{"PROJ-123", "--summary-old", "Q3'26", "--summary-new", "Q4'26", "--part"}},
		{"--part before summary flags", []string{"PROJ-123", "--part", "--summary-old", "Q3'26", "--summary-new", "Q4'26"}},
		{"--part with --summary-old alone", []string{"PROJ-123", "--part", "--summary-old", "Q3'26"}},
		{"--part with --summary-new alone", []string{"PROJ-123", "--part", "--summary-new", "Q4'26"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runClone(tt.args)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestMapCloneError_SourceNotFound(t *testing.T) {
	err := mapCloneError(&command.ErrSourceNotFound{Subject: "clone source", Key: "PROJ-999"})
	want := `jirahere: clone source "PROJ-999" not found (404).`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestMapCloneError_SummaryNotFound(t *testing.T) {
	err := mapCloneError(&command.ErrSummaryNotFound{
		SourceKey: "PROJ-123", SourceSummary: "Widget rollout Q3'26", Substr: "Q2'26",
	})
	want := `jirahere: PROJ-123's summary ("Widget rollout Q3'26") does not contain "Q2'26" — nothing was created.`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestMapCloneError_LabelNotFound(t *testing.T) {
	err := mapCloneError(&command.ErrLabelNotFound{
		SourceKey: "PROJ-123", Label: "FY26-Q2", Labels: []string{"FY26-Q3", "team-widgets"},
	})
	want := `jirahere: PROJ-123 does not have the label "FY26-Q2" (its labels: FY26-Q3, team-widgets).`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestMapCloneError_InvalidLabel(t *testing.T) {
	err := mapCloneError(&command.ErrInvalidLabel{Label: "New Label"})
	want := `jirahere: "New Label" is not a valid Jira label (labels can't contain whitespace).`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestMapCloneError_LabelNewEmpty(t *testing.T) {
	err := mapCloneError(command.ErrLabelNewEmpty)
	want := "jirahere: --label-new must not be empty."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestMapCloneError_ParentNotFound(t *testing.T) {
	err := mapCloneError(&command.ErrParentNotFound{Subject: "parent", Key: "PROJ-999"})
	want := `jirahere: parent "PROJ-999" not found (404).`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestMapCloneError_ClonersLinkTypeMissing(t *testing.T) {
	err := mapCloneError(command.ErrClonersLinkTypeMissing)
	want := `jirahere: this site has no "Cloners" issue link type (it may have been renamed or removed) — clone requires it to record the original-to-clone link. Ask a Jira admin to restore it.`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestMapCloneError_PreflightUnreachable(t *testing.T) {
	err := mapCloneError(&command.ErrPreflightUnreachable{Err: errors.New("connection reset")})
	want := "jirahere: could not reach Jira to validate the clone: could not reach Jira. Try again."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestCloneJiraErrorRenderingExcludesStatusBodies(t *testing.T) {
	const secret = "jira-response-body-secret-must-not-leak"

	t.Run("preflight", func(t *testing.T) {
		out := captureStderr(t, func() {
			_ = mapCloneError(&command.ErrPreflightUnreachable{Err: &jira.StatusError{StatusCode: 502, Body: secret}})
		})
		if strings.Contains(out, secret) || !strings.Contains(out, "(502)") {
			t.Errorf("preflight output = %q, want safe status without body", out)
		}
	})

	t.Run("create", func(t *testing.T) {
		out := captureStderr(t, func() {
			_ = mapCloneError(&command.ErrCreateFailed{SourceKey: "PROJ-123", Err: &jira.StatusError{StatusCode: 503, Body: secret}})
		})
		if strings.Contains(out, secret) || !strings.Contains(out, "(503)") {
			t.Errorf("create output = %q, want safe status without body", out)
		}
	})

	for _, tt := range []struct {
		name string
		res  *command.CloneResult
		want string
	}{
		{
			name: "link",
			res:  &command.CloneResult{SourceKey: "PROJ-123", NewKey: "PROJ-456", LinkErr: &jira.StatusError{StatusCode: 504, Body: secret}},
			want: "(504)",
		},
		{
			name: "parent",
			res:  &command.CloneResult{SourceKey: "PROJ-123", NewKey: "PROJ-456", ParentAttempted: true, ParentKey: "PROJ-001", ParentErr: &jira.StatusError{StatusCode: 500, Body: secret}},
			want: "(500)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() { printCloneResult(tt.res) })
			if strings.Contains(out, secret) || !strings.Contains(out, tt.want) {
				t.Errorf("%s output = %q, want safe status without body", tt.name, out)
			}
		})
	}
}

func TestCloneJiraErrorRendering_HTTPStatusBoundaries(t *testing.T) {
	for _, status := range []int{99, 100, 599, 600} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			wantStatus := status >= 100 && status <= 599
			preflight := captureStderr(t, func() {
				_ = mapCloneError(&command.ErrPreflightUnreachable{Err: &jira.StatusError{StatusCode: status, Body: "untrusted"}})
			})
			result := captureStdout(t, func() {
				printCloneResult(&command.CloneResult{SourceKey: "PROJ-123", NewKey: "PROJ-456", LinkErr: &jira.StatusError{StatusCode: status, Body: "untrusted"}})
			})
			marker := fmt.Sprintf("(%d)", status)
			for name, output := range map[string]string{"preflight": preflight, "result": result} {
				if strings.Contains(output, "untrusted") {
					t.Errorf("%s output leaked response body: %q", name, output)
				}
				if strings.Contains(output, marker) != wantStatus {
					t.Errorf("%s output = %q, status display = %t, want %t", name, output, strings.Contains(output, marker), wantStatus)
				}
			}
		})
	}
}

func TestRefreshCredentialsFailure_HTTPStatusBoundaries(t *testing.T) {
	for _, status := range []int{99, 100, 599, 600} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			var got error
			out := captureStderr(t, func() { got = refreshCredentialsFailure(&auth.RefreshStatusError{StatusCode: status}) })
			marker := fmt.Sprintf("(%d)", status)
			wantStatus := status >= 100 && status <= 599
			if strings.Contains(got.Error(), marker) != wantStatus || strings.Contains(out, marker) != wantStatus {
				t.Errorf("error=%q stderr=%q, status display want %t", got, out, wantStatus)
			}
		})
	}
}

func TestRunClone_ExpiredOAuthRefreshTransportFailureDoesNotExposeSecret(t *testing.T) {
	const secret = "provider-to-cli-refresh-secret-must-not-leak"
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderOAuth,
		Site:     "acme.atlassian.net",
		CloudID:  "cloud-123",
		OAuth: &auth.OAuthConfig{
			AccessToken:  "expired",
			RefreshToken: "refresh",
			ExpiresAt:    time.Now().Add(-time.Hour),
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	originalClientID, originalTransport := auth.OAuthClientID, http.DefaultTransport
	auth.OAuthClientID = "test-client"
	defer func() {
		auth.OAuthClientID = originalClientID
		http.DefaultTransport = originalTransport
	}()

	for _, tt := range []struct {
		name      string
		transport http.RoundTripper
	}{
		{
			name: "transport",
			transport: cloneRoundTripper(func(*http.Request) (*http.Response, error) {
				return nil, errors.New(secret)
			}),
		},
		{
			name: "response read",
			transport: cloneRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       cloneFailingReadCloser{err: errors.New(secret)},
					Header:     make(http.Header),
				}, nil
			}),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			http.DefaultTransport = tt.transport
			var err error
			stderr := captureStderr(t, func() { err = runClone([]string{"PROJ-123"}) })
			if err == nil {
				t.Fatal("runClone: expected refresh failure")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(stderr, secret) {
				t.Errorf("provider-to-CLI refresh failure exposes secret: error=%q stderr=%q", err, stderr)
			}
			if !strings.Contains(stderr, "could not refresh Jira credentials") {
				t.Errorf("stderr = %q, want fixed credential-refresh failure", stderr)
			}
		})
	}
}

func TestRunClone_ExpiredOAuthRefreshStatusPreservesOnlyStatus(t *testing.T) {
	const secret = "refresh-response-secret-must-not-leak"
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.Save("", &auth.Config{
		Provider: auth.ProviderOAuth, Site: "acme.atlassian.net", CloudID: "cloud-123",
		OAuth: &auth.OAuthConfig{AccessToken: "expired", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Hour)},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	originalClientID, originalTransport := auth.OAuthClientID, http.DefaultTransport
	auth.OAuthClientID = "test-client"
	http.DefaultTransport = cloneRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(secret)), Header: make(http.Header)}, nil
	})
	defer func() { auth.OAuthClientID, http.DefaultTransport = originalClientID, originalTransport }()

	var err error
	stderr := captureStderr(t, func() { err = runClone([]string{"PROJ-123"}) })
	if err == nil || !strings.Contains(stderr, "could not refresh Jira credentials (400)") {
		t.Fatalf("error=%v stderr=%q, want safe refresh status", err, stderr)
	}
	if strings.Contains(stderr, secret) || strings.Contains(err.Error(), secret) {
		t.Errorf("refresh output leaked response body: error=%q stderr=%q", err, stderr)
	}
}

func TestLabelsLine_NoSwap(t *testing.T) {
	res := &command.CloneResult{Labels: []string{"FY26-Q3", "team-widgets"}}
	got := labelsLine(res)
	want := "Labels carried over unchanged: FY26-Q3, team-widgets."
	if got != want {
		t.Errorf("labelsLine = %q, want %q", got, want)
	}
}

func TestLabelsLine_NoSwap_NoLabels(t *testing.T) {
	res := &command.CloneResult{Labels: nil}
	got := labelsLine(res)
	want := "No labels."
	if got != want {
		t.Errorf("labelsLine = %q, want %q", got, want)
	}
}

func TestLabelsLine_Swap_OneOther(t *testing.T) {
	res := &command.CloneResult{
		LabelSwapped: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
		OtherLabels: []string{"team-widgets"},
	}
	got := labelsLine(res)
	want := "Labels: FY26-Q3 -> FY26-Q4 (1 other label carried over unchanged: team-widgets)."
	if got != want {
		t.Errorf("labelsLine = %q, want %q", got, want)
	}
}

func TestLabelsLine_Swap_NoOthers(t *testing.T) {
	res := &command.CloneResult{LabelSwapped: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4"}
	got := labelsLine(res)
	want := "Labels: FY26-Q3 -> FY26-Q4 (no other labels)."
	if got != want {
		t.Errorf("labelsLine = %q, want %q", got, want)
	}
}

func TestLabelsLine_Swap_MultipleOthers(t *testing.T) {
	res := &command.CloneResult{
		LabelSwapped: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4",
		OtherLabels: []string{"team-widgets", "team-gadgets"},
	}
	got := labelsLine(res)
	want := "Labels: FY26-Q3 -> FY26-Q4 (2 other labels carried over unchanged: team-widgets, team-gadgets)."
	if got != want {
		t.Errorf("labelsLine = %q, want %q", got, want)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	var out []byte
	var readErr error
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		out, readErr = io.ReadAll(r)
	}()

	defer func() {
		os.Stdout = orig
		_ = w.Close()
		readers.Wait()
		_ = r.Close()
	}()
	os.Stdout = w
	fn()
	_ = w.Close()
	readers.Wait()
	if readErr != nil {
		t.Fatalf("read captured stdout: %v", readErr)
	}
	return string(out)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	var out []byte
	var readErr error
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		out, readErr = io.ReadAll(r)
	}()

	defer func() {
		os.Stderr = orig
		_ = w.Close()
		readers.Wait()
		_ = r.Close()
	}()
	os.Stderr = w
	fn()
	_ = w.Close()
	readers.Wait()
	if readErr != nil {
		t.Fatalf("read captured stderr: %v", readErr)
	}
	return string(out)
}

func countOpenFDs(t *testing.T) (int, bool) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, false
	}
	return len(entries), true
}

func assertCapturePanicSafe(t *testing.T, name string, stream **os.File, capture func(*testing.T, func()) string) {
	t.Helper()
	orig := *stream
	before, fdCountOK := countOpenFDs(t)
	if !fdCountOK {
		t.Logf("%s: /proc/self/fd unavailable on this platform; running the stream-restore assertion only, fd-leak check skipped", name)
	}

	const iterations = 16
	for i := 0; i < iterations; i++ {
		func() {
			defer func() {
				if rec := recover(); rec == nil {
					t.Fatalf("%s: panic in fn did not propagate out of the helper (iteration %d)", name, i)
				}
			}()
			capture(t, func() { panic("B0003 regression: boom") })
		}()
		if *stream != orig {
			*stream = orig
			t.Fatalf("%s: global stream not restored after panic (iteration %d)", name, i)
		}
	}

	after, _ := countOpenFDs(t)
	if !fdCountOK {
		t.Logf("%s: fd-leak assertion skipped (no /proc/self/fd); %d panicking captures completed with the stream restored each time", name, iterations)
		return
	}
	if after > before {
		t.Errorf("%s: descriptor leak across %d panicking captures: %d open before, %d after",
			name, iterations, before, after)
	}
}

func assertCaptureNoLeakOnNormalReturn(t *testing.T, name string, capture func(*testing.T, func()) string, emit func()) {
	t.Helper()
	before, fdCountOK := countOpenFDs(t)
	if !fdCountOK {
		t.Logf("%s: /proc/self/fd unavailable on this platform; normal-return fd-leak check skipped", name)
		return
	}

	const iterations = 32
	for i := 0; i < iterations; i++ {
		if got := capture(t, emit); got != "x" {
			t.Fatalf("%s: capture returned %q, want %q (iteration %d)", name, got, "x", i)
		}
	}

	if after, _ := countOpenFDs(t); after > before {
		t.Errorf("%s: descriptor leak across %d normal-return captures: %d open before, %d after",
			name, iterations, before, after)
	}
}

func TestCaptureStdout_PanicRestoresStreamAndClosesFDs(t *testing.T) {
	assertCapturePanicSafe(t, "captureStdout", &os.Stdout, captureStdout)
}

func TestCaptureStderr_PanicRestoresStreamAndClosesFDs(t *testing.T) {
	assertCapturePanicSafe(t, "captureStderr", &os.Stderr, captureStderr)
}

func TestCaptureStdout_NormalReturnClosesReadEnd(t *testing.T) {
	assertCaptureNoLeakOnNormalReturn(t, "captureStdout", captureStdout, func() { fmt.Print("x") })
}

func TestCaptureStderr_NormalReturnClosesReadEnd(t *testing.T) {
	assertCaptureNoLeakOnNormalReturn(t, "captureStderr", captureStderr, func() { fmt.Fprint(os.Stderr, "x") })
}

func TestCaptureStdout_LargeOutput(t *testing.T) {
	want := strings.Repeat("stdout large output\n", 1<<16)
	if got := captureStdout(t, func() { fmt.Print(want) }); got != want {
		t.Errorf("captureStdout large output mismatch: got %d bytes, want %d", len(got), len(want))
	}
}

func TestCaptureStderr_LargeOutput(t *testing.T) {
	want := strings.Repeat("stderr large output\n", 1<<16)
	if got := captureStderr(t, func() { fmt.Fprint(os.Stderr, want) }); got != want {
		t.Errorf("captureStderr large output mismatch: got %d bytes, want %d", len(got), len(want))
	}
}

func assertCaptureLargeOutputPanicSafe(t *testing.T, name string, stream **os.File, capture func(*testing.T, func()) string, emit func()) {
	t.Helper()
	orig := *stream
	func() {
		defer func() {
			if rec := recover(); rec == nil {
				t.Fatalf("%s: panic in fn did not propagate out of the helper", name)
			}
		}()
		capture(t, func() {
			emit()
			panic("B0006 regression: panic after large output")
		})
	}()
	if *stream != orig {
		*stream = orig
		t.Fatalf("%s: global stream not restored after panic following large output", name)
	}
}

func TestCaptureStdout_LargeOutputPanicRestoresStreamAndJoinsReader(t *testing.T) {
	large := strings.Repeat("stdout large output before panic\n", 1<<16)
	assertCaptureLargeOutputPanicSafe(t, "captureStdout", &os.Stdout, captureStdout, func() { fmt.Print(large) })
}

func TestCaptureStderr_LargeOutputPanicRestoresStreamAndJoinsReader(t *testing.T) {
	large := strings.Repeat("stderr large output before panic\n", 1<<16)
	assertCaptureLargeOutputPanicSafe(t, "captureStderr", &os.Stderr, captureStderr, func() { fmt.Fprint(os.Stderr, large) })
}

func TestMapCloneError_SanitizesStderr(t *testing.T) {
	const poison = "\x1b[31mHIDDEN\x1b[0m\r\nInjected forged line"

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "ErrPreflightUnreachable wrapping a StatusError body",
			err:  &command.ErrPreflightUnreachable{Err: &jira.StatusError{StatusCode: 500, Body: poison}},
		},
		{
			name: "ErrCreateFailed wrapping a StatusError body",
			err:  &command.ErrCreateFailed{SourceKey: "PROJ-123", Err: &jira.StatusError{StatusCode: 500, Body: poison}},
		},
		{
			name: "ErrLabelNotFound with a poisoned existing label",
			err:  &command.ErrLabelNotFound{SourceKey: "PROJ-123", Label: "FY26-Q2", Labels: []string{"team-widgets" + poison}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStderr(t, func() { _ = mapCloneError(tt.err) })

			if strings.Contains(out, "\x1b") {
				t.Errorf("stderr contains an unsanitized escape sequence: %q", out)
			}
			if strings.Contains(out, "\r") {
				t.Errorf("stderr contains an unsanitized carriage return: %q", out)
			}
			if want := 1; strings.Count(out, "\n") != want {
				t.Errorf("stderr has %d lines, want %d (a CR/LF in Jira-controlled text must not add a line): %q", strings.Count(out, "\n"), want, out)
			}
		})
	}
}

func TestRunClone_UnknownFlag_SanitizesStderr(t *testing.T) {
	const poison = "--\x1b[31mBOGUS\x1b[0m\r\nInjected forged line"

	out := captureStderr(t, func() { _ = runClone([]string{"PROJ-123", poison}) })

	if strings.Contains(out, "\x1b") {
		t.Errorf("stderr contains an unsanitized escape sequence: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("stderr contains an unsanitized carriage return: %q", out)
	}
	if want := 1; strings.Count(out, "\n") != want {
		t.Errorf("stderr has %d lines, want %d (the flag package's own dump must be suppressed and the message must print exactly once): %q", strings.Count(out, "\n"), want, out)
	}
}

func TestRunClone_Help(t *testing.T) {
	for _, flagForm := range []string{"-h", "--help"} {
		t.Run(flagForm, func(t *testing.T) {
			var err error
			var stderrOut string
			stdoutOut := captureStdout(t, func() {
				stderrOut = captureStderr(t, func() {
					err = runClone([]string{"PROJ-123", flagForm})
				})
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("err = %v, want errors.Is(err, flag.ErrHelp)", err)
			}
			for _, want := range []string{"-summary-old value", "-summary-new value", "-label-old value", "-label-new value", "-parent value"} {
				if !strings.Contains(stdoutOut, want) {
					t.Errorf("stdout = %q, want it to contain %q (clone's option listing)", stdoutOut, want)
				}
			}
			if stderrOut != "" {
				t.Errorf("stderr = %q, want empty (help is not a failure)", stderrOut)
			}
		})
	}
}

func TestPrintCloneResult_FullSuccess(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q4'26",
		LabelSwapped: true, LabelOld: "FY26-Q3", LabelNew: "FY26-Q4", OtherLabels: []string{"team-widgets"},
		ParentAttempted: true, ParentKey: "PROJ-001", ParentType: "Initiative",
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q4'26").
Linked PROJ-456 as a clone of PROJ-123 (Cloners link).
Labels: FY26-Q3 -> FY26-Q4 (1 other label carried over unchanged: team-widgets).
Parent set: PROJ-456 -> PROJ-001 (Initiative).
Done.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_MinimalSuccess(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26",
		Labels: []string{"FY26-Q3", "team-widgets"},
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26").
Linked PROJ-456 as a clone of PROJ-123 (Cloners link).
Labels carried over unchanged: FY26-Q3, team-widgets.
No parent set.
Done.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_SanitizesJiraControlledStrings(t *testing.T) {
	const poison = "\x1b[31mHIDDEN\x1b[0m\r\nInjected: PROJ-999 (Epic: \"forged\")."
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout" + poison,
		NewKey: "PROJ-456", NewSummary: "Widget rollout" + poison,
		Labels: []string{"team-widgets" + poison},
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	if strings.Contains(out, "\x1b") {
		t.Errorf("output contains an unsanitized escape sequence: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("output contains an unsanitized carriage return: %q", out)
	}
	if want := 6; strings.Count(out, "\n") != want {
		t.Errorf("output has %d lines, want %d (a bare LF in Jira-controlled text must not add a line): %q", strings.Count(out, "\n"), want, out)
	}
}

func TestPrintCloneResult_StripsBidiFormatControls(t *testing.T) {
	const poison = "safe\u202Eforged\u2067text"
	out := captureStdout(t, func() {
		printCloneResult(&command.CloneResult{SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: poison, NewKey: "PROJ-456", NewSummary: poison, Labels: []string{poison}})
	})
	if strings.ContainsAny(out, "\u202e\u2067") {
		t.Errorf("output contains bidi format controls: %q", out)
	}
	if !strings.Contains(out, "safeforgedtext") {
		t.Errorf("output lost visible Jira text: %q", out)
	}
}

func TestPrintCloneResult_SanitizesKeysAndSwapLabels(t *testing.T) {
	const poison = "\x1b[31mX\x1b[0m\r\nFORGED\u009bOSC"
	res := &command.CloneResult{
		SourceKey: poison, NewKey: poison, SourceType: "Epic", SourceSummary: "summary", NewSummary: "summary",
		LabelSwapped: true, LabelOld: poison, LabelNew: poison,
		ParentAttempted: true, ParentKey: poison, ParentType: "Initiative",
		LinkErr: errors.New("network"), ParentErr: errors.New("network"),
	}
	out := captureStdout(t, func() { printCloneResult(res) })
	for _, control := range []string{"\x1b", "\r", "\u009b"} {
		if strings.Contains(out, control) {
			t.Errorf("output contains control %q: %q", control, out)
		}
	}
	if want := 5; strings.Count(out, "\n") != want {
		t.Errorf("output has %d lines, want %d: %q", strings.Count(out, "\n"), want, out)
	}
}

func TestPrintCloneResult_SanitizesLinkErr(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout",
		NewKey:  "PROJ-456",
		LinkErr: errors.New("jira: unexpected status 500: \x1b[31mfake terminal takeover\x1b[0m\r\nDone.\nExtra forged line"),
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	if strings.Contains(out, "\x1b") {
		t.Errorf("output contains an unsanitized escape sequence: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("output contains an unsanitized carriage return: %q", out)
	}
	if want := 5; strings.Count(out, "\n") != want {
		t.Errorf("output has %d lines, want %d (a CR/LF in LinkErr text must not add or overwrite a line): %q", strings.Count(out, "\n"), want, out)
	}
}

func TestPrintCloneResult_LinkFails_NoParent(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26",
		Labels:  []string{"FY26-Q3", "team-widgets"},
		LinkErr: errors.New("network error"),
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26").
Could not link PROJ-456 to PROJ-123 as a clone: could not reach Jira. Add a "Cloners" link between PROJ-456 and PROJ-123 by hand, or via the Jira web UI, to fix it.
Labels carried over unchanged: FY26-Q3, team-widgets.
No parent set.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_LinkFails_ParentSucceeds(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26",
		Labels:          []string{"FY26-Q3", "team-widgets"},
		LinkErr:         errors.New("network error"),
		ParentAttempted: true, ParentKey: "PROJ-001", ParentType: "Initiative",
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26").
Could not link PROJ-456 to PROJ-123 as a clone: could not reach Jira. Add a "Cloners" link between PROJ-456 and PROJ-123 by hand, or via the Jira web UI, to fix it.
Labels carried over unchanged: FY26-Q3, team-widgets.
Parent set: PROJ-456 -> PROJ-001 (Initiative).
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_ParentRejected(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26",
		Labels:          []string{"FY26-Q3", "team-widgets"},
		ParentAttempted: true, ParentKey: "PROJ-001",
		ParentErr: errors.New("issue type cannot be a child of this parent"),
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26").
Linked PROJ-456 as a clone of PROJ-123 (Cloners link).
Labels carried over unchanged: FY26-Q3, team-widgets.
Could not set PROJ-001 as PROJ-456's parent: could not reach Jira. Set one with the parent/child link primitive once you have a valid target.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_LinkFailsAndParentFails(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26",
		Labels:          []string{"FY26-Q3", "team-widgets"},
		LinkErr:         errors.New("network error"),
		ParentAttempted: true, ParentKey: "PROJ-001",
		ParentErr: errors.New("issue type cannot be a child of this parent"),
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26").
Could not link PROJ-456 to PROJ-123 as a clone: could not reach Jira. Add a "Cloners" link between PROJ-456 and PROJ-123 by hand, or via the Jira web UI, to fix it.
Labels carried over unchanged: FY26-Q3, team-widgets.
Could not set PROJ-001 as PROJ-456's parent: could not reach Jira. Set one with the parent/child link primitive once you have a valid target.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_Part_Found(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout [Part 2]",
		NewKey: "PROJ-456", NewSummary: "Widget rollout [Part 3]",
		Labels:        []string{"FY26-Q3"},
		PartRequested: true, PartFound: true, PartFoundMarker: "[Part 2]", PartCloneMarker: "[Part 3]",
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout [Part 2]")...
Created PROJ-456 (Epic: "Widget rollout [Part 3]").
Linked PROJ-456 as a clone of PROJ-123 (Cloners link).
Labels carried over unchanged: FY26-Q3.
No parent set.
Part numbering: "[Part 2]" -> "[Part 3]".
Done.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_Part_NotFound_Success(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26 [Part 2]",
		Labels:        []string{"FY26-Q3"},
		PartRequested: true, PartFound: false, PartSourceMarker: "[Part 1]", PartCloneMarker: "[Part 2]",
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26 [Part 2]").
Linked PROJ-456 as a clone of PROJ-123 (Cloners link).
Labels carried over unchanged: FY26-Q3.
No parent set.
Part numbering: source unnumbered, source is now "[Part 1]", clone is "[Part 2]".
Done.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintCloneResult_Part_NotFound_SourceWriteFails(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26 [Part 2]",
		Labels:        []string{"FY26-Q3"},
		PartRequested: true, PartFound: false, PartSourceMarker: "[Part 1]", PartCloneMarker: "[Part 2]",
		PartSourceErr: errors.New("network error"),
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26 [Part 2]").
Linked PROJ-456 as a clone of PROJ-123 (Cloners link).
Labels carried over unchanged: FY26-Q3.
No parent set.
Could not mark PROJ-123 as "[Part 1]": could not reach Jira. Clone is "[Part 2]"; add "[Part 1]" to PROJ-123's summary by hand if you want it recorded.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	if strings.Contains(out, "Done.") {
		t.Error("output contains \"Done.\", want none once the source write has failed")
	}
}

func TestPrintCloneResult_Part_NotRequested_NoExtraLine(t *testing.T) {
	res := &command.CloneResult{
		SourceKey: "PROJ-123", SourceType: "Epic", SourceSummary: "Widget rollout Q3'26",
		NewKey: "PROJ-456", NewSummary: "Widget rollout Q3'26",
		Labels: []string{"FY26-Q3"},
	}
	out := captureStdout(t, func() { printCloneResult(res) })

	if strings.Contains(out, "Part numbering") {
		t.Errorf("output contains a part-numbering line when --part wasn't given: %q", out)
	}
	want := `Cloning PROJ-123 (Epic: "Widget rollout Q3'26")...
Created PROJ-456 (Epic: "Widget rollout Q3'26").
Linked PROJ-456 as a clone of PROJ-123 (Cloners link).
Labels carried over unchanged: FY26-Q3.
No parent set.
Done.
`
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
}
