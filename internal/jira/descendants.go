package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const jqlChildrenPageSize = 100

const maxChildrenOfPages = 5000

const maxDescendantNodes = 10000

type searchPage struct {
	Issues        []Issue `json:"issues"`
	NextPageToken string  `json:"nextPageToken"`
}

func escapeJQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func (c *Client) ChildrenOf(ctx context.Context, key string) ([]Issue, error) {
	return c.childrenOf(ctx, key, maxChildrenOfPages)
}

func (c *Client) childrenOf(ctx context.Context, key string, maxPages int) ([]Issue, error) {
	jql := fmt.Sprintf(`parent = "%s"`, escapeJQLString(key))

	children := []Issue{}
	err := c.paginateJQLSearch(ctx, jqlSearchParams{
		jql:      jql,
		fields:   issueFieldsParam,
		label:    fmt.Sprintf("ChildrenOf(%q)", key),
		maxPages: maxPages,
	}, func(body []byte) (string, error) {
		var page searchPage
		if err := json.Unmarshal(body, &page); err != nil {
			return "", fmt.Errorf("could not parse response: %w", err)
		}
		children = append(children, page.Issues...)
		return page.NextPageToken, nil
	})
	return children, err
}

type DescendantNode struct {
	Issue     Issue
	Depth     int
	ParentKey string
}

type DescendantFailure struct {
	Key       string
	ParentKey string
	Depth     int
	Err       error
}

type DescendantWalkResult struct {
	Nodes    []DescendantNode
	Failures []DescendantFailure
}

func (c *Client) WalkDescendants(ctx context.Context, rootKey string) (*DescendantWalkResult, error) {
	return c.walkDescendants(ctx, rootKey, false, nil, maxDescendantNodes)
}

func (c *Client) WalkDescendantsFromChildren(ctx context.Context, rootKey string, children []Issue) (*DescendantWalkResult, error) {
	return c.walkDescendants(ctx, rootKey, true, children, maxDescendantNodes)
}

func (c *Client) walkDescendants(ctx context.Context, rootKey string, rootChildrenSupplied bool, suppliedRootChildren []Issue, maxNodes int) (*DescendantWalkResult, error) {
	result := &DescendantWalkResult{Nodes: []DescendantNode{}, Failures: []DescendantFailure{}}
	visited := map[string]bool{rootKey: true}

	type queueItem struct {
		key       string
		parentKey string
		depth     int
	}
	queue := []queueItem{{key: rootKey, parentKey: "", depth: 0}}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		cur := queue[0]
		queue = queue[1:]

		var children []Issue
		var err error
		if cur.depth == 0 && rootChildrenSupplied {
			children = suppliedRootChildren
		} else {
			children, err = c.ChildrenOf(ctx, cur.key)
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			result.Failures = append(result.Failures, DescendantFailure{
				Key:       cur.key,
				ParentKey: cur.parentKey,
				Depth:     cur.depth,
				Err:       err,
			})
		}

		for _, child := range children {
			if visited[child.Key] {
				continue
			}
			if len(result.Nodes) >= maxNodes {
				return result, fmt.Errorf("jira: WalkDescendants(%q): exceeded %d-node ceiling; walk stopped with a partial result", rootKey, maxNodes)
			}
			visited[child.Key] = true

			depth := cur.depth + 1
			result.Nodes = append(result.Nodes, DescendantNode{
				Issue:     child,
				Depth:     depth,
				ParentKey: cur.key,
			})
			queue = append(queue, queueItem{key: child.Key, parentKey: cur.key, depth: depth})
		}
	}

	return result, nil
}
