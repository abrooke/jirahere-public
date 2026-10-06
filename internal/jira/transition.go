package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Transition struct {
	ID           string
	ToStatusName string
}

type transitionsEntry struct {
	ID string `json:"id"`
	To struct {
		Name string `json:"name"`
	} `json:"to"`
}

type transitionsResponse struct {
	Transitions []transitionsEntry `json:"transitions"`
}

func (c *Client) Transitions(ctx context.Context, issueKey string) ([]Transition, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/rest/api/3/issue/"+url.PathEscape(issueKey)+"/transitions", nil)
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIssueResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("could not read response: %w", err)
	}
	if int64(len(body)) > maxIssueResponseBytes {
		c.debugf("response: HTTP %d (body exceeds %d-byte limit)", resp.StatusCode, maxIssueResponseBytes)
		return nil, &ResponseTooLargeError{Limit: maxIssueResponseBytes}
	}
	c.debugResponse(resp, body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var parsed transitionsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}

	out := make([]Transition, 0, len(parsed.Transitions))
	for _, t := range parsed.Transitions {
		out = append(out, Transition{ID: t.ID, ToStatusName: t.To.Name})
	}
	return out, nil
}

func (c *Client) DoTransition(ctx context.Context, issueKey, transitionID string) error {
	payload := map[string]any{
		"transition": map[string]string{"id": transitionID},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("could not encode transition request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/rest/api/3/issue/"+url.PathEscape(issueKey)+"/transitions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Authorization", c.AuthHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	c.debugRequest(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.debugf("request failed")
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := c.readIssueUpdateResponse(resp)
	if err != nil {
		return fmt.Errorf("could not read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	return nil
}
