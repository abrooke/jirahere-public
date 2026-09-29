package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type CommentResult struct {
	ID string `json:"id"`
}

type CommentAuthor struct {
	DisplayName string `json:"displayName"`
}

type Comment struct {
	ID      string          `json:"id"`
	Author  CommentAuthor   `json:"author"`
	Created string          `json:"created"`
	Body    json.RawMessage `json:"body"`
}

type commentListPage struct {
	Total    int       `json:"total"`
	Comments []Comment `json:"comments"`
}

const commentListPageSize = 100

const maxCommentListPages = 5000

func (c *Client) ListComments(ctx context.Context, key string) ([]Comment, error) {
	return c.listComments(ctx, key, maxCommentListPages)
}

func (c *Client) listComments(ctx context.Context, key string, maxPages int) ([]Comment, error) {
	comments := []Comment{}
	startAt := 0
	for pages := 0; ; pages++ {
		if pages >= maxPages {
			return comments, fmt.Errorf("jira: ListComments(%q): exceeded %d-page ceiling without reaching the last page", key, maxPages)
		}

		reqURL := fmt.Sprintf("%s/rest/api/3/issue/%s/comment?startAt=%d&maxResults=%d", c.BaseURL, url.PathEscape(key), startAt, commentListPageSize)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return comments, fmt.Errorf("could not build request: %w", err)
		}
		req.Header.Set("Authorization", c.AuthHeader)
		req.Header.Set("Accept", "application/json")

		c.debugRequest(req)
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			c.debugf("request failed")
			return comments, err
		}
		body, err := c.readIssueResponse(resp)
		_ = resp.Body.Close()
		if err != nil {
			return comments, fmt.Errorf("could not read response: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return comments, &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
		}

		var page commentListPage
		if err := json.Unmarshal(body, &page); err != nil {
			return comments, fmt.Errorf("could not parse response: %w", err)
		}
		comments = append(comments, page.Comments...)
		startAt += len(page.Comments)
		if len(page.Comments) == 0 || startAt >= page.Total {
			return comments, nil
		}
	}
}

func (c *Client) AddComment(ctx context.Context, key string, body json.RawMessage) (*CommentResult, error) {
	payload, err := json.Marshal(map[string]json.RawMessage{"body": body})
	if err != nil {
		return nil, fmt.Errorf("could not encode comment request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/rest/api/3/issue/"+url.PathEscape(key)+"/comment", bytes.NewReader(payload))
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

	respBody, err := c.readIssueUpdateResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("could not read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var result CommentResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}
	return &result, nil
}
