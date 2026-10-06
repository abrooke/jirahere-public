package auth

import (
	"fmt"
	"regexp"
)

var hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

func ValidateSiteHostname(site string) error {
	if site == "" || len(site) > 253 || !hostnamePattern.MatchString(site) {
		return fmt.Errorf("%q is not a valid Jira site (expected a bare hostname like yourteam.atlassian.net, with no \"https://\", path, or \"@\")", site)
	}
	return nil
}
