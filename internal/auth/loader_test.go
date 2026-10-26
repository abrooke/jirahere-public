package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadProvider_NotLoggedIn(t *testing.T) {
	withConfigHome(t)

	_, err := LoadProvider("")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("LoadProvider err = %v, want ErrNotLoggedIn", err)
	}
}

func TestLoadProvider_MaliciousSiteRejected(t *testing.T) {
	dir := withConfigHome(t)
	configDir := filepath.Join(dir, "jirahere")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw := `{
		"provider": "api-token",
		"site": "acme.atlassian.net@evil.example.com",
		"api_token": {"email": "aslan@example.com", "token": "secret"}
	}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadProvider("")
	if err == nil {
		t.Fatal("LoadProvider: expected error for malicious site, got nil")
	}
}

func TestLoadProvider_APIToken(t *testing.T) {
	withConfigHome(t)

	cfg := &Config{
		Provider: ProviderAPIToken,
		Site:     "acme.atlassian.net",
		APIToken: &APITokenConfig{Email: "aslan@example.com", Token: "secret"},
	}
	if err := Save("", cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	provider, err := LoadProvider("")
	if err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}

	creds, err := provider.Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	wantAuth := BasicAuthHeader("aslan@example.com", "secret")
	if creds.AuthHeader != wantAuth {
		t.Errorf("AuthHeader = %q, want %q", creds.AuthHeader, wantAuth)
	}
	if creds.BaseURL != "https://acme.atlassian.net" {
		t.Errorf("BaseURL = %q, want %q", creds.BaseURL, "https://acme.atlassian.net")
	}
}

func TestLoadProvider_OAuth_NotExpired(t *testing.T) {
	withConfigHome(t)

	cfg := &Config{
		Provider: ProviderOAuth,
		Site:     "acme.atlassian.net",
		CloudID:  "cloud-123",
		OAuth: &OAuthConfig{
			AccessToken:  "still-valid",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	if err := Save("", cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	provider, err := LoadProvider("")
	if err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}

	creds, err := provider.Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if creds.AuthHeader != "Bearer still-valid" {
		t.Errorf("AuthHeader = %q, want %q", creds.AuthHeader, "Bearer still-valid")
	}
	want := "https://api.atlassian.com/ex/jira/cloud-123"
	if creds.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", creds.BaseURL, want)
	}
}

func TestLoadProvider_OAuth_ExpiredRefreshesAndPersists(t *testing.T) {
	withConfigHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode refresh request: %v", err)
		}
		if body["grant_type"] != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", body["grant_type"])
		}
		if body["refresh_token"] != "old-refresh" {
			t.Errorf("refresh_token = %q, want %q", body["refresh_token"], "old-refresh")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer srv.Close()

	origURL, origClientID := oauthTokenURL, OAuthClientID
	oauthTokenURL = srv.URL
	OAuthClientID = "test-client"
	defer func() { oauthTokenURL, OAuthClientID = origURL, origClientID }()

	cfg := &Config{
		Provider: ProviderOAuth,
		Site:     "acme.atlassian.net",
		CloudID:  "cloud-123",
		OAuth: &OAuthConfig{
			AccessToken:  "expired-access",
			RefreshToken: "old-refresh",
			ExpiresAt:    time.Now().Add(-time.Hour),
		},
	}
	if err := Save("", cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	provider, err := LoadProvider("")
	if err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}

	creds, err := provider.Credentials(context.Background())
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if creds.AuthHeader != "Bearer new-access" {
		t.Errorf("AuthHeader = %q, want %q", creds.AuthHeader, "Bearer new-access")
	}

	persisted, err := Load("")
	if err != nil {
		t.Fatalf("Load after refresh: %v", err)
	}
	if persisted.OAuth.AccessToken != "new-access" {
		t.Errorf("persisted AccessToken = %q, want %q", persisted.OAuth.AccessToken, "new-access")
	}
	if persisted.OAuth.RefreshToken != "new-refresh" {
		t.Errorf("persisted RefreshToken = %q, want %q", persisted.OAuth.RefreshToken, "new-refresh")
	}
	if !persisted.OAuth.ExpiresAt.After(time.Now()) {
		t.Errorf("persisted ExpiresAt = %v, want in the future", persisted.OAuth.ExpiresAt)
	}
}
