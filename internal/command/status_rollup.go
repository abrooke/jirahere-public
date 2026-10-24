package command

import (
	"strings"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

const (
	rollupDone       = "Done"
	rollupInProgress = "In Progress"
	rollupToDo       = "To Do"
	rollupBacklog    = "Backlog"
)

func RollupStatus(childStatuses []string) (target string, ok bool) {
	if len(childStatuses) == 0 {
		return "", false
	}
	var done, inProgress, toDo, backlog int
	for _, s := range childStatuses {
		switch {
		case strings.EqualFold(s, rollupDone):
			done++
		case strings.EqualFold(s, rollupInProgress):
			inProgress++
		case strings.EqualFold(s, rollupToDo):
			toDo++
		case strings.EqualFold(s, rollupBacklog):
			backlog++
		}
	}
	n := len(childStatuses)
	switch {
	case done == n:
		return rollupDone, true
	case done > 0:
		return rollupInProgress, true
	case inProgress > 0:
		return rollupInProgress, true
	case toDo > 0:
		return rollupToDo, true
	case backlog == n:
		return rollupBacklog, true
	}
	return "", false
}

type RollupResult struct {
	Key string

	Current string

	Target string

	Matched bool
}

func (r RollupResult) Effective() string {
	if r.Matched {
		return r.Target
	}
	return r.Current
}

func EvaluateRollup(rootKey, rootStatus string, nodes []jira.DescendantNode) []RollupResult {
	children := make(map[string][]jira.DescendantNode, len(nodes))
	for _, n := range nodes {
		children[n.ParentKey] = append(children[n.ParentKey], n)
	}

	var results []RollupResult
	effective := make(map[string]string, len(nodes)+1)
	active := make(map[string]bool, len(nodes)+1)
	var eval func(key, current string) string
	eval = func(key, current string) string {
		kids := children[key]
		if len(kids) == 0 || active[key] {
			return current
		}
		if e, done := effective[key]; done {
			return e
		}
		active[key] = true
		defer delete(active, key)
		statuses := make([]string, len(kids))
		for i, k := range kids {
			statuses[i] = eval(k.Issue.Key, k.Issue.Fields.Status.Name)
		}
		target, ok := RollupStatus(statuses)
		r := RollupResult{Key: key, Current: current, Target: target, Matched: ok}
		results = append(results, r)
		effective[key] = r.Effective()
		return effective[key]
	}
	eval(rootKey, rootStatus)
	return results
}
