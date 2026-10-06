package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var identityURL = "https://api.atlassian.com/me"

type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func Me(ctx context.Context, accessToken string) (*Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, identityURL, nil)
	if err != nil {
		return nil, fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var id Identity
	if err := json.Unmarshal(body, &id); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}
	return &id, nil
}
