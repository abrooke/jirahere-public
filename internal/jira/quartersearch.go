package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const jiraDateTimeLayout = "2006-01-02T15:04:05.000-0700"

const quarterIssueFieldsParam = "summary,created,updated,status,resolutiondate,assignee"

type QuarterIssue struct {
	Key      string
	Summary  string
	Status   Status
	Created  time.Time
	Updated  time.Time
	Resolved *time.Time
	Assignee string
}

type quarterSearchPage struct {
	Issues []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary        string `json:"summary"`
			Status         Status `json:"status"`
			Created        string `json:"created"`
			Updated        string `json:"updated"`
			ResolutionDate string `json:"resolutiondate"`
			Assignee       *struct {
				DisplayName string `json:"displayName"`
			} `json:"assignee"`
		} `json:"fields"`
	} `json:"issues"`
	NextPageToken string `json:"nextPageToken"`
}

func (c *Client) SearchQuarterIssues(ctx context.Context, projectKey, quarterLabel string) ([]QuarterIssue, error) {
	return c.searchQuarterIssues(ctx, projectKey, quarterLabel, maxChildrenOfPages)
}

type quarterUpdateProbePage struct {
	Issues []struct {
		Fields struct {
			Updated string `json:"updated"`
		} `json:"fields"`
	} `json:"issues"`
}

func (c *Client) LatestQuarterUpdate(ctx context.Context, projectKey, quarterLabel string) (time.Time, bool, error) {
	jql := fmt.Sprintf(
		`project = "%s" AND labels = "%s" ORDER BY updated DESC`,
		escapeJQLString(projectKey),
		escapeJQLString(quarterLabel),
	)

	values := url.Values{}
	values.Set("jql", jql)
	values.Set("fields", "updated")
	values.Set("maxResults", "1")
	reqURL := fmt.Sprintf("%s/rest/api/3/search/jql?%s", c.BaseURL, values.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Authorization", c.AuthHeader)
	req.Header.Set("Accept", "application/json")

	c.debugRequest(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.debugf("request failed")
		return time.Time{}, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := c.readIssueResponse(resp)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("could not read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return time.Time{}, false, &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var page quarterUpdateProbePage
	if err := json.Unmarshal(body, &page); err != nil {
		return time.Time{}, false, fmt.Errorf("could not parse response: %w", err)
	}
	if len(page.Issues) == 0 {
		return time.Time{}, false, nil
	}
	raw := page.Issues[0].Fields.Updated
	updated, err := time.Parse(jiraDateTimeLayout, raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf(
			"jira: LatestQuarterUpdate(%q, %q): could not parse updated timestamp %q: %w",
			projectKey, quarterLabel, raw, err,
		)
	}
	return updated, true, nil
}

func (c *Client) searchQuarterIssues(ctx context.Context, projectKey, quarterLabel string, maxPages int) ([]QuarterIssue, error) {
	jql := fmt.Sprintf(
		`project = "%s" AND labels = "%s"`,
		escapeJQLString(projectKey),
		escapeJQLString(quarterLabel),
	)
	label := fmt.Sprintf("SearchQuarterIssues(%q, %q)", projectKey, quarterLabel)

	issues := []QuarterIssue{}
	err := c.paginateJQLSearch(ctx, jqlSearchParams{
		jql:      jql,
		fields:   quarterIssueFieldsParam,
		label:    label,
		maxPages: maxPages,
	}, func(body []byte) (string, error) {
		var page quarterSearchPage
		if err := json.Unmarshal(body, &page); err != nil {
			return "", fmt.Errorf("could not parse response: %w", err)
		}
		for _, raw := range page.Issues {
			created, err := time.Parse(jiraDateTimeLayout, raw.Fields.Created)
			if err != nil {
				return "", fmt.Errorf("jira: %s: could not parse created timestamp %q for %s: %w", label, raw.Fields.Created, raw.Key, err)
			}
			updated, err := time.Parse(jiraDateTimeLayout, raw.Fields.Updated)
			if err != nil {
				return "", fmt.Errorf("jira: %s: could not parse updated timestamp %q for %s: %w", label, raw.Fields.Updated, raw.Key, err)
			}
			var resolved *time.Time
			if raw.Fields.ResolutionDate != "" {
				r, err := time.Parse(jiraDateTimeLayout, raw.Fields.ResolutionDate)
				if err != nil {
					return "", fmt.Errorf("jira: %s: could not parse resolutiondate timestamp %q for %s: %w", label, raw.Fields.ResolutionDate, raw.Key, err)
				}
				resolved = &r
			}
			assignee := ""
			if raw.Fields.Assignee != nil {
				assignee = raw.Fields.Assignee.DisplayName
			}
			issues = append(issues, QuarterIssue{
				Key:      raw.Key,
				Summary:  raw.Fields.Summary,
				Status:   raw.Fields.Status,
				Created:  created,
				Updated:  updated,
				Resolved: resolved,
				Assignee: assignee,
			})
		}
		return page.NextPageToken, nil
	})
	return issues, err
}
