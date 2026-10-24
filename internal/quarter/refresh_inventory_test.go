package quarter

import (
	"context"
	"errors"
	"testing"
)

func refreshEnv(t *testing.T) {
	t.Helper()
	withConfigHome(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"Q1'26"}}`)
}

func TestRefreshInventory_FetchesAndReplacesCache(t *testing.T) {
	refreshEnv(t)
	ctx := context.Background()

	old := &fakeSearcher{issues: []QuarterIssue{{Key: "PROJ-1", Summary: "old"}}}
	if _, err := Inventory(ctx, old, ""); err != nil {
		t.Fatal(err)
	}

	fresh := &fakeSearcher{issues: []QuarterIssue{{Key: "PROJ-1", Summary: "old"}, {Key: "PROJ-2", Summary: "new"}}}
	got, err := RefreshInventory(ctx, fresh, "")
	if err != nil || len(got) != 2 {
		t.Fatalf("RefreshInventory = %v, %v", got, err)
	}
	if fresh.calls != 1 || fresh.probeCalls != 0 {
		t.Errorf("searches = %d, probes = %d, want 1, 0", fresh.calls, fresh.probeCalls)
	}

	warm := &fakeSearcher{}
	got, err = Inventory(ctx, warm, "")
	if err != nil || len(got) != 2 || warm.calls != 0 {
		t.Errorf("Inventory after refresh = %v, %v, searches = %d; want replaced copy, 0 searches", got, err, warm.calls)
	}
}

func TestInventory_WithoutRefreshUsesCache(t *testing.T) {
	refreshEnv(t)
	ctx := context.Background()
	f := &fakeSearcher{issues: []QuarterIssue{{Key: "PROJ-1", Summary: "x"}}}
	for i := 0; i < 2; i++ {
		if _, err := Inventory(ctx, f, ""); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls != 1 {
		t.Errorf("searches = %d, want 1 (second call served from cache)", f.calls)
	}
}

func TestRefreshInventory_FailedFetchKeepsOldCache(t *testing.T) {
	refreshEnv(t)
	ctx := context.Background()
	if _, err := Inventory(ctx, &fakeSearcher{issues: []QuarterIssue{{Key: "PROJ-1", Summary: "old"}}}, ""); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if _, err := RefreshInventory(ctx, &fakeSearcher{err: boom, issues: []QuarterIssue{{Key: "PARTIAL-1"}}}, ""); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	warm := &fakeSearcher{}
	got, err := Inventory(ctx, warm, "")
	if err != nil || len(got) != 1 || got[0].Key != "PROJ-1" || warm.calls != 0 {
		t.Errorf("after failed refresh: %v, %v, searches = %d; want old cache", got, err, warm.calls)
	}
}

func TestRefreshInventory_UnsetSettings(t *testing.T) {
	withConfigHome(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettings(t, `{"defaults":{"current_quarter":"Q1'26"}}`)
	f := &fakeSearcher{}
	_, err := RefreshInventory(context.Background(), f, "")
	var unset *ErrSettingUnset
	if !errors.As(err, &unset) || unset.Setting != "defaults.project" || f.calls != 0 {
		t.Errorf("err = %v, calls = %d", err, f.calls)
	}
}
