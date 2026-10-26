package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const relationsFieldsParam = "parent,status,created,resolutiondate"

const relationsKeyChunk = 100

func (c *Client) SearchRelations(ctx context.Context, keys []string) ([]Issue, error) {
	return c.searchRelations(ctx, keys, maxChildrenOfPages)
}

func (c *Client) searchRelations(ctx context.Context, keys []string, maxPages int) ([]Issue, error) {
	out := []Issue{}
	seen := map[string]bool{}
	for start := 0; start < len(keys); start += relationsKeyChunk {
		end := min(start+relationsKeyChunk, len(keys))
		quoted := make([]string, 0, end-start)
		for _, k := range keys[start:end] {
			quoted = append(quoted, `"`+escapeJQLString(k)+`"`)
		}
		list := strings.Join(quoted, ", ")
		jql := fmt.Sprintf("key in (%s) OR parent in (%s)", list, list)
		err := c.paginateJQLSearch(ctx, jqlSearchParams{
			jql:      jql,
			fields:   relationsFieldsParam,
			label:    fmt.Sprintf("SearchRelations(%d keys)", len(keys)),
			maxPages: maxPages,
		}, func(body []byte) (string, error) {
			var page searchPage
			if err := json.Unmarshal(body, &page); err != nil {
				return "", fmt.Errorf("could not parse response: %w", err)
			}
			for _, is := range page.Issues {
				if !seen[is.Key] {
					seen[is.Key] = true
					out = append(out, is)
				}
			}
			return page.NextPageToken, nil
		})
		if err != nil {
			var se *StatusError
			if errors.As(err, &se) && se.StatusCode == http.StatusBadRequest {
				err = fmt.Errorf("jira rejected the parent/child lookup (a listed key may no longer exist, so the cached quarter inventory may be stale; refresh the inventory and retry): %w", err)
			}
			return out, err
		}
	}
	return out, nil
}
