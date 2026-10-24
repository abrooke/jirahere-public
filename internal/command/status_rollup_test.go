package command

import (
	"reflect"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

func TestRollupStatus(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
		ok   bool
	}{
		{"rule1 all done", []string{"Done", "Done"}, "Done", true},
		{"rule1 single done", []string{"Done"}, "Done", true},
		{"rule2 some done", []string{"Done", "Backlog"}, "In Progress", true},
		{"rule2 beats rule4 (Done+To Do)", []string{"Done", "To Do"}, "In Progress", true},
		{"rule3 in progress", []string{"In Progress", "Backlog"}, "In Progress", true},
		{"rule3 beats rule4 (In Progress+To Do)", []string{"In Progress", "To Do"}, "In Progress", true},
		{"rule4 any to do", []string{"To Do", "Backlog"}, "To Do", true},
		{"rule4 to do with unknown", []string{"To Do", "Blocked"}, "To Do", true},
		{"rule5 all backlog", []string{"Backlog", "Backlog"}, "Backlog", true},
		{"rule6 backlog+unknown", []string{"Backlog", "Blocked"}, "", false},
		{"rule6 all unknown", []string{"Cancelled"}, "", false},
		{"rule2 done+unknown", []string{"Done", "Cancelled"}, "In Progress", true},
		{"no children", nil, "", false},
		{"case-insensitive", []string{"dOnE", "DONE"}, "Done", true},
		{"case-insensitive in progress", []string{"in progress", "TO DO"}, "In Progress", true},
		{"exact match only", []string{"Done ", "Doing"}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RollupStatus(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("RollupStatus(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func rollupNode(key, parent, status string) jira.DescendantNode {
	var n jira.DescendantNode
	n.Issue.Key = key
	n.Issue.Fields.Status.Name = status
	n.ParentKey = parent
	return n
}

func TestEvaluateRollup(t *testing.T) {
	tests := []struct {
		name       string
		rootStatus string
		nodes      []jira.DescendantNode
		want       []RollupResult
	}{
		{
			name:       "leaf root yields nothing",
			rootStatus: "To Do",
			want:       nil,
		},
		{
			name:       "single level",
			rootStatus: "To Do",
			nodes:      []jira.DescendantNode{rollupNode("A-1", "R", "Done"), rollupNode("A-2", "R", "Done")},
			want:       []RollupResult{{Key: "R", Current: "To Do", Target: "Done", Matched: true}},
		},
		{
			name:       "multi-level uses computed child status, not Jira status",
			rootStatus: "To Do",
			nodes: []jira.DescendantNode{
				rollupNode("M", "R", "To Do"),
				rollupNode("L1", "M", "Done"),
				rollupNode("L2", "M", "Done"),
			},
			want: []RollupResult{
				{Key: "M", Current: "To Do", Target: "Done", Matched: true},
				{Key: "R", Current: "To Do", Target: "Done", Matched: true},
			},
		},
		{
			name:       "propagates partial progress upward",
			rootStatus: "Backlog",
			nodes: []jira.DescendantNode{
				rollupNode("M", "R", "Backlog"),
				rollupNode("L1", "M", "Done"),
				rollupNode("L2", "M", "To Do"),
				rollupNode("S", "R", "Done"),
			},
			want: []RollupResult{
				{Key: "M", Current: "Backlog", Target: "In Progress", Matched: true},
				{Key: "R", Current: "Backlog", Target: "In Progress", Matched: true},
			},
		},
		{
			name:       "no-match keeps current status and contributes it",
			rootStatus: "To Do",
			nodes: []jira.DescendantNode{
				rollupNode("M", "R", "Blocked"),
				rollupNode("L1", "M", "Backlog"),
				rollupNode("L2", "M", "Cancelled"),
				rollupNode("S", "R", "Backlog"),
			},
			want: []RollupResult{
				{Key: "M", Current: "Blocked", Matched: false},
				{Key: "R", Current: "To Do", Matched: false},
			},
		},
		{
			name:       "case-insensitive names, input order irrelevant",
			rootStatus: "to do",
			nodes: []jira.DescendantNode{
				rollupNode("L1", "M", "DONE"),
				rollupNode("M", "R", "to do"),
			},
			want: []RollupResult{
				{Key: "M", Current: "to do", Target: "Done", Matched: true},
				{Key: "R", Current: "to do", Target: "Done", Matched: true},
			},
		},
		{
			name:       "all backlog children roll up to Backlog",
			rootStatus: "To Do",
			nodes:      []jira.DescendantNode{rollupNode("A-1", "R", "Backlog"), rollupNode("A-2", "R", "backlog")},
			want:       []RollupResult{{Key: "R", Current: "To Do", Target: "Backlog", Matched: true}},
		},
		{
			name:       "orphan nodes are ignored and absent from results",
			rootStatus: "To Do",
			nodes: []jira.DescendantNode{
				rollupNode("A-1", "R", "Done"),
				rollupNode("O", "GONE", "To Do"),
				rollupNode("OL", "O", "To Do"),
			},
			want: []RollupResult{{Key: "R", Current: "To Do", Target: "Done", Matched: true}},
		},
		{
			name:       "duplicate key evaluated once; revisit uses computed status",
			rootStatus: "To Do",
			nodes: []jira.DescendantNode{
				rollupNode("X", "R", "To Do"),
				rollupNode("X", "R", "To Do"),
				rollupNode("L", "X", "Done"),
			},
			want: []RollupResult{
				{Key: "X", Current: "To Do", Target: "Done", Matched: true},
				{Key: "R", Current: "To Do", Target: "Done", Matched: true},
			},
		},
		{
			name:       "ParentKey cycle terminates with defined results",
			rootStatus: "Backlog",
			nodes: []jira.DescendantNode{
				rollupNode("A", "R", "To Do"),
				rollupNode("B", "A", "Backlog"),
				rollupNode("A", "B", "To Do"),
			},
			want: []RollupResult{
				{Key: "B", Current: "Backlog", Target: "To Do", Matched: true},
				{Key: "A", Current: "To Do", Target: "To Do", Matched: true},
				{Key: "R", Current: "Backlog", Target: "To Do", Matched: true},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateRollup("R", tc.rootStatus, tc.nodes)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("EvaluateRollup = %+v, want %+v", got, tc.want)
			}
		})
	}
}
