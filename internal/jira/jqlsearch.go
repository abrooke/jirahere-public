package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type jqlSearchParams struct {
	jql      string
	fields   string
	label    string
	maxPages int
}

func (c *Client) paginateJQLSearch(ctx context.Context, p jqlSearchParams, onPage func(body []byte) (nextPageToken string, err error)) error {
	pageToken := ""
	seenTokens := map[string]bool{}
	for pages := 0; ; pages++ {
		if pages >= p.maxPages {
			return fmt.Errorf("jira: %s: exceeded %d-page ceiling without reaching the last page", p.label, p.maxPages)
		}

		values := url.Values{}
		values.Set("jql", p.jql)
		values.Set("fields", p.fields)
		values.Set("maxResults", fmt.Sprintf("%d", jqlChildrenPageSize))
		if pageToken != "" {
			values.Set("nextPageToken", pageToken)
		}
		reqURL := fmt.Sprintf("%s/rest/api/3/search/jql?%s", c.BaseURL, values.Encode())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return fmt.Errorf("could not build request: %w", err)
		}
		req.Header.Set("Authorization", c.AuthHeader)
		req.Header.Set("Accept", "application/json")

		c.debugRequest(req)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			c.debugf("request failed")
			return err
		}
		body, err := c.readIssueResponse(resp)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("could not read response: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
		}

		nextPageToken, err := onPage(body)
		if err != nil {
			return err
		}
		if nextPageToken == "" {
			return nil
		}
		if seenTokens[nextPageToken] {
			return fmt.Errorf("jira: %s: pagination cursor repeated a token already seen after %d page(s); pagination did not advance", p.label, pages+1)
		}
		seenTokens[nextPageToken] = true
		pageToken = nextPageToken
	}
}
