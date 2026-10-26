package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("jira: unexpected status %d: %s", e.StatusCode, e.Body)
}

func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.StatusCode == http.StatusNotFound
}

type Client struct {
	BaseURL    string
	AuthHeader string
	HTTPClient *http.Client

	Debug io.Writer
}

func NewClient(baseURL, authHeader string) *Client {
	return &Client{
		BaseURL:    baseURL,
		AuthHeader: authHeader,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type Myself struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

func (c *Client) Myself(ctx context.Context) (*Myself, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/rest/api/3/myself", nil)
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

	var me Myself
	if err := json.Unmarshal(body, &me); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}
	return &me, nil
}

func (c *Client) debugf(format string, args ...any) {
	if c.Debug == nil {
		return
	}
	_, _ = fmt.Fprintf(c.Debug, "[debug] "+format+"\n", args...)
}

func (c *Client) debugRequest(req *http.Request) {
	if c.Debug == nil {
		return
	}
	c.debugf("request: %s", req.Method)
}

func (c *Client) debugResponse(resp *http.Response, body []byte) {
	if c.Debug == nil {
		return
	}
	c.debugf("response: HTTP %d (body: %d bytes)", resp.StatusCode, len(body))
}
