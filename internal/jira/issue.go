package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const issueFieldsParam = "summary,description,labels,issuetype,project,status,parent,created"

const relationFieldsParam = "parent,summary,issuetype,status"

const maxIssueResponseBytes int64 = 1 << 20

type IssueType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subtask *bool  `json:"subtask"`
}

type Project struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

type Status struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ParentRefFields struct {
	Summary   string    `json:"summary"`
	Status    Status    `json:"status"`
	IssueType IssueType `json:"issuetype"`
}

type ParentRef struct {
	Key    string          `json:"key"`
	Fields ParentRefFields `json:"fields"`
}

type IssueFields struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description,omitempty"`
	Labels      []string        `json:"labels"`
	IssueType   IssueType       `json:"issuetype"`
	Project     Project         `json:"project"`
	Status      Status          `json:"status"`

	Parent *ParentRef `json:"parent,omitempty"`

	Created time.Time `json:"-"`
}

func (f *IssueFields) UnmarshalJSON(data []byte) error {
	type plain IssueFields
	aux := struct {
		*plain
		Created *string `json:"created"`
	}{plain: (*plain)(f)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	f.Created = time.Time{}
	if aux.Created == nil || *aux.Created == "" {
		return nil
	}
	created, err := time.Parse(jiraDateTimeLayout, *aux.Created)
	if err != nil {
		return fmt.Errorf("could not parse created timestamp %q: %w", *aux.Created, err)
	}
	f.Created = created
	return nil
}

type Issue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
}

func (c *Client) GetIssue(ctx context.Context, key string) (*Issue, error) {
	return c.getIssue(ctx, key, issueFieldsParam)
}

func (c *Client) GetIssueRelation(ctx context.Context, key string) (*Issue, error) {
	return c.getIssue(ctx, key, relationFieldsParam)
}

func (c *Client) getIssue(ctx context.Context, key, fields string) (*Issue, error) {
	reqURL := fmt.Sprintf("%s/rest/api/3/issue/%s?fields=%s", c.BaseURL, url.PathEscape(key), fields)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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

	var issue Issue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}
	return &issue, nil
}

func (c *Client) readIssueResponse(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIssueResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxIssueResponseBytes {
		c.debugf("response: HTTP %d (body exceeds %d-byte limit)", resp.StatusCode, maxIssueResponseBytes)
		return nil, &ResponseTooLargeError{Limit: maxIssueResponseBytes}
	}
	c.debugResponse(resp, body)
	return body, nil
}

type CreateIssueFields struct {
	ProjectID   string
	IssueTypeID string
	Summary     string
	Description json.RawMessage
	Labels      []string
	ParentKey   string
}

type CreateIssueResult struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func (c *Client) CreateIssue(ctx context.Context, in CreateIssueFields) (*CreateIssueResult, error) {
	fields := map[string]any{
		"project":   map[string]string{"id": in.ProjectID},
		"issuetype": map[string]string{"id": in.IssueTypeID},
		"summary":   in.Summary,
		"labels":    in.Labels,
	}
	if len(in.Description) > 0 {
		fields["description"] = in.Description
	}
	if in.ParentKey != "" {
		fields["parent"] = map[string]string{"key": in.ParentKey}
	}
	body, err := json.Marshal(map[string]any{"fields": fields})
	if err != nil {
		return nil, fmt.Errorf("could not encode create request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/rest/api/3/issue", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Authorization", c.AuthHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	c.debugRequest(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.debugf("request failed")
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response: %w", err)
	}
	c.debugResponse(resp, respBody)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var result CreateIssueResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}
	return &result, nil
}
