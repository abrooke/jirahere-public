package plan

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

type Client interface {
	Inventory(ctx context.Context) ([]quarter.QuarterIssue, error)
	SearchRelations(ctx context.Context, keys []string) ([]jira.Issue, error)
	GetIssue(ctx context.Context, key string) (*jira.Issue, error)
}

type Vision struct {
	Key         string
	Summary     string
	Description string
}

type PlanEpic struct {
	Key     string
	Summary string
}

type Done struct {
	Key      string
	Resolved time.Time
}

type Item struct {
	Key       string
	Summary   string
	Status    string
	Created   time.Time
	Updated   time.Time
	Resolved  *time.Time
	Assignee  string
	ParentKey string

	AgeDays int

	LastDone *Done
}

type Facts struct {
	Vision   *Vision
	PlanEpic *PlanEpic
	Items    []Item
}

func Gather(ctx context.Context, client Client, window Range, quarterLabel string) (Facts, error) {
	inv, err := client.Inventory(ctx)
	if err != nil {
		return Facts{}, fmt.Errorf("could not list the quarter's work items: %w", err)
	}

	visionEpic, hasVisionEpic, err := quarter.FindVisionEpic(inv)
	if err != nil {
		return Facts{}, err
	}
	planEpic, hasPlanEpic, err := quarter.FindPlanEpic(inv, quarterLabel)
	if err != nil {
		return Facts{}, err
	}

	keys := make([]string, len(inv))
	for i, it := range inv {
		keys[i] = it.Key
	}
	rel, err := client.SearchRelations(ctx, keys)
	if err != nil {
		return Facts{}, fmt.Errorf("could not read parents and children of the quarter's work items: %w", err)
	}

	parentOf := map[string]string{}
	children := map[string][]jira.Issue{}
	for _, r := range rel {
		if r.Fields.Parent == nil {
			continue
		}
		pk := r.Fields.Parent.Key
		parentOf[r.Key] = pk
		children[pk] = append(children[pk], r)
	}

	facts := Facts{Items: make([]Item, 0, len(inv))}
	for _, it := range inv {
		facts.Items = append(facts.Items, Item{
			Key:       it.Key,
			Summary:   it.Summary,
			Status:    it.Status.Name,
			Created:   it.Created,
			Updated:   it.Updated,
			Resolved:  it.Resolved,
			Assignee:  it.Assignee,
			ParentKey: parentOf[it.Key],
			AgeDays:   ageDays(it.Updated, window.Start),
			LastDone:  lastDone(children[it.Key]),
		})
	}
	sort.Slice(facts.Items, func(i, j int) bool { return keyLess(facts.Items[i].Key, facts.Items[j].Key) })

	if hasPlanEpic {
		facts.PlanEpic = &PlanEpic{Key: planEpic.Key, Summary: planEpic.Summary}
	}
	if hasVisionEpic {
		if active, ok := quarter.SelectActiveVisionItem(children[visionEpic.Key]); ok {
			full, err := client.GetIssue(ctx, active.Key)
			if err != nil {
				return Facts{}, fmt.Errorf("could not read Vision item %s: %w", active.Key, err)
			}
			facts.Vision = &Vision{
				Key:         full.Key,
				Summary:     full.Fields.Summary,
				Description: adf.PlainText(full.Fields.Description),
			}
		}
	}
	return facts, nil
}

func lastDone(children []jira.Issue) *Done {
	var best *Done
	for _, c := range children {
		if !quarter.IsDone(c.Fields.Status.Name) || c.Fields.Resolved == nil {
			continue
		}
		r := *c.Fields.Resolved
		if best == nil || r.After(best.Resolved) || (r.Equal(best.Resolved) && keyLess(c.Key, best.Key)) {
			best = &Done{Key: c.Key, Resolved: r}
		}
	}
	return best
}

func keyLess(a, b string) bool {
	pa, na, oka := splitKey(a)
	pb, nb, okb := splitKey(b)
	if pa != pb {
		return pa < pb
	}
	if oka && okb && na != nb {
		return na < nb
	}
	return a < b
}

func splitKey(k string) (prefix string, n int, ok bool) {
	i := strings.LastIndexByte(k, '-')
	if i < 0 {
		return k, 0, false
	}
	n, err := strconv.Atoi(k[i+1:])
	if err != nil {
		return k, 0, false
	}
	return k[:i], n, true
}

func ageDays(updated, start time.Time) int {
	uy, um, ud := updated.In(start.Location()).Date()
	sy, sm, sd := start.Date()
	u := time.Date(uy, um, ud, 0, 0, 0, 0, time.UTC)
	s := time.Date(sy, sm, sd, 0, 0, 0, 0, time.UTC)
	return int(s.Sub(u).Hours() / 24)
}
