package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/auth"
	contextoutput "github.com/aslanbrooke/jirahere/internal/context"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

var contextGetValueFlags = map[string]bool{"level": true, "profile": true}

const maxContextAncestors = 10000

const maxContextAncestorOutputBytes = 1 << 20

var contextGetLevel2Timeout = 2 * time.Minute

type contextGetOptions struct {
	level   int
	format  string
	profile string
}

func splitContextGetArgs(args []string) ([]string, []string) {
	var positional, flagArgs []string
	terminated := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if terminated {
			positional = append(positional, a)
			continue
		}
		if a == "--" {
			terminated = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if (contextGetValueFlags[name] || !isContextGetFlag(name)) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return positional, flagArgs
}

func isContextGetFlag(name string) bool {
	return contextGetValueFlags[name] || name == "json" || name == "md"
}

func runContext(args []string) error {
	if hasHelpToken(args) {

		return runContextGet([]string{"--help"})
	}
	if len(args) == 0 || args[0] != "get" {
		return usagef("usage: jirahere context get <id> [--level {0,1,2,3}] [--json|--md] [--profile <name>]")
	}
	return runContextGet(args[1:])
}

func runContextGet(args []string) error {
	key, opts, err := parseContextGetOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {

			return err
		}
		var silent *errSilent
		if errors.As(err, &silent) {
			return err
		}
		return usagef("%s.", err)
	}
	provider, err := auth.LoadProvider(opts.profile)
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(opts.profile))
		}
		return failf("jirahere: could not load Jira credentials. Run \"%s\" again.", loginRemediation(opts.profile))
	}
	creds, err := provider.Credentials(context.Background())
	if err != nil {
		return refreshCredentialsFailureFor(err, opts.profile)
	}
	var client jira.AccessStrategy = jira.NewClient(creds.BaseURL, creds.AuthHeader)
	issue, err := client.GetIssue(context.Background(), key)
	if err != nil {
		if jira.IsNotFound(err) {
			return failf("jirahere: context get %s: item was not found (404).", key)
		}
		return failf("jirahere: context get %s: could not validate item existence: %s.", key, renderJiraError(err))
	}

	switch opts.level {
	case 0:
		return renderContextGetOutput(newContextGetOutput(issue), opts.format)
	case 1:
		children, err := client.ChildrenOf(context.Background(), key)
		if err != nil {
			return failf("jirahere: context get %s: could not list immediate children: %s.", key, renderJiraError(err))
		}
		parent, err := resolveContextParent(context.Background(), client, issue.Fields.Parent)
		if err != nil {
			return failf("jirahere: context get %s: could not read parent: %s.", key, renderJiraError(err))
		}
		return renderContextGetOutput(newContextGetOutputLevel1(issue, children, parent), opts.format)
	case 2:
		ctx, cancel := context.WithTimeout(context.Background(), contextGetLevel2Timeout)
		defer cancel()

		children, err := client.ChildrenOf(ctx, key)
		if err != nil {
			return failf("jirahere: context get %s: could not list immediate children: %s.", key, renderJiraError(err))
		}
		ancestors, err := walkContextAncestors(ctx, client, issue, maxContextAncestors, maxContextAncestorOutputBytes, func(rel contextoutput.Relation, first bool) int {
			return ancestorRenderedContribution(opts.format, rel, first)
		})
		if err != nil {
			var limitErr *contextAncestorLimitError
			if errors.As(err, &limitErr) {
				return failf("jirahere: context get %s: could not read ancestor chain: %s.", key, limitErr)
			}
			return failf("jirahere: context get %s: could not read ancestor chain: %s.", key, renderJiraError(err))
		}
		parent, err := resolveContextParent(ctx, client, issue.Fields.Parent)
		if err != nil {
			return failf("jirahere: context get %s: could not read parent: %s.", key, renderJiraError(err))
		}
		return renderContextGetOutput(newContextGetOutputLevel2(issue, children, ancestors, parent), opts.format)
	case 3:

		children, err := client.ChildrenOf(context.Background(), key)
		if err != nil {
			return failf("jirahere: context get %s: could not list immediate children: %s.", key, renderJiraError(err))
		}
		walk, err := client.WalkDescendantsFromChildren(context.Background(), key, children)
		if err != nil {
			return failf("jirahere: context get %s: descendant walk did not complete.", key)
		}
		parent, err := resolveContextParent(context.Background(), client, issue.Fields.Parent)
		if err != nil {
			return failf("jirahere: context get %s: could not read parent: %s.", key, renderJiraError(err))
		}
		return renderContextGetOutput(newContextGetOutputLevel3(issue, children, walk, parent), opts.format)
	default:
		return failf("jirahere: context get %s: unsupported context level %d.", key, opts.level)
	}
}

func renderContextGetOutput(out contextoutput.Output, format string) error {
	if format == "json" {
		fmt.Println(renderContextGetJSON(out))
		return nil
	}
	fmt.Println(renderContextGetMD(out))
	return nil
}

func newContextGetOutput(issue *jira.Issue) contextoutput.Output {
	return contextoutput.Output{
		Level: 0,
		Item:  newContextGetItem(issue),
	}
}

func newContextGetOutputLevel1(issue *jira.Issue, children []jira.Issue, parent *contextoutput.Relation) contextoutput.Output {
	kids := contextChildRelations(children)
	return contextoutput.Output{
		Level:    1,
		Item:     newContextGetItem(issue),
		Parent:   parent,
		Children: &kids,
	}
}

func newContextGetOutputLevel3(issue *jira.Issue, children []jira.Issue, walk *jira.DescendantWalkResult, parent *contextoutput.Relation) contextoutput.Output {
	out := newContextGetOutputLevel1(issue, children, parent)
	out.Level = 3
	descendants := contextDescendants(walk.Nodes)
	failures := contextDescendantFailures(walk.Failures)
	out.Descendants = &descendants
	out.DescendantFailures = &failures
	return out
}

func newContextGetOutputLevel2(issue *jira.Issue, children []jira.Issue, ancestors []contextoutput.Relation, parent *contextoutput.Relation) contextoutput.Output {
	kids := contextChildRelations(children)
	return contextoutput.Output{
		Level:     2,
		Item:      newContextGetItem(issue),
		Parent:    parent,
		Children:  &kids,
		Ancestors: &ancestors,
	}
}

type contextAncestorLimitError struct {
	message string
}

func (e *contextAncestorLimitError) Error() string { return e.message }

func walkContextAncestors(ctx context.Context, client jira.IssueReader, target *jira.Issue, maxNodes, maxOutputBytes int, contribution func(rel contextoutput.Relation, first bool) int) ([]contextoutput.Relation, error) {
	ancestors := []contextoutput.Relation{}
	ancestorBytes := 0
	visited := map[string]bool{target.Key: true}
	current := target

	for current.Fields.Parent != nil {
		if err := ctx.Err(); err != nil {
			return ancestors, err
		}
		parentKey := current.Fields.Parent.Key
		if parentKey == "" {
			return ancestors, nil
		}
		if visited[parentKey] {
			return ancestors, nil
		}
		if len(ancestors) >= maxNodes {
			return ancestors, &contextAncestorLimitError{message: fmt.Sprintf("context ancestor walk for %q: exceeded %d-node ceiling", target.Key, maxNodes)}
		}
		visited[parentKey] = true

		parent, err := client.GetIssueRelation(ctx, parentKey)
		if err != nil {
			return ancestors, err
		}
		rel := contextIssueRelation(parent)
		added := contribution(rel, len(ancestors) == 0)
		if ancestorBytes+added > maxOutputBytes {
			return ancestors, &contextAncestorLimitError{message: fmt.Sprintf("context ancestor walk for %q: exceeded %d-byte rendered ancestor-output budget", target.Key, maxOutputBytes)}
		}
		ancestors = append(ancestors, rel)
		ancestorBytes += added
		current = parent
	}
	return ancestors, nil
}

func ancestorRenderedContribution(format string, rel contextoutput.Relation, first bool) int {
	if format == "json" {

		openingContribution := len(jsonAncestorsArray([]contextoutput.Relation{rel})) - len("[]")
		if first {
			return openingContribution
		}

		return openingContribution - 2
	}
	bullet := fmt.Sprintf("- %s — %s (%s, %s, %s)", rel.Key, rel.Summary, rel.Type, rel.Status, rel.Assignee)
	if first {
		return len("\n\n## Ancestors\n\n") + len(bullet)
	}
	return len("\n") + len(bullet)
}

func jsonAncestorsArray(rels []contextoutput.Relation) string {
	b, _ := json.MarshalIndent(rels, "  ", "  ")
	return string(b)
}

func newContextGetItem(issue *jira.Issue) contextoutput.Item {
	labels := issue.Fields.Labels
	if labels == nil {
		labels = []string{}
	}
	description := adf.PlainText(issue.Fields.Description)
	if strings.TrimSpace(description) == "" {
		description = ""
	}
	var resolved *string
	if issue.Fields.Resolved != nil {
		s := issue.Fields.Resolved.Format(time.RFC3339)
		resolved = &s
	}
	return contextoutput.Item{
		Key:         sanitizeContextField(issue.Key),
		Type:        sanitizeContextField(issue.Fields.IssueType.Name),
		Summary:     sanitizeContextField(issue.Fields.Summary),
		Status:      sanitizeContextField(issue.Fields.Status.Name),
		Labels:      sanitizeContextLabels(labels),
		Description: description,
		Resolved:    resolved,
		Assignee:    assigneeOrUnassigned(sanitizeContextField(issue.Fields.Assignee)),
	}
}

func assigneeOrUnassigned(s string) string {
	if s == "" {
		return "(unassigned)"
	}
	return s
}

func contextParentRelation(parent *jira.ParentRef) *contextoutput.Relation {
	if parent == nil {
		return nil
	}
	return &contextoutput.Relation{
		Key:      sanitizeContextField(parent.Key),
		Type:     sanitizeContextField(parent.Fields.IssueType.Name),
		Summary:  sanitizeContextField(parent.Fields.Summary),
		Status:   sanitizeContextField(parent.Fields.Status.Name),
		Assignee: assigneeOrUnassigned(sanitizeContextField(parent.Fields.Assignee)),
	}
}

func resolveContextParent(ctx context.Context, client jira.IssueReader, parentRef *jira.ParentRef) (*contextoutput.Relation, error) {
	if parentRef == nil {
		return nil, nil
	}
	rel := contextParentRelation(parentRef)
	if parentRef.Fields.Assignee != "" || parentRef.Key == "" {
		return rel, nil
	}
	accurate, err := client.GetIssueRelation(ctx, parentRef.Key)
	if err != nil {
		return nil, err
	}
	rel.Assignee = assigneeOrUnassigned(sanitizeContextField(accurate.Fields.Assignee))
	return rel, nil
}

func contextIssueRelation(issue *jira.Issue) contextoutput.Relation {
	return contextoutput.Relation{
		Key:      sanitizeContextField(issue.Key),
		Type:     sanitizeContextField(issue.Fields.IssueType.Name),
		Summary:  sanitizeContextField(issue.Fields.Summary),
		Status:   sanitizeContextField(issue.Fields.Status.Name),
		Assignee: assigneeOrUnassigned(sanitizeContextField(issue.Fields.Assignee)),
	}
}

func contextChildRelations(children []jira.Issue) []contextoutput.Relation {
	out := make([]contextoutput.Relation, len(children))
	for i, c := range children {
		out[i] = contextoutput.Relation{
			Key:      sanitizeContextField(c.Key),
			Type:     sanitizeContextField(c.Fields.IssueType.Name),
			Summary:  sanitizeContextField(c.Fields.Summary),
			Status:   sanitizeContextField(c.Fields.Status.Name),
			Assignee: assigneeOrUnassigned(sanitizeContextField(c.Fields.Assignee)),
		}
	}
	return out
}

func contextDescendants(nodes []jira.DescendantNode) []contextoutput.Descendant {
	out := make([]contextoutput.Descendant, len(nodes))
	for i, n := range nodes {
		out[i] = contextoutput.Descendant{
			Node: contextoutput.Relation{
				Key:      sanitizeContextField(n.Issue.Key),
				Type:     sanitizeContextField(n.Issue.Fields.IssueType.Name),
				Summary:  sanitizeContextField(n.Issue.Fields.Summary),
				Status:   sanitizeContextField(n.Issue.Fields.Status.Name),
				Assignee: assigneeOrUnassigned(sanitizeContextField(n.Issue.Fields.Assignee)),
			},
			Depth:     n.Depth,
			ParentKey: sanitizeContextField(n.ParentKey),
		}
	}
	return out
}

func contextDescendantFailures(failures []jira.DescendantFailure) []contextoutput.DescendantFailure {
	out := make([]contextoutput.DescendantFailure, len(failures))
	for i, f := range failures {
		out[i] = contextoutput.DescendantFailure{
			Key:       sanitizeContextField(f.Key),
			ParentKey: sanitizeContextField(f.ParentKey),
			Depth:     f.Depth,
			Reason:    renderJiraError(f.Err),
		}
	}
	return out
}

func sanitizeContextField(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x20:
		case r >= 0x80 && r <= 0x9F:
		case r == 0x2028 || r == 0x2029:
		case r >= 0x202A && r <= 0x202E:
		case r >= 0x2066 && r <= 0x2069:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sanitizeContextLabels(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = sanitizeContextField(l)
	}
	return out
}

func renderContextGetJSON(out contextoutput.Output) string {
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b)
}

func renderContextGetMD(out contextoutput.Output) string {
	item := out.Item
	lines := []string{fmt.Sprintf("# %s — %s (%s, %s)", item.Key, item.Summary, item.Type, item.Status)}
	lines = append(lines, "", "Assignee: "+item.Assignee)
	if item.Resolved != nil {
		lines = append(lines, "", "Resolved: "+*item.Resolved)
	}
	if len(item.Labels) > 0 {
		lines = append(lines, "", "Labels: "+strings.Join(item.Labels, ", "))
	}
	if item.Description != "" {
		lines = append(lines, "", "## Description", "", item.Description)
	}
	if out.Parent != nil {
		p := *out.Parent
		lines = append(lines, "", fmt.Sprintf("Parent: %s — %s (%s, %s, %s)", p.Key, p.Summary, p.Type, p.Status, p.Assignee))
	}
	if out.Ancestors != nil && len(*out.Ancestors) > 0 {
		lines = append(lines, "", "## Ancestors", "")
		for _, ancestor := range *out.Ancestors {
			lines = append(lines, fmt.Sprintf("- %s — %s (%s, %s, %s)", ancestor.Key, ancestor.Summary, ancestor.Type, ancestor.Status, ancestor.Assignee))
		}
	}
	if out.Descendants != nil {

		tree := descendantTreeLines(*out.Descendants)
		if len(tree) > 0 {
			lines = append(lines, "", "## Children", "")
			lines = append(lines, tree...)
		}
		if out.DescendantFailures != nil && len(*out.DescendantFailures) > 0 {
			lines = append(lines, "", "## Descendant walk failures", "")
			for _, f := range *out.DescendantFailures {

				if f.ParentKey == "" {
					lines = append(lines, fmt.Sprintf("- %s (depth %d): %s", f.Key, f.Depth, f.Reason))
					continue
				}
				lines = append(lines, fmt.Sprintf("- %s (depth %d, parent %s): %s", f.Key, f.Depth, f.ParentKey, f.Reason))
			}
		}
	} else if out.Children != nil && len(*out.Children) > 0 {
		lines = append(lines, childListLines(*out.Children)...)
	}
	return strings.Join(lines, "\n")
}

func childRows(children []contextoutput.Relation) []string {
	lines := make([]string, 0, len(children))
	for _, c := range children {
		lines = append(lines, fmt.Sprintf("- %s — %s (%s, %s, %s)", c.Key, c.Summary, c.Type, c.Status, c.Assignee))
	}
	return lines
}

func childListLines(children []contextoutput.Relation) []string {
	return append([]string{"", "## Children", ""}, childRows(children)...)
}

func descendantTreeLines(descendants []contextoutput.Descendant) []string {
	nodeKeys := make(map[string]bool, len(descendants))
	for _, d := range descendants {
		nodeKeys[d.Node.Key] = true
	}
	childrenByParent := make(map[string][]contextoutput.Descendant, len(descendants))
	var roots []contextoutput.Descendant
	for _, d := range descendants {
		if nodeKeys[d.ParentKey] {
			childrenByParent[d.ParentKey] = append(childrenByParent[d.ParentKey], d)
		} else {
			roots = append(roots, d)
		}
	}
	var lines []string
	var walk func(d contextoutput.Descendant)
	walk = func(d contextoutput.Descendant) {

		indent := strings.Repeat("  ", max(0, d.Depth-1))
		lines = append(lines, fmt.Sprintf("%s- %s — %s (%s, %s, %s)", indent, d.Node.Key, d.Node.Summary, d.Node.Type, d.Node.Status, d.Node.Assignee))
		for _, c := range childrenByParent[d.Node.Key] {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return lines
}

func parseContextGetOptions(args []string) (string, contextGetOptions, error) {
	positional, flagArgs := splitContextGetArgs(args)
	help := hasHelpToken(args)
	if help {

		flagArgs = []string{"--help"}
	}
	if !help && len(positional) == 0 {
		return "", contextGetOptions{}, errors.New("usage: jirahere context get <id> [--level {0,1,2,3}] [--json|--md] [--profile <name>]")
	}
	key := ""
	if len(positional) > 0 {
		key = positional[0]
	}

	if !help && (key == "" || moveIssueKey(key) != key) {
		return "", contextGetOptions{}, errors.New("jirahere: context get: <id> must be a well-formed Jira issue key")
	}
	if !help && len(positional) != 1 {
		return "", contextGetOptions{}, errors.New("usage: jirahere context get <id> [--level {0,1,2,3}] [--json|--md] [--profile <name>]")
	}

	fs := flag.NewFlagSet("context get", flag.ContinueOnError)
	levelText := fs.String("level", "0", "context level: 0, 1, 2, or 3")
	jsonOutput := fs.Bool("json", false, "render JSON output")
	fs.Bool("md", false, "render Markdown output (default)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<id>"}}); err != nil {
		return "", contextGetOptions{}, err
	}
	prof, err := resolveProfile()
	if err != nil {
		return "", contextGetOptions{}, err
	}

	level, err := strconv.Atoi(*levelText)
	if err != nil || level < 0 || level > 3 {
		return "", contextGetOptions{}, errors.New("jirahere: context get: --level must be one of 0, 1, 2, or 3")
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["json"] && visited["md"] {
		return "", contextGetOptions{}, errors.New("jirahere: context get: --json and --md may not be used together")
	}
	format := "md"
	if *jsonOutput {
		format = "json"
	}
	return key, contextGetOptions{level: level, format: format, profile: prof}, nil
}
