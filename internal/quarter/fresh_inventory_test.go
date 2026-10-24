package quarter

import (
	"context"
	"errors"
	"testing"
)

func TestFreshInventory_AlwaysSearches(t *testing.T) {
	withConfigHome(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"Q1'26"}}`)

	f := &fakeSearcher{issues: []QuarterIssue{{Key: "PROJ-1", Summary: "x"}}}
	for i := 0; i < 2; i++ {
		got, err := FreshInventory(context.Background(), f, "")
		if err != nil || len(got) != 1 {
			t.Fatalf("FreshInventory = %v, %v", got, err)
		}
	}
	if f.calls != 2 || f.probeCalls != 0 || f.gotProj != "PROJ" || f.gotQtr != "Q1'26" {
		t.Errorf("calls = %d, probes = %d, proj = %q, qtr = %q", f.calls, f.probeCalls, f.gotProj, f.gotQtr)
	}
}

func TestFreshInventory_UnsetSettings(t *testing.T) {
	for raw, want := range map[string]string{
		`{"defaults":{"current_quarter":"Q1'26"}}`: "defaults.project",
		`{"defaults":{"project":"PROJ"}}`:          "defaults.current_quarter",
	} {
		withConfigHome(t)
		writeSettings(t, raw)
		f := &fakeSearcher{}
		_, err := FreshInventory(context.Background(), f, "")
		var unset *ErrSettingUnset
		if !errors.As(err, &unset) || unset.Setting != want || f.calls != 0 {
			t.Errorf("err = %v, calls = %d, want ErrSettingUnset %s", err, f.calls, want)
		}
	}
}
