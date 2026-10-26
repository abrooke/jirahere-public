package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const accessibleResourcesURL = "https://api.atlassian.com/oauth/token/accessible-resources"

type AccessibleResource struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Name string `json:"name"`
}

func AccessibleResources(ctx context.Context, accessToken string) ([]AccessibleResource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, accessibleResourcesURL, nil)
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

	var resources []AccessibleResource
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}
	return resources, nil
}
