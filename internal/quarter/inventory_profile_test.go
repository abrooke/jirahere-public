package quarter

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/layout"
)

func writeProfileSettings(t *testing.T, prof, raw string) {
	t.Helper()
	dir, err := auth.ConfigDir(prof)
	if err != nil {
		t.Fatalf("auth.ConfigDir(%q): %v", prof, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func populateCacheDir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	f := filepath.Join(dir, "quarter-inventory-marker.json")
	if err := os.WriteFile(f, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", f, err)
	}
	return f
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s should exist: %v", path, err)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s should be absent, Lstat err = %v", path, err)
	}
}

func TestCacheDir_Profile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)

	def, err := CacheDir("")
	if err != nil {
		t.Fatalf("CacheDir(\"\"): %v", err)
	}
	if want := filepath.Join(base, "jirahere"); def != want {
		t.Errorf("CacheDir(\"\") = %q, want %q", def, want)
	}

	got, err := CacheDir("work")
	if err != nil {
		t.Fatalf("CacheDir(work): %v", err)
	}
	if want := filepath.Join(base, "jirahere-profiles", "work"); got != want {
		t.Errorf("CacheDir(work) = %q, want %q", got, want)
	}
	if strings.HasPrefix(got, def+string(os.PathSeparator)) {
		t.Errorf("profile cache %q is nested under the default cache %q", got, def)
	}
}

func TestCacheDir_InvalidProfileRejected(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	withConfigHome(t)
	writeSettings(t, `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`)

	for _, bad := range []string{"../x", "a/b", ".hidden", "-x", "a b", strings.Repeat("a", 65)} {
		if got, err := CacheDir(bad); err == nil {
			t.Errorf("CacheDir(%q) = %q, want error", bad, got)
		}
		if err := ClearCache(bad); err == nil {
			t.Errorf("ClearCache(%q) = nil, want error", bad)
		}
		f := &fakeSearcher{issues: sampleIssues()}
		if _, err := Inventory(context.Background(), f, bad); err == nil {
			t.Errorf("Inventory(%q) = nil error, want error", bad)
		}
		if _, err := InventoryForQuarter(context.Background(), f, "FY26-Q3", bad); err == nil {
			t.Errorf("InventoryForQuarter(%q) = nil error, want error", bad)
		}
		if f.calls != 0 || f.probeCalls != 0 {
			t.Errorf("invalid profile %q reached Jira: calls=%d probes=%d", bad, f.calls, f.probeCalls)
		}
	}

	if entries, _ := os.ReadDir(base); len(entries) != 0 {
		t.Errorf("cache home has %d entries after invalid profiles, want 0", len(entries))
	}
}

func TestClearCache_DefaultLeavesProfilesIntact(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	populateCacheDir(t, filepath.Join(base, "jirahere"))
	workMarker := populateCacheDir(t, filepath.Join(base, "jirahere-profiles", "work"))
	homeMarker := populateCacheDir(t, filepath.Join(base, "jirahere-profiles", "home"))

	if err := ClearCache(""); err != nil {
		t.Fatalf("ClearCache(\"\"): %v", err)
	}
	assertAbsent(t, filepath.Join(base, "jirahere"))
	assertExists(t, workMarker)
	assertExists(t, homeMarker)
}

func TestClearCache_NamedLeavesDefaultAndOthersIntact(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	defMarker := populateCacheDir(t, filepath.Join(base, "jirahere"))
	workMarker := populateCacheDir(t, filepath.Join(base, "jirahere-profiles", "work"))
	homeMarker := populateCacheDir(t, filepath.Join(base, "jirahere-profiles", "home"))

	if err := ClearCache("work"); err != nil {
		t.Fatalf("ClearCache(work): %v", err)
	}
	assertAbsent(t, filepath.Dir(workMarker))
	assertExists(t, defMarker)
	assertExists(t, homeMarker)
	assertExists(t, filepath.Join(base, "jirahere-profiles"))
}

func TestClearCache_NamedAbsentIsNil(t *testing.T) {
	t.Run("profiles parent absent", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", base)
		if err := ClearCache("work"); err != nil {
			t.Fatalf("ClearCache(work): %v", err)
		}
		assertAbsent(t, filepath.Join(base, "jirahere-profiles"))
	})
	t.Run("profile dir absent, sibling present", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", base)
		other := populateCacheDir(t, filepath.Join(base, "jirahere-profiles", "home"))
		if err := ClearCache("work"); err != nil {
			t.Fatalf("ClearCache(work): %v", err)
		}
		assertExists(t, other)
	})
	t.Run("cache home absent", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "never-created"))
		if err := ClearCache("work"); err != nil {
			t.Fatalf("ClearCache(work): %v", err)
		}
	})
}

func TestInventory_ProfileReadsOwnSettingsAndCache(t *testing.T) {
	withConfigHome(t)
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	writeSettings(t, `{"defaults":{"project":"DEF","current_quarter":"FY26-Q1"}}`)
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORK","current_quarter":"FY26-Q2"}}`)

	f := &fakeSearcher{issues: sampleIssues()}
	if _, err := Inventory(context.Background(), f, "work"); err != nil {
		t.Fatalf("Inventory(work): %v", err)
	}
	if f.gotProj != "WORK" || f.gotQtr != "FY26-Q2" {
		t.Errorf("search args = (%q, %q), want (WORK, FY26-Q2) from the profile's settings", f.gotProj, f.gotQtr)
	}

	profDir := filepath.Join(cacheHome, "jirahere-profiles", "work")
	assertExists(t, filepath.Join(profDir, cacheFileName("WORK", "FY26-Q2")))

	assertAbsent(t, filepath.Join(cacheHome, layout.Namespace))
}

func TestInventory_ProfileMissingSettingsNoFallback(t *testing.T) {
	withConfigHome(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeSettings(t, `{"defaults":{"project":"DEF","current_quarter":"FY26-Q1"}}`)

	f := &fakeSearcher{issues: sampleIssues()}
	_, err := Inventory(context.Background(), f, "fresh")
	var unset *ErrSettingUnset
	if !errors.As(err, &unset) || unset.Setting != "defaults.project" {
		t.Fatalf("Inventory(fresh) err = %v, want *ErrSettingUnset{defaults.project}", err)
	}
	if f.calls != 0 {
		t.Errorf("search calls = %d, want 0", f.calls)
	}
}

func TestInventoryForQuarter_ProfileReadsOwnSettingsAndCache(t *testing.T) {
	withConfigHome(t)
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	writeSettings(t, `{"defaults":{"project":"DEF"}}`)
	writeProfileSettings(t, "work", `{"defaults":{"project":"WORK"}}`)

	f := &fakeSearcher{issues: sampleIssues()}
	if _, err := InventoryForQuarter(context.Background(), f, "FY26-Q3", "work"); err != nil {
		t.Fatalf("InventoryForQuarter(work): %v", err)
	}
	if f.gotProj != "WORK" || f.gotQtr != "FY26-Q3" {
		t.Errorf("search args = (%q, %q), want (WORK, FY26-Q3)", f.gotProj, f.gotQtr)
	}
	assertExists(t, filepath.Join(cacheHome, "jirahere-profiles", "work", cacheFileName("WORK", "FY26-Q3")))
	assertAbsent(t, filepath.Join(cacheHome, layout.Namespace))
}

func TestInventory_TwoProfileCacheIsolation(t *testing.T) {
	withConfigHome(t)
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	same := `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`
	writeSettings(t, same)
	writeProfileSettings(t, "a", same)
	writeProfileSettings(t, "b", same)

	issuesA := sampleIssues()[:1]
	issuesB := sampleIssues()[1:]

	if _, err := Inventory(context.Background(), &fakeSearcher{issues: issuesA}, "a"); err != nil {
		t.Fatalf("Inventory(a): %v", err)
	}
	name := cacheFileName("PROJ", "FY26-Q1")
	assertExists(t, filepath.Join(cacheHome, "jirahere-profiles", "a", name))
	assertAbsent(t, filepath.Join(cacheHome, "jirahere-profiles", "b"))
	assertAbsent(t, filepath.Join(cacheHome, "jirahere"))

	fb := &fakeSearcher{issues: issuesB}
	gotB, err := Inventory(context.Background(), fb, "b")
	if err != nil {
		t.Fatalf("Inventory(b): %v", err)
	}
	if fb.calls != 1 {
		t.Errorf("profile b search calls = %d, want 1 (cold; must not read a's cache)", fb.calls)
	}
	assertIssuesEqual(t, gotB, issuesB)

	fd := &fakeSearcher{issues: issuesB}
	if _, err := Inventory(context.Background(), fd, ""); err != nil {
		t.Fatalf("Inventory(\"\"): %v", err)
	}
	if fd.calls != 1 {
		t.Errorf("default search calls = %d, want 1 (cold; must not read a profile's cache)", fd.calls)
	}

	warmA := &fakeSearcher{}
	gotA, err := Inventory(context.Background(), warmA, "a")
	if err != nil {
		t.Fatalf("warm Inventory(a): %v", err)
	}
	if warmA.calls != 0 {
		t.Errorf("warm a search calls = %d, want 0", warmA.calls)
	}
	assertIssuesEqual(t, gotA, issuesA)

	if err := ClearCache("a"); err != nil {
		t.Fatalf("ClearCache(a): %v", err)
	}
	assertAbsent(t, filepath.Join(cacheHome, "jirahere-profiles", "a"))
	assertExists(t, filepath.Join(cacheHome, "jirahere-profiles", "b", name))
	assertExists(t, filepath.Join(cacheHome, "jirahere", name))
}

func TestInventory_StaleRebuildIsolatedToProfile(t *testing.T) {

	warmAll := func(t *testing.T) (def, a, b string) {
		t.Helper()
		withConfigHome(t)
		cacheHome := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheHome)
		same := `{"defaults":{"project":"PROJ","current_quarter":"FY26-Q1"}}`
		writeSettings(t, same)
		writeProfileSettings(t, "a", same)
		writeProfileSettings(t, "b", same)
		for _, prof := range []string{"", "a", "b"} {
			if _, err := Inventory(context.Background(), &fakeSearcher{issues: sampleIssues()}, prof); err != nil {
				t.Fatalf("warm Inventory(%q): %v", prof, err)
			}
		}
		name := cacheFileName("PROJ", "FY26-Q1")
		return filepath.Join(cacheHome, "jirahere", name),
			filepath.Join(cacheHome, "jirahere-profiles", "a", name),
			filepath.Join(cacheHome, "jirahere-profiles", "b", name)
	}
	readBytes := func(t *testing.T, path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", path, err)
		}
		return data
	}
	fresh := []QuarterIssue{{
		Key:     "PROJ-5",
		Summary: "edited in jira",
		Created: time.Date(2026, 1, 2, 15, 4, 5, 123_000_000, tz),
		Updated: time.Date(2026, 4, 1, 12, 0, 0, 0, tz),
	}}

	t.Run("rebuild rewrites only the profile's file", func(t *testing.T) {
		def, a, b := warmAll(t)
		defBefore, bBefore := readBytes(t, def), readBytes(t, b)
		aBefore := readBytes(t, a)

		f := &fakeSearcher{issues: fresh, probeFound: true, probeUpdated: fresh[0].Updated}
		got, err := Inventory(context.Background(), f, "a")
		if err != nil {
			t.Fatalf("Inventory(a) stale: %v", err)
		}
		if f.probeCalls != 1 || f.calls != 1 {
			t.Errorf("probeCalls = %d, calls = %d; want 1 and 1 (probe then rebuild)", f.probeCalls, f.calls)
		}
		assertIssuesEqual(t, got, fresh)

		if bytes.Equal(readBytes(t, a), aBefore) {
			t.Error("profile a's cache file was not rewritten")
		}
		if !bytes.Equal(readBytes(t, def), defBefore) {
			t.Error("default cache file changed by a profile-a rebuild")
		}
		if !bytes.Equal(readBytes(t, b), bBefore) {
			t.Error("profile b cache file changed by a profile-a rebuild")
		}
	})

	t.Run("removal is scoped to the profile's file", func(t *testing.T) {
		def, a, b := warmAll(t)
		defBefore, bBefore := readBytes(t, def), readBytes(t, b)

		f := &fakeSearcher{
			err:          errors.New("search failed"),
			probeFound:   true,
			probeUpdated: fresh[0].Updated,
		}
		if _, err := Inventory(context.Background(), f, "a"); err == nil {
			t.Fatal("Inventory(a) = nil error, want the search failure")
		}
		if f.probeCalls != 1 || f.calls != 1 {
			t.Errorf("probeCalls = %d, calls = %d; want 1 and 1", f.probeCalls, f.calls)
		}
		assertAbsent(t, a)
		if !bytes.Equal(readBytes(t, def), defBefore) {
			t.Error("default cache file changed by a profile-a stale removal")
		}
		if !bytes.Equal(readBytes(t, b), bBefore) {
			t.Error("profile b cache file changed by a profile-a stale removal")
		}
	})
}
