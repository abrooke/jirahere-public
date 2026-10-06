package quarter

import (
	"fmt"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

const visionMarker = "_Vision"

func FindVisionEpic(items []QuarterIssue) (QuarterIssue, bool, error) {
	var matches []QuarterIssue
	for _, it := range items {
		if strings.Contains(it.Summary, visionMarker) {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 0:
		return QuarterIssue{}, false, nil
	case 1:
		return matches[0], true, nil
	}
	keys := make([]string, len(matches))
	for i, m := range matches {
		keys[i] = m.Key
	}
	return QuarterIssue{}, false, fmt.Errorf(
		"ambiguous vision epic: %d work items have %q in their summary (%s); keep exactly one",
		len(matches), visionMarker, strings.Join(keys, ", "),
	)
}

const doneStatusName = "Done"

func SelectActiveVisionItem(children []jira.Issue) (jira.Issue, bool) {
	var best jira.Issue
	found := false
	for _, c := range children {
		if strings.EqualFold(c.Fields.Status.Name, doneStatusName) {
			continue
		}
		if !found || c.Fields.Created.After(best.Fields.Created) {
			best = c
			found = true
		}
	}
	return best, found
}
