package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/plan"
	"github.com/aslanbrooke/jirahere/internal/quarter"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

const planContextUsage = "usage: jirahere plan context (--day|--week|--month) [--from YYYY-MM-DD] [--json|--md] [--refresh] [--profile <name>]"

type planContextArgs struct {
	kind    plan.Kind
	from    time.Time
	format  string
	refresh bool
	prof    string
}

func runPlanContext(args []string) error {
	pa, err := parsePlanContextArgs(args)
	if err != nil {
		return err
	}

	prof := pa.prof
	s, err := settings.Load(prof)
	if err != nil {
		return failf("jirahere: could not load settings: %s.", err)
	}
	provider, err := auth.LoadProvider(prof)
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(prof))
		}
		return failf("jirahere: could not load Jira credentials. Run \"%s\" again.", loginRemediation(prof))
	}
	ctx := context.Background()
	creds, err := provider.Credentials(ctx)
	if err != nil {
		return refreshCredentialsFailureFor(err, prof)
	}

	window := plan.Window(pa.kind, pa.from)
	facts, err := plan.Gather(ctx, newPlanClient(creds, prof, pa.refresh), window, s.Defaults.CurrentQuarter)
	if err != nil {
		var unset *quarter.ErrSettingUnset
		if errors.As(err, &unset) {
			return quarterSettingUnsetFailure(unset, prof)
		}
		return failf("jirahere: plan context: %s.", renderJiraError(err))
	}
	order, circuitErr := plan.Circuit(facts.Items)
	bundle := newPlanContextBundle(pa.kind, window, facts, order, circuitErr)
	if pa.format == "json" {
		if _, err := fmt.Fprintln(os.Stdout, renderPlanContextJSON(bundle)); err != nil {
			return failf("jirahere: plan context: could not write output: %s.", err)
		}
		return nil
	}
	if _, err := fmt.Fprintln(os.Stdout, renderPlanContextMD(bundle)); err != nil {
		return failf("jirahere: plan context: could not write output: %s.", err)
	}
	return nil
}

type planClient struct {
	*jira.Client
	ctxInventory func(ctx context.Context) ([]quarter.QuarterIssue, error)
}

func (c planClient) Inventory(ctx context.Context) ([]quarter.QuarterIssue, error) {
	return c.ctxInventory(ctx)
}

var newPlanClient = func(creds auth.Credentials, prof string, refresh bool) plan.Client {
	return planClient{
		Client: jira.NewClient(creds.BaseURL, creds.AuthHeader),
		ctxInventory: func(ctx context.Context) ([]quarter.QuarterIssue, error) {
			if refresh {
				return quarter.RefreshInventory(ctx, newQuarterSearcher(creds), prof)
			}
			return quarter.Inventory(ctx, newQuarterSearcher(creds), prof)
		},
	}
}

const bundleDateLayout = "2006-01-02"

type planContextBundle struct {
	Window   bundleWindow          `json:"window"`
	Vision   *bundleVision         `json:"vision"`
	PlanEpic *string               `json:"planEpic"`
	Items    []bundleItem          `json:"items"`
	LastDone map[string]bundleDone `json:"lastDone"`
	Circuit  bundleCircuit         `json:"circuit"`
}

type bundleWindow struct {
	Kind  string `json:"kind"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type bundleVision struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

type bundleItem struct {
	Key      string  `json:"key"`
	Summary  string  `json:"summary"`
	Status   string  `json:"status"`
	Created  string  `json:"created"`
	Updated  string  `json:"updated"`
	Resolved *string `json:"resolved"`
	Assignee string  `json:"assignee"`
	Parent   *string `json:"parent"`
	AgeDays  int     `json:"ageDays"`
}

type bundleDone struct {
	Key      string `json:"key"`
	Resolved string `json:"resolved"`
}

type bundleZone struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type bundleCircuit struct {
	Zones  []bundleZone `json:"zones,omitempty"`
	Closed bool         `json:"closed"`
	Last   *string      `json:"last"`
	Next   *string      `json:"next"`
	Error  *string      `json:"error,omitempty"`
}

func (c bundleCircuit) MarshalJSON() ([]byte, error) {
	if c.Error != nil {
		return json.Marshal(struct {
			Error string `json:"error"`
		}{*c.Error})
	}
	zones := c.Zones
	if zones == nil {
		zones = []bundleZone{}
	}
	return json.Marshal(struct {
		Zones  []bundleZone `json:"zones"`
		Closed bool         `json:"closed"`
		Last   *string      `json:"last"`
		Next   *string      `json:"next"`
	}{zones, c.Closed, c.Last, c.Next})
}

func newPlanContextBundle(kind plan.Kind, w plan.Range, f plan.Facts, order plan.Order, circuitErr error) planContextBundle {
	b := planContextBundle{
		Window:   bundleWindow{Kind: string(kind), Start: w.Start.Format(bundleDateLayout), End: w.End.Format(bundleDateLayout)},
		Items:    make([]bundleItem, 0, len(f.Items)),
		LastDone: map[string]bundleDone{},
	}
	if f.Vision != nil {
		b.Vision = &bundleVision{Key: f.Vision.Key, Summary: f.Vision.Summary, Description: f.Vision.Description}
	}
	if f.PlanEpic != nil {
		k := f.PlanEpic.Key
		b.PlanEpic = &k
	}
	for _, it := range f.Items {
		bi := bundleItem{
			Key: it.Key, Summary: it.Summary, Status: it.Status,
			Created:  it.Created.Format(time.RFC3339),
			Updated:  it.Updated.Format(time.RFC3339),
			Assignee: it.Assignee, AgeDays: it.AgeDays,
		}
		if it.Resolved != nil {
			r := it.Resolved.Format(time.RFC3339)
			bi.Resolved = &r
		}
		if it.ParentKey != "" {
			p := it.ParentKey
			bi.Parent = &p
		}
		b.Items = append(b.Items, bi)
		if it.LastDone != nil {
			b.LastDone[it.Key] = bundleDone{Key: it.LastDone.Key, Resolved: it.LastDone.Resolved.Format(bundleDateLayout)}
		}
	}
	if circuitErr != nil {
		msg := circuitErr.Error()
		b.Circuit = bundleCircuit{Error: &msg}
		return b
	}
	b.Circuit = newBundleCircuit(order, f.Items)
	return b
}

func newBundleCircuit(order plan.Order, items []plan.Item) bundleCircuit {
	c := bundleCircuit{Zones: make([]bundleZone, len(order.Zones)), Closed: order.Closed}
	byKey := map[string]plan.Item{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	lastIdx := -1
	var lastAt time.Time
	for i, z := range order.Zones {
		c.Zones[i] = bundleZone{Name: z.Name, Key: z.Key}
		if d := byKey[z.Key].LastDone; d != nil && (lastIdx < 0 || d.Resolved.After(lastAt)) {
			lastIdx, lastAt = i, d.Resolved
		}
	}
	if len(order.Zones) == 0 {
		return c
	}
	nextIdx := 0
	if lastIdx >= 0 {
		n := order.Zones[lastIdx].Name
		c.Last = &n
		nextIdx = lastIdx + 1
		if nextIdx == len(order.Zones) {
			if !order.Closed {
				return c
			}
			nextIdx = 0
		}
	}
	n := order.Zones[nextIdx].Name
	c.Next = &n
	return c
}

func renderPlanContextJSON(b planContextBundle) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(b)
	return strings.TrimSuffix(buf.String(), "\n")
}

func quoteDescription(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight("> "+sanitizeContextField(ln), " ")
	}
	return strings.Join(lines, "\n")
}

func cell(s string) string {
	s = strings.ReplaceAll(sanitizeContextField(s), "\\", "\\\\")
	return strings.ReplaceAll(s, "|", "\\|")
}

func orDash(p *string) string {
	if p == nil {
		return "-"
	}
	if *p == "" {
		return "-"
	}
	return cell(*p)
}

func renderPlanContextMD(b planContextBundle) string {
	l := []string{"# Plan context", "", "## Window", "",
		fmt.Sprintf("%s: %s to %s", b.Window.Kind, b.Window.Start, b.Window.End), "", "## Vision", ""}
	if b.Vision == nil {
		l = append(l, "(none)")
	} else {
		l = append(l, fmt.Sprintf("%s — %s", b.Vision.Key, sanitizeContextField(b.Vision.Summary)))
		if b.Vision.Description != "" {
			l = append(l, "", quoteDescription(b.Vision.Description))
		}
	}
	l = append(l, "", "## Plan epic", "")
	if b.PlanEpic == nil {
		l = append(l, "(none)")
	} else {
		l = append(l, *b.PlanEpic)
	}
	l = append(l, "", "## Items", "")
	if len(b.Items) == 0 {
		l = append(l, "(none)")
	} else {
		l = append(l, "| Key | Summary | Status | Created | Updated | Resolved | Assignee | Parent | Age (days) |", "|---|---|---|---|---|---|---|---|---|")
		for _, it := range b.Items {
			l = append(l, fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s | %d |",
				it.Key, cell(it.Summary), cell(it.Status), it.Created, it.Updated, orDash(it.Resolved), orDash(&it.Assignee), orDash(it.Parent), it.AgeDays))
		}
	}
	l = append(l, "", "## Last done", "")
	if len(b.LastDone) == 0 {
		l = append(l, "(none)")
	} else {
		l = append(l, "| Parent | Last done | Resolved |", "|---|---|---|")
		for _, it := range b.Items {
			if d, ok := b.LastDone[it.Key]; ok {
				l = append(l, fmt.Sprintf("| %s | %s | %s |", it.Key, d.Key, d.Resolved))
			}
		}
	}
	l = append(l, "", "## Circuit", "")
	c := b.Circuit
	if c.Error != nil {
		l = append(l, "Error: "+sanitizeContextField(*c.Error))
		return strings.Join(l, "\n")
	}
	if len(c.Zones) == 0 {
		l = append(l, "(none)")
		return strings.Join(l, "\n")
	}
	l = append(l, "| # | Zone | Item |", "|---|---|---|")
	for i, z := range c.Zones {
		l = append(l, fmt.Sprintf("| %d | %s | %s |", i+1, cell(z.Name), z.Key))
	}
	l = append(l, "", fmt.Sprintf("Closed: %t", c.Closed), "Last: "+orDash(c.Last), "Next: "+orDash(c.Next))
	return strings.Join(l, "\n")
}

func parsePlanContextArgs(args []string) (planContextArgs, error) {
	if hasHelpToken(args) {
		args = []string{"--help"}
	}
	fs := flag.NewFlagSet("plan context", flag.ContinueOnError)
	day := fs.Bool("day", false, "window of one day")
	week := fs.Bool("week", false, "window of one week (7 days)")
	month := fs.Bool("month", false, "window of one month (30 days)")
	from := fs.String("from", "", "window start date, YYYY-MM-DD (default today)")
	jsonOutput := fs.Bool("json", false, "render JSON output")
	fs.Bool("md", false, "render Markdown output (default)")
	refresh := fs.Bool("refresh", false, "re-fetch the quarter inventory from Jira and replace the cached copy")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return planContextArgs{}, err
	}
	if fs.NArg() != 0 {
		return planContextArgs{}, usagef("jirahere: plan context: unexpected argument %q. %s", fs.Arg(0), planContextUsage)
	}

	prof, err := resolveProfile()
	if err != nil {
		return planContextArgs{}, err
	}

	var kind plan.Kind
	n := 0
	for _, w := range []struct {
		set  bool
		kind plan.Kind
	}{{*day, plan.Day}, {*week, plan.Week}, {*month, plan.Month}} {
		if w.set {
			kind = w.kind
			n++
		}
	}
	if n != 1 {
		return planContextArgs{}, usagef("jirahere: plan context: exactly one of --day, --week or --month is required. %s", planContextUsage)
	}

	start := time.Now()
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["from"] {
		t, err := plan.ParseFrom(*from)
		if err != nil {
			return planContextArgs{}, usagef("jirahere: plan context: --from: %s.", err)
		}
		start = t
	}
	if visited["json"] && visited["md"] {
		return planContextArgs{}, usagef("jirahere: plan context: --json and --md may not be used together.")
	}
	format := "md"
	if *jsonOutput {
		format = "json"
	}
	return planContextArgs{kind: kind, from: start, format: format, refresh: *refresh, prof: prof}, nil
}
