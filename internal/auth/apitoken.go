package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

func BasicAuthHeader(email, token string) string {
	creds := email + ":" + token
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

func ValidateAPIToken(ctx context.Context, siteHostname, email, token string, debug io.Writer) (*jira.Myself, error) {
	if err := ValidateSiteHostname(siteHostname); err != nil {
		return nil, err
	}
	client := jira.NewClient("https://"+siteHostname, BasicAuthHeader(email, token))
	client.Debug = debug
	me, err := client.Myself(ctx)
	if err != nil {
		return nil, fmt.Errorf("validating API token against %s: %w", siteHostname, err)
	}
	return me, nil
}
