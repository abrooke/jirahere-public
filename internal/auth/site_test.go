package auth

import "testing"

func TestValidateSiteHostname_Valid(t *testing.T) {
	valid := []string{
		"acme.atlassian.net",
		"my-team.atlassian.net",
		"localhost",
		"a.b.c.example.com",
	}
	for _, site := range valid {
		if err := ValidateSiteHostname(site); err != nil {
			t.Errorf("ValidateSiteHostname(%q) = %v, want nil", site, err)
		}
	}
}

func TestValidateSiteHostname_Invalid(t *testing.T) {
	invalid := []string{
		"",
		"acme.atlassian.net@evil.example.com",
		"evil.example.com/acme.atlassian.net",
		"https://acme.atlassian.net",
		"user:pass@acme.atlassian.net",
		"acme.atlassian.net:8080",
		"acme atlassian net",
		"-acme.atlassian.net",
		"acme.atlassian.net-",
	}
	for _, site := range invalid {
		if err := ValidateSiteHostname(site); err == nil {
			t.Errorf("ValidateSiteHostname(%q) = nil, want an error", site)
		}
	}
}
