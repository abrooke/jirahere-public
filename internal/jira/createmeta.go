package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type CreateMetadata struct {
	ProjectID  string
	IssueTypes map[string]IssueType
}

func (c *Client) ResolveCreateMetadata(ctx context.Context, projectKey string) (*CreateMetadata, error) {
	endpoint, err := url.Parse(c.BaseURL + "/rest/api/3/issue/createmeta")
	if err != nil {
		return nil, fmt.Errorf("could not build request URL: %w", err)
	}
	q := endpoint.Query()
	q.Set("projectKeys", projectKey)
	q.Set("expand", "projects.issuetypes")
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Authorization", c.AuthHeader)
	req.Header.Set("Accept", "application/json")

	c.debugRequest(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.debugf("request failed")
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := c.readIssueResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("could not read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var response struct {
		Projects []struct {
			ID         string      `json:"id"`
			Key        string      `json:"key"`
			IssueTypes []IssueType `json:"issuetypes"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}

	for _, project := range response.Projects {
		if project.Key != projectKey {
			continue
		}
		result := &CreateMetadata{ProjectID: project.ID, IssueTypes: make(map[string]IssueType, len(project.IssueTypes))}
		for _, issueType := range project.IssueTypes {
			result.IssueTypes[issueType.Name] = issueType
		}
		return result, nil
	}
	return &CreateMetadata{IssueTypes: map[string]IssueType{}}, nil
}
