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

const maxIssueUpdateResponseBytes int64 = 1 << 20

type ResponseTooLargeError struct {
	Limit int64
}

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("jira: response body exceeds %d-byte limit", e.Limit)
}

func (c *Client) readIssueUpdateResponse(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIssueUpdateResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxIssueUpdateResponseBytes {
		c.debugf("response: HTTP %d (body exceeds %d-byte limit)", resp.StatusCode, maxIssueUpdateResponseBytes)
		return nil, &ResponseTooLargeError{Limit: maxIssueUpdateResponseBytes}
	}
	c.debugResponse(resp, body)
	return body, nil
}

type IssueLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

type issueLinkTypesResponse struct {
	IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
}

func (c *Client) IssueLinkTypes(ctx context.Context) ([]IssueLinkType, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/rest/api/3/issueLinkType", nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response: %w", err)
	}
	c.debugResponse(resp, body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var parsed issueLinkTypesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}
	return parsed.IssueLinkTypes, nil
}

func (c *Client) CreateIssueLink(ctx context.Context, linkTypeName, inwardKey, outwardKey string) error {
	payload := map[string]any{
		"type":         map[string]string{"name": linkTypeName},
		"inwardIssue":  map[string]string{"key": inwardKey},
		"outwardIssue": map[string]string{"key": outwardKey},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("could not encode issue link request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/rest/api/3/issueLink", bytes.NewReader(body))
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

func (c *Client) SetParent(ctx context.Context, issueKey, parentKey string) error {
	payload := map[string]any{
		"fields": map[string]any{
			"parent": map[string]string{"key": parentKey},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("could not encode set-parent request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/rest/api/3/issue/"+url.PathEscape(issueKey), bytes.NewReader(body))
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

func (c *Client) SetSummary(ctx context.Context, issueKey, summary string) error {
	payload := map[string]any{
		"fields": map[string]string{
			"summary": summary,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("could not encode set-summary request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/rest/api/3/issue/"+url.PathEscape(issueKey), bytes.NewReader(body))
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

func (c *Client) SetLabels(ctx context.Context, issueKey string, labels []string) error {
	if labels == nil {
		labels = []string{}
	}
	payload := map[string]any{
		"fields": map[string]any{
			"labels": labels,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("could not encode set-labels request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/rest/api/3/issue/"+url.PathEscape(issueKey), bytes.NewReader(body))
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
