package auth

import (
	"errors"
	"testing"
)

func TestSelectSite_ZeroSites(t *testing.T) {
	_, err := SelectSite(nil, "", failPrompt(t))
	if err == nil {
		t.Fatal("expected error for zero sites, got nil")
	}
}

func TestSelectSite_OneSiteAutoSelects(t *testing.T) {
	sites := []Site{{CloudID: "1", URL: "https://acme.atlassian.net"}}
	got, err := SelectSite(sites, "", failPrompt(t))
	if err != nil {
		t.Fatalf("SelectSite: %v", err)
	}
	if got != sites[0] {
		t.Errorf("got %+v, want %+v", got, sites[0])
	}
}

func TestSelectSite_OneSiteAutoSelects_IgnoresSiteFlagMismatch(t *testing.T) {

	sites := []Site{{CloudID: "1", URL: "https://acme.atlassian.net"}}
	got, err := SelectSite(sites, "someone-else.atlassian.net", failPrompt(t))
	if err != nil {
		t.Fatalf("SelectSite: %v", err)
	}
	if got != sites[0] {
		t.Errorf("got %+v, want %+v", got, sites[0])
	}
}

func TestSelectSite_MultipleWithMatchingSiteFlag(t *testing.T) {
	sites := []Site{
		{CloudID: "1", URL: "https://acme.atlassian.net"},
		{CloudID: "2", URL: "https://acme-sandbox.atlassian.net"},
	}
	got, err := SelectSite(sites, "acme-sandbox.atlassian.net", failPrompt(t))
	if err != nil {
		t.Fatalf("SelectSite: %v", err)
	}
	if got != sites[1] {
		t.Errorf("got %+v, want %+v", got, sites[1])
	}
}

func TestSelectSite_MultipleWithMismatchedSiteFlag(t *testing.T) {
	sites := []Site{
		{CloudID: "1", URL: "https://acme.atlassian.net"},
		{CloudID: "2", URL: "https://acme-sandbox.atlassian.net"},
	}
	_, err := SelectSite(sites, "nope.atlassian.net", failPrompt(t))
	if err == nil {
		t.Fatal("expected error for mismatched --site, got nil")
	}
}

func TestSelectSite_MultipleWithoutSiteFlagPrompts(t *testing.T) {
	sites := []Site{
		{CloudID: "1", URL: "https://acme.atlassian.net"},
		{CloudID: "2", URL: "https://acme-sandbox.atlassian.net"},
	}
	called := false
	got, err := SelectSite(sites, "", func(got []Site) (int, error) {
		called = true
		if len(got) != 2 {
			t.Fatalf("prompt received %d sites, want 2", len(got))
		}
		return 1, nil
	})
	if err != nil {
		t.Fatalf("SelectSite: %v", err)
	}
	if !called {
		t.Fatal("prompt was not called for multiple sites with no --site")
	}
	if got != sites[1] {
		t.Errorf("got %+v, want %+v", got, sites[1])
	}
}

func TestSelectSite_PromptErrorPropagates(t *testing.T) {
	sites := []Site{
		{CloudID: "1", URL: "https://acme.atlassian.net"},
		{CloudID: "2", URL: "https://acme-sandbox.atlassian.net"},
	}
	wantErr := errors.New("boom")
	_, err := SelectSite(sites, "", func([]Site) (int, error) {
		return 0, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestSite_Hostname(t *testing.T) {
	s := Site{URL: "https://acme.atlassian.net"}
	if got := s.Hostname(); got != "acme.atlassian.net" {
		t.Errorf("Hostname() = %q, want %q", got, "acme.atlassian.net")
	}
}

func TestSelectSite_RejectsInvalidAccessibleResourceURL(t *testing.T) {
	invalid := []string{
		"http://acme.atlassian.net",
		"https://acme.atlassian.net@evil.example.com",
		"https://acme.atlassian.net/path",
		"https://acme.atlassian.net:8443",
		"https://acme.atlassian.net?x=y",
		"https://acme.atlassian.net\x1b[31m",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			_, err := SelectSite([]Site{{CloudID: "1", URL: raw}}, "", failPrompt(t))
			if err == nil || err.Error() != "received an invalid Jira site from Atlassian" {
				t.Errorf("SelectSite(%q) error = %v, want safe invalid-site error", raw, err)
			}
		})
	}
}

func TestSelectSite_NormalizesTrailingSlash(t *testing.T) {
	got, err := SelectSite([]Site{{CloudID: "1", URL: "https://acme.atlassian.net/"}}, "", failPrompt(t))
	if err != nil {
		t.Fatalf("SelectSite: %v", err)
	}
	if got.URL != "https://acme.atlassian.net" || got.Hostname() != "acme.atlassian.net" {
		t.Errorf("selected site = %+v, want normalized HTTPS site", got)
	}
}

func failPrompt(t *testing.T) PromptFunc {
	t.Helper()
	return func([]Site) (int, error) {
		t.Fatal("prompt should not have been called")
		return 0, nil
	}
}
