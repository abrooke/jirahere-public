package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
)

func withConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withConfigHome(t)

	want := &Config{
		Provider: ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &APITokenConfig{
			Email: "aslan@example.com",
			Token: "secret-token",
		},
	}
	if err := Save("", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Provider != want.Provider || got.Site != want.Site {
		t.Fatalf("Load returned %+v, want %+v", got, want)
	}
	if got.APIToken == nil || *got.APIToken != *want.APIToken {
		t.Fatalf("Load returned APIToken %+v, want %+v", got.APIToken, want.APIToken)
	}
}

func TestSaveLoadRoundTrip_OAuth(t *testing.T) {
	withConfigHome(t)

	expires := time.Date(2026, 8, 9, 20, 15, 0, 0, time.UTC)
	want := &Config{
		Provider: ProviderOAuth,
		Site:     "acme.atlassian.net",
		CloudID:  "1324a887-1234-5678-9abc-def012345678",
		OAuth: &OAuthConfig{
			AccessToken:  "access",
			RefreshToken: "refresh",
			ExpiresAt:    expires,
		},
	}
	if err := Save("", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.OAuth == nil {
		t.Fatalf("Load returned nil OAuth section")
	}
	if !got.OAuth.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.OAuth.ExpiresAt, expires)
	}
	if got.CloudID != want.CloudID {
		t.Errorf("CloudID = %q, want %q", got.CloudID, want.CloudID)
	}
}

func TestSave_Permissions(t *testing.T) {
	dir := withConfigHome(t)

	cfg := &Config{Provider: ProviderAPIToken, Site: "acme.atlassian.net"}
	if err := Save("", cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path, err := ConfigPath("")
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat config file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %o, want 0600", perm)
	}

	configDir := filepath.Join(dir, "jirahere")
	di, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("Stat config dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir mode = %o, want 0700", perm)
	}
}

func TestSave_NoLeftoverTempFile(t *testing.T) {
	dir := withConfigHome(t)

	cfg := &Config{Provider: ProviderAPIToken, Site: "acme.atlassian.net"}
	if err := Save("", cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "jirahere"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("config dir contains %v, want exactly [config.json]", names)
	}
}

func TestSave_Overwrite(t *testing.T) {
	withConfigHome(t)

	first := &Config{Provider: ProviderOAuth, Site: "acme.atlassian.net"}
	if err := Save("", first); err != nil {
		t.Fatalf("Save(first): %v", err)
	}
	second := &Config{Provider: ProviderAPIToken, Site: "other.atlassian.net"}
	if err := Save("", second); err != nil {
		t.Fatalf("Save(second): %v", err)
	}

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Provider != ProviderAPIToken || got.Site != "other.atlassian.net" {
		t.Fatalf("Load returned %+v, want the second config", got)
	}
}

func TestLoad_NotExist(t *testing.T) {
	withConfigHome(t)

	_, err := Load("")
	if err == nil {
		t.Fatal("Load: expected error for missing config, got nil")
	}
	if !os.IsNotExist(err) && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load error = %v, want an os.IsNotExist error", err)
	}
}

func TestDelete(t *testing.T) {
	withConfigHome(t)

	existed, err := Delete("")
	if err != nil {
		t.Fatalf("Delete on empty config: %v", err)
	}
	if existed {
		t.Error("Delete reported existed=true with no config present")
	}

	if err := Save("", &Config{Provider: ProviderAPIToken, Site: "acme.atlassian.net"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	existed, err = Delete("")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !existed {
		t.Error("Delete reported existed=false after Save")
	}

	if _, err := Load(""); !os.IsNotExist(err) {
		t.Fatalf("Load after Delete: err = %v, want IsNotExist", err)
	}

	path, err := ConfigPath("")
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config file still on disk after Delete: Lstat err = %v", err)
	}
}

func TestDelete_DirPresentFileAbsent(t *testing.T) {
	home := withConfigHome(t)

	dir := filepath.Join(home, "jirahere")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	path := filepath.Join(dir, "config.json")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("precondition: config file should not exist, Lstat err = %v", err)
	}

	existed, err := Delete("")
	if err != nil {
		t.Fatalf("Delete with dir present, file absent: %v", err)
	}
	if existed {
		t.Error("Delete reported existed=true with the config file absent")
	}
}

func TestDelete_ConfigDirAbsent(t *testing.T) {
	home := withConfigHome(t)

	dir := filepath.Join(home, "jirahere")
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("precondition: config dir should not exist, Lstat err = %v", err)
	}

	existed, err := Delete("")
	if err != nil {
		t.Fatalf("Delete with absent config dir: %v", err)
	}
	if existed {
		t.Error("Delete reported existed=true with no config directory present")
	}
}

func TestDelete_GuardRejection(t *testing.T) {
	home := withConfigHome(t)

	realDir := t.TempDir()
	if strings.Contains(realDir, layout.Namespace) {
		t.Skipf("temp dir %q unexpectedly contains %q; symlink-rejection setup not valid here", realDir, layout.Namespace)
	}
	target := filepath.Join(realDir, "config.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}

	if err := os.Symlink(realDir, filepath.Join(home, "jirahere")); err != nil {
		t.Fatalf("symlink config dir: %v", err)
	}

	existed, err := Delete("")
	if existed {
		t.Error("Delete reported existed=true on a guard rejection")
	}
	if err == nil {
		t.Fatal("Delete returned nil error on a guard rejection")
	}
	var outside *safedelete.ErrOutsideBase
	if !errors.As(err, &outside) {
		t.Fatalf("Delete error = %v, want it to wrap *safedelete.ErrOutsideBase", err)
	}

	if !strings.Contains(outside.Reason, "namespace token") || !strings.Contains(outside.Reason, layout.Namespace) {
		t.Errorf("rejection Reason = %q, want it to name the %q namespace token", outside.Reason, layout.Namespace)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("target config file missing after a rejected Delete: %v", statErr)
	}
}

func TestProviderLabel(t *testing.T) {
	cases := map[string]string{
		ProviderOAuth:    "OAuth",
		ProviderAPIToken: "API token",
		"unknown":        "unknown",
	}
	for in, want := range cases {
		if got := ProviderLabel(in); got != want {
			t.Errorf("ProviderLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
