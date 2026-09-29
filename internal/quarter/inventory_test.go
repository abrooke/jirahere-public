package quarter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
)

func withConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func writeSettings(t *testing.T, raw string) {
	t.Helper()
	dir, err := auth.ConfigDir("")
	if err != nil {
		t.Fatalf("auth.ConfigDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile settings.json: %v", err)
	}
}

type fakeSearcher struct {
	calls   int
	gotProj string
	gotQtr  string
	issues  []QuarterIssue
	err     error

	probeCalls   int
	probeUpdated time.Time
	probeFound   bool
	probeErr     error
}

func (f *fakeSearcher) SearchQuarterIssues(_ context.Context, project, quarter string) ([]QuarterIssue, error) {
	f.calls++
	f.gotProj = project
	f.gotQtr = quarter
	return f.issues, f.err
}

func (f *fakeSearcher) LatestQuarterUpdate(_ context.Context, _, _ string) (time.Time, bool, error) {
	f.probeCalls++
	return f.probeUpdated, f.probeFound, f.probeErr
}

var tz = time.FixedZone("MST", -7*60*60)

func sampleIssues() []QuarterIssue {
	return []QuarterIssue{
		{
			Key:     "PROJ-1",
			Summary: "first",
			Status:  jira.Status{ID: "10001", Name: "In Progress"},
			Created: time.Date(2026, 1, 2, 15, 4, 5, 123_000_000, tz),
			Updated: time.Date(2026, 2, 3, 9, 8, 7, 456_000_000, tz),
		},
		{
			Key:     "PROJ-2",
			Summary: "second",
			Status:  jira.Status{ID: "10002", Name: "Done"},
			Created: time.Date(2026, 3, 4, 1, 2, 3, 789_000_000, tz),
			Updated: time.Date(2026, 1, 9, 0, 0, 0, 0, tz),
		},
	}
}

func assertIssuesEqual(t *testing.T, got, want []QuarterIssue) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Key != want[i].Key || got[i].Summary != want[i].Summary {
			t.Errorf("record %d = {%q %q}, want {%q %q}", i, got[i].Key, got[i].Summary, want[i].Key, want[i].Summary)
		}
		if got[i].Status != want[i].Status {
			t.Errorf("record %d Status = %+v, want %+v", i, got[i].Status, want[i].Status)
		}
		if !got[i].Created.Equal(want[i].Created) {
			t.Errorf("record %d Created = %s, want %s", i, got[i].Created, want[i].Created)
		}
		if !got[i].Updated.Equal(want[i].Updated) {
			t.Errorf("record %d Updated = %s, want %s", i, got[i].Updated, want[i].Updated)
		}
	}
}

func TestInventory_ColdPopulate(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()
	want := sampleIssues()
	f := &fakeSearcher{issues: want}

	got, err := inventory(context.Background(), f, cacheDir, "")
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	assertIssuesEqual(t, got, want)

	if f.calls != 1 {
		t.Errorf("search calls = %d, want 1", f.calls)
	}
	if f.gotProj != "PROJ" || f.gotQtr != "FY26-Q1" {
		t.Errorf("search args = (%q, %q), want (PROJ, FY26-Q1)", f.gotProj, f.gotQtr)
	}

	path := filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q1"))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("cache file mode = %o, want 600", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile cache: %v", err)
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("cache file is not valid JSON: %v", err)
	}
	if cf.Project != "PROJ" || cf.Quarter != "FY26-Q1" {
		t.Errorf("cache pair = (%q, %q), want (PROJ, FY26-Q1)", cf.Project, cf.Quarter)
	}
	if len(cf.Records) != 2 {
		t.Fatalf("cache records = %d, want 2", len(cf.Records))
	}

	wantMaxCreated := want[1].Created.Format(cacheTimeLayout)
	wantMaxUpdated := want[0].Updated.Format(cacheTimeLayout)
	if cf.MaxCreated != wantMaxCreated {
		t.Errorf("max_created = %q, want %q", cf.MaxCreated, wantMaxCreated)
	}
	if cf.MaxUpdated != wantMaxUpdated {
		t.Errorf("max_updated = %q, want %q", cf.MaxUpdated, wantMaxUpdated)
	}
	if cf.Records[0].StatusID != want[0].Status.ID || cf.Records[0].StatusName != want[0].Status.Name {
		t.Errorf("record[0] status = {%q %q}, want {%q %q}", cf.Records[0].StatusID, cf.Records[0].StatusName, want[0].Status.ID, want[0].Status.Name)
	}

	if cf.Records[0].Created != "2026-01-02T15:04:05.123-07:00" {
		t.Errorf("record[0].created = %q, want RFC3339 with millis and offset", cf.Records[0].Created)
	}
}

func TestInventory_WarmServe(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()
	want := sampleIssues()

	if _, err := inventory(context.Background(), &fakeSearcher{issues: want}, cacheDir, ""); err != nil {
		t.Fatalf("cold inventory: %v", err)
	}

	warm := &fakeSearcher{err: errors.New("search must not be called on the warm path")}
	got, err := inventory(context.Background(), warm, cacheDir, "")
	if err != nil {
		t.Fatalf("warm inventory: %v", err)
	}
	if warm.calls != 0 {
		t.Fatalf("warm path invoked the search %d time(s), want 0", warm.calls)
	}
	assertIssuesEqual(t, got, want)
}

func TestInventory_UnsetProject(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"current_quarter":"FY26-Q1"}}`)

	_, err := inventory(context.Background(), &fakeSearcher{}, t.TempDir(), "")
	var unset *ErrSettingUnset
	if !errors.As(err, &unset) {
		t.Fatalf("err = %v, want *ErrSettingUnset", err)
	}
	if unset.Setting != "defaults.project" {
		t.Errorf("Setting = %q, want defaults.project", unset.Setting)
	}
}

func TestInventory_UnsetQuarter(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ"}}`)

	_, err := inventory(context.Background(), &fakeSearcher{}, t.TempDir(), "")
	var unset *ErrSettingUnset
	if !errors.As(err, &unset) {
		t.Fatalf("err = %v, want *ErrSettingUnset", err)
	}
	if unset.Setting != "defaults.current_quarter" {
		t.Errorf("Setting = %q, want defaults.current_quarter", unset.Setting)
	}
}

func TestInventory_CorruptCacheFallback(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q1"))
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt cache: %v", err)
	}

	want := sampleIssues()
	f := &fakeSearcher{issues: want}
	got, err := inventory(context.Background(), f, cacheDir, "")
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("search calls = %d, want 1 (corrupt cache is a miss)", f.calls)
	}
	assertIssuesEqual(t, got, want)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile rewritten cache: %v", err)
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("cache not rewritten as valid JSON: %v", err)
	}
}

func TestInventory_WrongProjectKeyIgnored(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stale := cacheFile{
		Records:    []cacheRecord{{Key: "OTHER-9", Summary: "stale", Created: "2020-01-01T00:00:00.000Z", Updated: "2020-01-01T00:00:00.000Z"}},
		MaxCreated: "2020-01-01T00:00:00.000Z",
		MaxUpdated: "2020-01-01T00:00:00.000Z",
		Project:    "OTHER",
		Quarter:    "FY26-Q1",
	}
	raw, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("Marshal stale: %v", err)
	}
	path := filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q1"))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile stale cache: %v", err)
	}

	want := sampleIssues()
	f := &fakeSearcher{issues: want}
	got, err := inventory(context.Background(), f, cacheDir, "")
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("search calls = %d, want 1 (wrong-key cache is a miss)", f.calls)
	}
	assertIssuesEqual(t, got, want)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile rewritten cache: %v", err)
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("Unmarshal rewritten cache: %v", err)
	}
	if cf.Project != "PROJ" {
		t.Errorf("rewritten cache Project = %q, want PROJ", cf.Project)
	}
}

func TestInventory_SearchErrorNotCached(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()

	sentinel := errors.New("boom")
	f := &fakeSearcher{issues: sampleIssues(), err: sentinel}
	_, err := inventory(context.Background(), f, cacheDir, "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q1"))); !os.IsNotExist(statErr) {
		t.Errorf("cache file written despite search error (stat err = %v)", statErr)
	}
}

func TestInventory_EmptyResult(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()

	cold := &fakeSearcher{issues: []QuarterIssue{}}
	got, err := inventory(context.Background(), cold, cacheDir, "")
	if err != nil {
		t.Fatalf("cold inventory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d records, want 0", len(got))
	}

	warm := &fakeSearcher{err: errors.New("must not be called")}
	got, err = inventory(context.Background(), warm, cacheDir, "")
	if err != nil {
		t.Fatalf("warm inventory: %v", err)
	}
	if warm.calls != 0 {
		t.Errorf("warm path called search %d time(s), want 0", warm.calls)
	}
	if len(got) != 0 {
		t.Errorf("warm got %d records, want 0", len(got))
	}
}

func TestInventory_WarmReadPreservesOffset(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()
	want := sampleIssues()

	if _, err := inventory(context.Background(), &fakeSearcher{issues: want}, cacheDir, ""); err != nil {
		t.Fatalf("cold inventory: %v", err)
	}

	warm := &fakeSearcher{err: errors.New("search must not be called on the warm path")}
	got, err := inventory(context.Background(), warm, cacheDir, "")
	if err != nil {
		t.Fatalf("warm inventory: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("warm read returned no records")
	}
	const wantOffset = -7 * 60 * 60
	for i, rec := range got {
		if _, off := rec.Created.Zone(); off != wantOffset {
			t.Errorf("record %d Created zone offset = %d, want %d", i, off, wantOffset)
		}
		if _, off := rec.Updated.Zone(); off != wantOffset {
			t.Errorf("record %d Updated zone offset = %d, want %d", i, off, wantOffset)
		}
	}
}

func TestInventory_SingleRecordMaxima(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()

	only := QuarterIssue{
		Key:     "PROJ-7",
		Summary: "lone",
		Created: time.Date(2026, 5, 6, 7, 8, 9, 250_000_000, tz),
		Updated: time.Date(2026, 6, 7, 8, 9, 10, 750_000_000, tz),
	}
	if _, err := inventory(context.Background(), &fakeSearcher{issues: []QuarterIssue{only}}, cacheDir, ""); err != nil {
		t.Fatalf("inventory: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q1")))
	if err != nil {
		t.Fatalf("ReadFile cache: %v", err)
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("Unmarshal cache: %v", err)
	}
	if want := only.Created.Format(cacheTimeLayout); cf.MaxCreated != want {
		t.Errorf("max_created = %q, want %q", cf.MaxCreated, want)
	}
	if want := only.Updated.Format(cacheTimeLayout); cf.MaxUpdated != want {
		t.Errorf("max_updated = %q, want %q", cf.MaxUpdated, want)
	}
}

func TestCacheDir_HonorsXDGCacheHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)

	got, err := CacheDir("")
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if want := filepath.Join(base, "jirahere"); got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}

func TestInventory_UsesCacheDirFromXDG(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	want := sampleIssues()
	if _, err := Inventory(context.Background(), &fakeSearcher{issues: want}, ""); err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	path := filepath.Join(cacheHome, "jirahere", cacheFileName("PROJ", "FY26-Q1"))
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cache file not under XDG_CACHE_HOME/jirahere: %v", err)
	}
}

func mkCacheDir(t *testing.T) (parent, dir string) {
	t.Helper()
	parent = t.TempDir()
	dir = filepath.Join(parent, layout.Namespace)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	return parent, dir
}

func TestClearCache_RemovesPopulatedDir(t *testing.T) {
	_, dir := mkCacheDir(t)
	for _, name := range []string{
		"quarter-inventory-PROJ-FY26-Q1-0011223344556677.json",
		"quarter-inventory-PROJ-FY26-Q2-8899aabbccddeeff.json",
		"quarter-inventory-OTHER-FY26-Q1-0102030405060708.json",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	if err := clearCache(dir); err != nil {
		t.Fatalf("clearCache: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("cache dir still present after clearCache, Lstat err = %v", err)
	}
}

func TestClearCache_AbsentDirIsNil(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, layout.Namespace)

	if err := clearCache(dir); err != nil {
		t.Fatalf("clearCache of an absent dir: %v", err)
	}
}

func TestClearCache_GuardRejectionRemovesNothing(t *testing.T) {
	t.Run("no namespace token in resolved path", func(t *testing.T) {
		parent := t.TempDir()
		if strings.Contains(parent, layout.Namespace) {
			t.Skipf("temp dir %q unexpectedly contains %q", parent, layout.Namespace)
		}
		dir := filepath.Join(parent, "cache")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		keep := filepath.Join(dir, "quarter-inventory-x.json")
		if err := os.WriteFile(keep, []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		err := clearCache(dir)
		var outside *safedelete.ErrOutsideBase
		if !errors.As(err, &outside) {
			t.Fatalf("clearCache err = %v (%T), want *safedelete.ErrOutsideBase", err, err)
		}
		if _, statErr := os.Stat(keep); statErr != nil {
			t.Errorf("guard rejected but the cache file is gone: %v", statErr)
		}
	})

	t.Run("parent exists but does not resolve", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("running as root: directory permission bits do not block traversal")
		}
		parent := t.TempDir()
		blocked := filepath.Join(parent, "blocked")
		dir := filepath.Join(blocked, "inner", layout.Namespace)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		keep := filepath.Join(dir, "quarter-inventory-x.json")
		if err := os.WriteFile(keep, []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if err := os.Chmod(blocked, 0o000); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

		err := clearCache(dir)
		var outside *safedelete.ErrOutsideBase
		if !errors.As(err, &outside) {
			t.Fatalf("clearCache err = %v (%T), want *safedelete.ErrOutsideBase", err, err)
		}

		if err := os.Chmod(blocked, 0o700); err != nil {
			t.Fatalf("Chmod restore: %v", err)
		}
		if _, statErr := os.Stat(keep); statErr != nil {
			t.Errorf("guard rejected but the cache file is gone: %v", statErr)
		}
	})
}

func TestClearCache_AbsentCacheHomeIsNil(t *testing.T) {
	root := t.TempDir()
	cacheHome := filepath.Join(root, "cache")
	dir := filepath.Join(cacheHome, layout.Namespace)

	if err := clearCache(dir); err != nil {
		t.Fatalf("clearCache with an absent cache home: %v", err)
	}
	if _, err := os.Stat(cacheHome); !os.IsNotExist(err) {
		t.Errorf("cache home brought into existence as a side effect, Stat err = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cache dir brought into existence as a side effect, Stat err = %v", err)
	}
}

func TestClearCache_RemovalFailureIsWrapped(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits do not block unlink")
	}
	_, dir := mkCacheDir(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "quarter-inventory-x.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })

	err := clearCache(dir)
	if err == nil {
		t.Fatal("clearCache: expected a removal failure, got nil")
	}
	var outside *safedelete.ErrOutsideBase
	if errors.As(err, &outside) {
		t.Fatalf("clearCache err = %v, want a wrapped os error, not a guard rejection", err)
	}
	if !strings.Contains(err.Error(), "could not clear quarter inventory cache") {
		t.Errorf("clearCache err = %q, want it wrapped with the %q context", err, "could not clear quarter inventory cache")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("clearCache err = %v, want it to wrap os.ErrPermission", err)
	}
}

var sampleMaxUpdated = time.Date(2026, 2, 3, 9, 8, 7, 456_000_000, tz)

func warmCache(t *testing.T) (dir, path string) {
	t.Helper()
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	_, dir = mkCacheDir(t)
	if _, err := inventory(context.Background(), &fakeSearcher{issues: sampleIssues()}, dir, ""); err != nil {
		t.Fatalf("cold inventory: %v", err)
	}
	return dir, filepath.Join(dir, cacheFileName("PROJ", "FY26-Q1"))
}

func TestInventory_ProbeNewer_ClearsAndRebuilds(t *testing.T) {
	dir, path := warmCache(t)

	fresh := []QuarterIssue{{
		Key:     "PROJ-5",
		Summary: "edited in jira",
		Created: time.Date(2026, 1, 2, 15, 4, 5, 123_000_000, tz),
		Updated: time.Date(2026, 4, 1, 12, 0, 0, 0, tz),
	}}
	warm := &fakeSearcher{
		issues:       fresh,
		probeFound:   true,
		probeUpdated: fresh[0].Updated,
	}

	got, err := inventory(context.Background(), warm, dir, "")
	if err != nil {
		t.Fatalf("warm inventory: %v", err)
	}
	if warm.probeCalls != 1 {
		t.Errorf("probe calls = %d, want 1", warm.probeCalls)
	}
	if warm.calls != 1 {
		t.Errorf("SearchQuarterIssues calls = %d, want 1 (rebuild)", warm.calls)
	}
	assertIssuesEqual(t, got, fresh)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile rewritten cache: %v", err)
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("Unmarshal rewritten cache: %v", err)
	}
	if len(cf.Records) != 1 || cf.Records[0].Key != "PROJ-5" {
		t.Errorf("rewritten records = %+v, want the single PROJ-5", cf.Records)
	}
	if want := fresh[0].Updated.Format(cacheTimeLayout); cf.MaxUpdated != want {
		t.Errorf("rewritten max_updated = %q, want %q", cf.MaxUpdated, want)
	}
}

func TestInventory_ProbeNotNewer_ServesFromCache(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value time.Time
	}{
		{"equal", sampleMaxUpdated},
		{"older", sampleMaxUpdated.Add(-time.Hour)},
		{"equal instant, different zone", sampleMaxUpdated.UTC()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, _ := warmCache(t)
			want := sampleIssues()
			warm := &fakeSearcher{
				err:          errors.New("SearchQuarterIssues must not run when the probe is not newer"),
				probeFound:   true,
				probeUpdated: tt.value,
			}

			got, err := inventory(context.Background(), warm, dir, "")
			if err != nil {
				t.Fatalf("warm inventory: %v", err)
			}
			if warm.probeCalls != 1 {
				t.Errorf("probe calls = %d, want 1", warm.probeCalls)
			}
			if warm.calls != 0 {
				t.Errorf("SearchQuarterIssues calls = %d, want 0", warm.calls)
			}
			assertIssuesEqual(t, got, want)
		})
	}
}

func TestInventory_ProbeEmptyQuarter_ServesFromCache(t *testing.T) {
	dir, _ := warmCache(t)
	want := sampleIssues()
	warm := &fakeSearcher{
		err:        errors.New("SearchQuarterIssues must not run for an empty-quarter probe"),
		probeFound: false,
	}

	got, err := inventory(context.Background(), warm, dir, "")
	if err != nil {
		t.Fatalf("warm inventory: %v", err)
	}
	if warm.probeCalls != 1 || warm.calls != 0 {
		t.Errorf("probeCalls = %d, calls = %d; want 1 and 0", warm.probeCalls, warm.calls)
	}
	assertIssuesEqual(t, got, want)
}

func TestInventory_ProbeError_SurfacedNotSwallowed(t *testing.T) {
	dir, path := warmCache(t)
	sentinel := errors.New("probe boom")
	warm := &fakeSearcher{
		err:      errors.New("SearchQuarterIssues must not run after a probe failure"),
		probeErr: sentinel,
	}

	_, err := inventory(context.Background(), warm, dir, "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the probe sentinel", err)
	}
	if warm.calls != 0 {
		t.Errorf("SearchQuarterIssues calls = %d, want 0 (probe failure aborts before rebuild)", warm.calls)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("cache file removed after a probe failure: %v", statErr)
	}
}

func TestInventory_ProbeNewer_RemovalRoutesThroughGuard(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := t.TempDir()
	if strings.Contains(cacheDir, layout.Namespace) {
		t.Skipf("temp dir %q unexpectedly contains %q", cacheDir, layout.Namespace)
	}
	if _, err := inventory(context.Background(), &fakeSearcher{issues: sampleIssues()}, cacheDir, ""); err != nil {
		t.Fatalf("cold inventory: %v", err)
	}
	path := filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q1"))

	warm := &fakeSearcher{
		issues:       sampleIssues(),
		probeFound:   true,
		probeUpdated: sampleMaxUpdated.Add(24 * time.Hour),
	}
	_, err := inventory(context.Background(), warm, cacheDir, "")
	var outside *safedelete.ErrOutsideBase
	if !errors.As(err, &outside) {
		t.Fatalf("err = %v (%T), want *safedelete.ErrOutsideBase", err, err)
	}
	if warm.calls != 0 {
		t.Errorf("SearchQuarterIssues calls = %d, want 0 (rebuild not reached after a guard rejection)", warm.calls)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("cache file removed despite the guard rejection: %v", statErr)
	}
}

func TestInventory_EmptyQuarterCache_ProbeNewer_RebuildsOnceThenStabilises(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	_, dir := mkCacheDir(t)
	path := filepath.Join(dir, cacheFileName("PROJ", "FY26-Q1"))

	cold := &fakeSearcher{issues: []QuarterIssue{}}
	if _, err := inventory(context.Background(), cold, dir, ""); err != nil {
		t.Fatalf("cold inventory: %v", err)
	}
	if cf := readCacheFile(t, path); cf.MaxUpdated != (time.Time{}).Format(cacheTimeLayout) {
		t.Fatalf("cold empty cache max_updated = %q, want the zero time", cf.MaxUpdated)
	}

	populated := sampleIssues()
	rebuild := &fakeSearcher{
		issues:       populated,
		probeFound:   true,
		probeUpdated: sampleMaxUpdated,
	}
	got, err := inventory(context.Background(), rebuild, dir, "")
	if err != nil {
		t.Fatalf("rebuild inventory: %v", err)
	}
	if rebuild.probeCalls != 1 {
		t.Errorf("probe calls = %d, want 1", rebuild.probeCalls)
	}
	if rebuild.calls != 1 {
		t.Errorf("SearchQuarterIssues calls = %d, want 1 (empty cache is stale, rebuild once)", rebuild.calls)
	}
	assertIssuesEqual(t, got, populated)
	if cf := readCacheFile(t, path); cf.MaxUpdated != sampleMaxUpdated.Format(cacheTimeLayout) {
		t.Errorf("rewritten max_updated = %q, want %q", cf.MaxUpdated, sampleMaxUpdated.Format(cacheTimeLayout))
	}

	stable := &fakeSearcher{
		err:          errors.New("SearchQuarterIssues must not run once the empty→populated rebuild has stabilised"),
		probeFound:   true,
		probeUpdated: sampleMaxUpdated,
	}
	got, err = inventory(context.Background(), stable, dir, "")
	if err != nil {
		t.Fatalf("stable inventory: %v", err)
	}
	if stable.probeCalls != 1 {
		t.Errorf("probe calls = %d, want 1", stable.probeCalls)
	}
	if stable.calls != 0 {
		t.Errorf("SearchQuarterIssues calls = %d, want 0 (cache is current after the rebuild)", stable.calls)
	}
	assertIssuesEqual(t, got, populated)
}

func withCacheHome(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	return filepath.Join(base, layout.Namespace)
}

func TestInventoryForQuarter_ColdPopulate(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ"}}`)
	cacheDir := withCacheHome(t)
	want := sampleIssues()
	f := &fakeSearcher{issues: want}

	got, err := InventoryForQuarter(context.Background(), f, "FY26-Q3", "")
	if err != nil {
		t.Fatalf("InventoryForQuarter: %v", err)
	}
	assertIssuesEqual(t, got, want)

	if f.calls != 1 {
		t.Errorf("search calls = %d, want 1", f.calls)
	}
	if f.gotProj != "PROJ" || f.gotQtr != "FY26-Q3" {
		t.Errorf("search args = (%q, %q), want (PROJ, FY26-Q3)", f.gotProj, f.gotQtr)
	}

	path := filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q3"))
	if cf := readCacheFile(t, path); cf.Project != "PROJ" || cf.Quarter != "FY26-Q3" {
		t.Errorf("cache pair = (%q, %q), want (PROJ, FY26-Q3)", cf.Project, cf.Quarter)
	}
}

func TestInventoryForQuarter_WarmServe(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ"}}`)
	withCacheHome(t)
	want := sampleIssues()

	if _, err := InventoryForQuarter(context.Background(), &fakeSearcher{issues: want}, "FY26-Q3", ""); err != nil {
		t.Fatalf("cold InventoryForQuarter: %v", err)
	}

	warm := &fakeSearcher{err: errors.New("search must not be called on the warm path")}
	got, err := InventoryForQuarter(context.Background(), warm, "FY26-Q3", "")
	if err != nil {
		t.Fatalf("warm InventoryForQuarter: %v", err)
	}
	if warm.calls != 0 {
		t.Fatalf("warm path invoked the search %d time(s), want 0", warm.calls)
	}
	assertIssuesEqual(t, got, want)
}

func TestInventoryForQuarter_UnsetProject(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{}`)
	withCacheHome(t)

	_, err := InventoryForQuarter(context.Background(), &fakeSearcher{}, "FY26-Q3", "")
	var unset *ErrSettingUnset
	if !errors.As(err, &unset) {
		t.Fatalf("err = %v, want *ErrSettingUnset", err)
	}
	if unset.Setting != "defaults.project" {
		t.Errorf("Setting = %q, want defaults.project", unset.Setting)
	}
}

func TestInventoryForQuarter_CurrentQuarterIgnored(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)
	cacheDir := withCacheHome(t)
	want := sampleIssues()
	f := &fakeSearcher{issues: want}

	got, err := InventoryForQuarter(context.Background(), f, "FY26-Q3", "")
	if err != nil {
		t.Fatalf("InventoryForQuarter: %v", err)
	}
	assertIssuesEqual(t, got, want)
	if f.gotQtr != "FY26-Q3" {
		t.Errorf("search quarter = %q, want FY26-Q3 (current_quarter must be ignored)", f.gotQtr)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q3"))); err != nil {
		t.Errorf("cache file for FY26-Q3 not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q1"))); !os.IsNotExist(err) {
		t.Errorf("cache file for current_quarter FY26-Q1 unexpectedly written")
	}
}

func TestInventoryForQuarter_ProbeNewer_ClearsAndRebuilds(t *testing.T) {
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ"}}`)
	cacheDir := withCacheHome(t)

	if _, err := InventoryForQuarter(context.Background(), &fakeSearcher{issues: sampleIssues()}, "FY26-Q3", ""); err != nil {
		t.Fatalf("cold InventoryForQuarter: %v", err)
	}
	path := filepath.Join(cacheDir, cacheFileName("PROJ", "FY26-Q3"))

	fresh := []QuarterIssue{{
		Key:     "PROJ-5",
		Summary: "edited in jira",
		Created: time.Date(2026, 1, 2, 15, 4, 5, 123_000_000, tz),
		Updated: time.Date(2026, 4, 1, 12, 0, 0, 0, tz),
	}}
	warm := &fakeSearcher{
		issues:       fresh,
		probeFound:   true,
		probeUpdated: fresh[0].Updated,
	}

	got, err := InventoryForQuarter(context.Background(), warm, "FY26-Q3", "")
	if err != nil {
		t.Fatalf("warm InventoryForQuarter: %v", err)
	}
	if warm.probeCalls != 1 {
		t.Errorf("probe calls = %d, want 1", warm.probeCalls)
	}
	if warm.calls != 1 {
		t.Errorf("SearchQuarterIssues calls = %d, want 1 (rebuild)", warm.calls)
	}
	assertIssuesEqual(t, got, fresh)

	if cf := readCacheFile(t, path); len(cf.Records) != 1 || cf.Records[0].Key != "PROJ-5" {
		t.Errorf("rewritten records = %+v, want the single PROJ-5", cf.Records)
	}
}

func TestInventoryForQuarter_ProbeNotNewer_ServesFromCache(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value time.Time
	}{
		{"equal", sampleMaxUpdated},
		{"older", sampleMaxUpdated.Add(-time.Hour)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withConfigHome(t)
			writeSettings(t, `{"defaults":{"project":"PROJ"}}`)
			withCacheHome(t)
			want := sampleIssues()
			if _, err := InventoryForQuarter(context.Background(), &fakeSearcher{issues: want}, "FY26-Q3", ""); err != nil {
				t.Fatalf("cold InventoryForQuarter: %v", err)
			}

			warm := &fakeSearcher{
				err:          errors.New("SearchQuarterIssues must not run when the probe is not newer"),
				probeFound:   true,
				probeUpdated: tt.value,
			}
			got, err := InventoryForQuarter(context.Background(), warm, "FY26-Q3", "")
			if err != nil {
				t.Fatalf("warm InventoryForQuarter: %v", err)
			}
			if warm.probeCalls != 1 {
				t.Errorf("probe calls = %d, want 1", warm.probeCalls)
			}
			if warm.calls != 0 {
				t.Errorf("SearchQuarterIssues calls = %d, want 0", warm.calls)
			}
			assertIssuesEqual(t, got, want)
		})
	}
}

func readCacheFile(t *testing.T, path string) cacheFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile cache %s: %v", path, err)
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("Unmarshal cache %s: %v", path, err)
	}
	return cf
}
