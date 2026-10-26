package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
)

func profileCfg(site string) *Config {
	return &Config{
		Provider: ProviderAPIToken,
		Site:     site,
		APIToken: &APITokenConfig{Email: "a@example.com", Token: "tok-" + site},
	}
}

func TestConfigPaths_Profile(t *testing.T) {
	home := withConfigHome(t)

	def, err := ConfigDir("")
	if err != nil {
		t.Fatalf("ConfigDir(\"\"): %v", err)
	}
	if want := filepath.Join(home, "jirahere"); def != want {
		t.Errorf("ConfigDir(\"\") = %q, want %q", def, want)
	}

	dir, err := ConfigDir("work")
	if err != nil {
		t.Fatalf("ConfigDir(work): %v", err)
	}
	if want := filepath.Join(home, layout.Namespace+"-profiles", "work"); dir != want {
		t.Errorf("ConfigDir(work) = %q, want %q", dir, want)
	}

	path, err := ConfigPath("work")
	if err != nil {
		t.Fatalf("ConfigPath(work): %v", err)
	}
	if want := filepath.Join(dir, "config.json"); path != want {
		t.Errorf("ConfigPath(work) = %q, want %q", path, want)
	}

	named, err := ConfigDir("default")
	if err != nil {
		t.Fatalf("ConfigDir(default): %v", err)
	}
	if named == def {
		t.Errorf("ConfigDir(\"default\") = %q, must differ from the unnamed default", named)
	}
}

func TestProfileIsolation(t *testing.T) {
	home := withConfigHome(t)

	sites := map[string]string{
		"":     "default.atlassian.net",
		"work": "work.atlassian.net",
		"home": "home.atlassian.net",
	}
	for prof, site := range sites {
		if err := Save(prof, profileCfg(site)); err != nil {
			t.Fatalf("Save(%q): %v", prof, err)
		}
	}
	for prof, site := range sites {
		got, err := Load(prof)
		if err != nil {
			t.Fatalf("Load(%q): %v", prof, err)
		}
		if got.Site != site {
			t.Errorf("Load(%q).Site = %q, want %q", prof, got.Site, site)
		}
	}

	for _, p := range []string{
		filepath.Join(home, "jirahere", "config.json"),
		filepath.Join(home, "jirahere-profiles", "work", "config.json"),
		filepath.Join(home, "jirahere-profiles", "home", "config.json"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s on disk: %v", p, err)
		}
	}

	existed, err := Delete("work")
	if err != nil || !existed {
		t.Fatalf("Delete(work) = (%v, %v), want (true, nil)", existed, err)
	}
	if _, err := Load("work"); !os.IsNotExist(err) {
		t.Errorf("Load(work) after Delete err = %v, want not-exist", err)
	}
	for _, prof := range []string{"", "home"} {
		got, err := Load(prof)
		if err != nil {
			t.Fatalf("Load(%q) after Delete(work): %v", prof, err)
		}
		if got.Site != sites[prof] {
			t.Errorf("Load(%q).Site = %q, want %q", prof, got.Site, sites[prof])
		}
	}

	if existed, err := Delete(""); err != nil || !existed {
		t.Fatalf("Delete(\"\") = (%v, %v), want (true, nil)", existed, err)
	}
	if _, err := Load("home"); err != nil {
		t.Errorf("Load(home) after Delete(\"\"): %v", err)
	}
}

func TestLoadProvider_MissingProfileIsNotLoggedIn(t *testing.T) {
	withConfigHome(t)

	if err := Save("", profileCfg("default.atlassian.net")); err != nil {
		t.Fatalf("Save default: %v", err)
	}
	if _, err := LoadProvider(""); err != nil {
		t.Fatalf("LoadProvider(\"\"): %v", err)
	}

	_, err := LoadProvider("work")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("LoadProvider(work) err = %v, want ErrNotLoggedIn", err)
	}
	if _, err := Load("work"); !os.IsNotExist(err) {
		t.Errorf("Load(work) err = %v, want not-exist", err)
	}
}

func TestSave_ProfilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	home := withConfigHome(t)

	if err := Save("work", profileCfg("work.atlassian.net")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for _, d := range []string{
		filepath.Join(home, "jirahere-profiles"),
		filepath.Join(home, "jirahere-profiles", "work"),
	} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %s: %v", d, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%s perm = %o, want 700", d, got)
		}
	}
	file := filepath.Join(home, "jirahere-profiles", "work", "config.json")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat %s: %v", file, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("config.json perm = %o, want 600", got)
	}

	entries, err := os.ReadDir(filepath.Join(home, "jirahere-profiles", "work"))
	if err != nil {
		t.Fatalf("read profile dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Errorf("profile dir entries = %v, want only config.json", entries)
	}
	if _, err := os.Lstat(filepath.Join(home, "jirahere")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("default config dir exists after saving a named profile: %v", err)
	}
}

func TestSave_TightensExistingProfilesParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	home := withConfigHome(t)

	parent := filepath.Join(home, "jirahere-profiles")
	profDir := filepath.Join(parent, "work")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, d := range []string{parent, profDir} {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := Save("work", profileCfg("work.atlassian.net")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, d := range []string{parent, profDir} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %s: %v", d, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%s perm = %o after Save, want 700", d, got)
		}
	}
}

func TestDelete_ProfileAbsent(t *testing.T) {
	home := withConfigHome(t)

	existed, err := Delete("work")
	if err != nil || existed {
		t.Fatalf("Delete(work), nothing on disk = (%v, %v), want (false, nil)", existed, err)
	}

	if err := os.MkdirAll(filepath.Join(home, "jirahere-profiles", "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	existed, err = Delete("work")
	if err != nil || existed {
		t.Fatalf("Delete(work), dir only = (%v, %v), want (false, nil)", existed, err)
	}
}

func TestDelete_ProfileGuardBase(t *testing.T) {
	t.Run("plain profile dir", func(t *testing.T) {
		home := withConfigHome(t)
		if err := Save("", profileCfg("default.atlassian.net")); err != nil {
			t.Fatal(err)
		}
		if err := Save("work", profileCfg("work.atlassian.net")); err != nil {
			t.Fatal(err)
		}

		existed, err := Delete("work")
		var outside *safedelete.ErrOutsideBase
		if errors.As(err, &outside) {
			t.Fatalf("Delete(work) rejected by the guard (base wrongly not the profile dir?): %v", err)
		}
		if err != nil || !existed {
			t.Fatalf("Delete(work) = (%v, %v), want (true, nil)", existed, err)
		}
		if _, err := os.Stat(filepath.Join(home, "jirahere-profiles", "work", "config.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("profile config.json still present after Delete: %v", err)
		}
		if _, err := os.Stat(filepath.Join(home, "jirahere", "config.json")); err != nil {
			t.Errorf("default config.json touched by Delete(work): %v", err)
		}
	})

	t.Run("symlinked profile dir", func(t *testing.T) {
		home := withConfigHome(t)
		realDir := filepath.Join(t.TempDir(), layout.Namespace+"-real")
		if err := os.Mkdir(realDir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(realDir, "config.json")
		if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, "jirahere-profiles"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realDir, filepath.Join(home, "jirahere-profiles", "work")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}

		existed, err := Delete("work")
		if err != nil || !existed {
			t.Fatalf("Delete(work) = (%v, %v), want (true, nil)", existed, err)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("target still present after Delete: %v", err)
		}
	})
}

func TestDelete_ProfileGuardRejectsEscape(t *testing.T) {
	home := withConfigHome(t)

	realDir := t.TempDir()
	if strings.Contains(realDir, layout.Namespace) {
		t.Skipf("temp dir %q unexpectedly contains %q; symlink-rejection setup not valid here", realDir, layout.Namespace)
	}
	target := filepath.Join(realDir, "config.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "jirahere-profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(home, "jirahere-profiles", "work")); err != nil {
		t.Fatalf("symlink profile dir: %v", err)
	}

	existed, err := Delete("work")
	if existed {
		t.Error("Delete reported existed=true on a guard rejection")
	}
	var outside *safedelete.ErrOutsideBase
	if !errors.As(err, &outside) {
		t.Fatalf("Delete error = %v, want it to wrap *safedelete.ErrOutsideBase", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("target removed or unreadable after guard rejection: %v", statErr)
	}
}

func TestInvalidProfile_NoFilesystemAccess(t *testing.T) {
	home := withConfigHome(t)
	before, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"..", "../evil", "a/b", `a\b`, ".hidden", "-x", "has space", strings.Repeat("a", 65), "nul\x00", "é"} {
		if _, err := ConfigDir(name); err == nil {
			t.Errorf("ConfigDir(%q) = nil error, want invalid-profile error", name)
		}
		if _, err := ConfigPath(name); err == nil {
			t.Errorf("ConfigPath(%q) = nil error", name)
		}
		if _, err := Load(name); err == nil || os.IsNotExist(err) {
			t.Errorf("Load(%q) err = %v, want invalid-profile error", name, err)
		}
		if err := Save(name, profileCfg("work.atlassian.net")); err == nil {
			t.Errorf("Save(%q) = nil error", name)
		}
		if existed, err := Delete(name); err == nil || existed {
			t.Errorf("Delete(%q) = (%v, %v), want (false, error)", name, existed, err)
		}
		_, err := LoadProvider(name)
		if err == nil || errors.Is(err, ErrNotLoggedIn) {
			t.Errorf("LoadProvider(%q) err = %v, want invalid-profile error, not ErrNotLoggedIn", name, err)
		}
	}

	after, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Errorf("config home entries changed from %d to %d after invalid-profile calls", len(before), len(after))
	}
}

func TestInvalidProfile_BeforeEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	_, err := ConfigDir("../x")
	if err == nil {
		t.Fatal("ConfigDir(../x) = nil error")
	}
	if strings.Contains(err.Error(), "config directory") {
		t.Errorf("err = %v; want the profile validation error, reported before environment lookup", err)
	}
}

func TestOAuthRefresh_PersistsToSameProfile(t *testing.T) {
	withConfigHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer srv.Close()

	origURL, origClientID := oauthTokenURL, OAuthClientID
	oauthTokenURL = srv.URL
	OAuthClientID = "test-client"
	defer func() { oauthTokenURL, OAuthClientID = origURL, origClientID }()

	def := profileCfg("default.atlassian.net")
	if err := Save("", def); err != nil {
		t.Fatal(err)
	}
	expired := &Config{
		Provider: ProviderOAuth,
		Site:     "work.atlassian.net",
		CloudID:  "cloud-123",
		OAuth: &OAuthConfig{
			AccessToken:  "expired-access",
			RefreshToken: "old-refresh",
			ExpiresAt:    time.Now().Add(-time.Hour),
		},
	}
	if err := Save("work", expired); err != nil {
		t.Fatal(err)
	}

	provider, err := LoadProvider("work")
	if err != nil {
		t.Fatalf("LoadProvider(work): %v", err)
	}
	if _, err := provider.Credentials(context.Background()); err != nil {
		t.Fatalf("Credentials: %v", err)
	}

	got, err := Load("work")
	if err != nil {
		t.Fatal(err)
	}
	if got.OAuth == nil || got.OAuth.AccessToken != "new-access" {
		t.Errorf("work config after refresh = %+v, want refreshed access token", got)
	}
	gotDef, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if gotDef.Provider != ProviderAPIToken || gotDef.Site != "default.atlassian.net" {
		t.Errorf("default config changed by profile refresh: %+v", gotDef)
	}
}
