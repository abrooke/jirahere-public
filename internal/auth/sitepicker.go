package auth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Site struct {
	CloudID string
	URL     string
}

func (s Site) Hostname() string {
	return strings.TrimPrefix(strings.TrimPrefix(s.URL, "https://"), "http://")
}

func (s Site) normalized() (Site, error) {
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" ||
		u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Site{}, errors.New("received an invalid Jira site from Atlassian")
	}
	host := u.Hostname()
	if u.Host == "" || host == "" || u.Host != host {
		return Site{}, errors.New("received an invalid Jira site from Atlassian")
	}
	if err := ValidateSiteHostname(host); err != nil {
		return Site{}, errors.New("received an invalid Jira site from Atlassian")
	}
	s.URL = "https://" + host
	return s, nil
}

type PromptFunc func(sites []Site) (int, error)

var ErrNoSites = errors.New("no accessible Jira sites")

type ErrSiteMismatch struct {
	Requested string
	Available []string
}

func (e *ErrSiteMismatch) Error() string {
	return fmt.Sprintf("site %q not among authorized sites: %s", e.Requested, strings.Join(e.Available, ", "))
}

func SelectSite(sites []Site, siteFlag string, prompt PromptFunc) (Site, error) {
	if len(sites) == 0 {
		return Site{}, ErrNoSites
	}

	validated := make([]Site, len(sites))
	for i, site := range sites {
		var err error
		validated[i], err = site.normalized()
		if err != nil {
			return Site{}, err
		}
	}
	sites = validated

	if len(sites) == 1 {
		return sites[0], nil
	}

	if siteFlag != "" {
		for _, s := range sites {
			if s.Hostname() == siteFlag {
				return s, nil
			}
		}
		return Site{}, &ErrSiteMismatch{Requested: siteFlag, Available: joinHostnames(sites)}
	}

	idx, err := prompt(sites)
	if err != nil {
		return Site{}, err
	}
	if idx < 0 || idx >= len(sites) {
		return Site{}, fmt.Errorf("invalid site selection")
	}
	return sites[idx], nil
}

func joinHostnames(sites []Site) []string {
	names := make([]string, len(sites))
	for i, s := range sites {
		names[i] = s.Hostname()
	}
	return names
}
