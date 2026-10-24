package main

import (
	"context"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/plan"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

func TestNewPlanClient_RefreshChoosesInventory(t *testing.T) {
	quarterListLogin(t)
	writeSettingsFile(t, planSettings)
	creds := auth.Credentials{BaseURL: "https://acme.atlassian.net", AuthHeader: "Basic x"}
	ctx := context.Background()
	f := &fakeQuarterSearcher{issues: quarterListSample()[:1]}
	stubQuarterSearcher(t, f)

	inv := func(refresh bool) []quarter.QuarterIssue {
		t.Helper()
		got, err := newPlanClient(creds, "", refresh).Inventory(ctx)
		if err != nil {
			t.Fatalf("Inventory(refresh=%v): %v", refresh, err)
		}
		return got
	}

	if got := inv(false); len(got) != 1 || f.calls != 1 {
		t.Fatalf("cold: items = %d, fetches = %d; want 1, 1", len(got), f.calls)
	}
	if got := inv(false); len(got) != 1 || f.calls != 1 {
		t.Fatalf("warm no flag: items = %d, fetches = %d; want 1, 1 (no new fetch)", len(got), f.calls)
	}

	f.issues = quarterListSample()
	if got := inv(true); len(got) != len(f.issues) || f.calls != 2 {
		t.Fatalf("refresh: items = %d, fetches = %d; want %d, 2", len(got), f.calls, len(f.issues))
	}
	if got := inv(false); len(got) != len(f.issues) || f.calls != 2 {
		t.Fatalf("after refresh: items = %d, fetches = %d; want %d, 2 (cache replaced)", len(got), f.calls, len(f.issues))
	}
}

func TestRunPlanContext_RefreshFlagReachesClient(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want bool
	}{
		{"without flag", []string{"context", "--day"}, false},
		{"with flag", []string{"context", "--day", "--refresh"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			quarterListLogin(t)
			writeSettingsFile(t, planSettings)
			orig := newPlanClient
			t.Cleanup(func() { newPlanClient = orig })
			calls := 0
			var got bool
			newPlanClient = func(_ auth.Credentials, _ string, refresh bool) plan.Client {
				calls++
				got = refresh
				return sampleClient()
			}
			var err error
			captureStdout(t, func() { _ = captureStderr(t, func() { err = runPlan(c.args) }) })
			if err != nil {
				t.Fatalf("runPlan(%v) = %v; want nil", c.args, err)
			}
			if calls != 1 || got != c.want {
				t.Errorf("newPlanClient calls = %d, refresh = %v; want 1, %v", calls, got, c.want)
			}
		})
	}
}
