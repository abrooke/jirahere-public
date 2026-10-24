package plan

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const linkSep = " :: "

var quarterToken = regexp.MustCompile(` Q[1-4]'\d\d$`)

type Zone struct {
	Name string
	Key  string
}

type Link struct {
	Name    string
	Next    string
	Key     string
	NextKey string
}

type Order struct {
	Zones  []Zone
	Closed bool
	Links  []Link
}

func Circuit(items []Item) (Order, error) {
	var links []linkItem
	byName := map[string][]linkItem{}
	for _, it := range items {
		l, ok := parseLink(it)
		if !ok {
			continue
		}
		links = append(links, l)
		byName[l.name] = append(byName[l.name], l)
	}
	if len(links) == 0 {
		return Order{}, nil
	}

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		dup := byName[n]
		if len(dup) < 2 {
			continue
		}
		keys := keysOf(dup)
		if differentNext(dup) {
			return Order{}, fmt.Errorf("zone %q has more than one outgoing link (%s)", n, strings.Join(keys, ", "))
		}
		return Order{}, fmt.Errorf("zone %q is named by more than one work item (%s)", n, strings.Join(keys, ", "))
	}

	plain := map[string][]string{}
	for _, it := range items {
		if _, ok := parseLink(it); ok {
			continue
		}
		if n := zoneName(it.Summary); n != "" {
			plain[n] = append(plain[n], it.Key)
		}
	}
	nextKey := map[string]string{}
	tails := map[string]string{}
	sorted := append([]linkItem(nil), links...)
	sort.Slice(sorted, func(i, j int) bool { return keyLess(sorted[i].key, sorted[j].key) })
	for _, l := range sorted {
		if t, ok := byName[l.next]; ok {
			nextKey[l.key] = t[0].key
			continue
		}
		cands := plain[l.next]
		switch len(cands) {
		case 0:
			return Order{}, fmt.Errorf("work item %s links to %q, which names no work item", l.key, l.next)
		case 1:
			nextKey[l.key] = cands[0]
			tails[l.next] = cands[0]
		default:
			sort.Slice(cands, func(i, j int) bool { return keyLess(cands[i], cands[j]) })
			return Order{}, fmt.Errorf("work item %s links to %q, which is named by more than one work item (%s)", l.key, l.next, strings.Join(cands, ", "))
		}
	}

	into := map[string][]string{}
	for _, l := range sorted {
		into[l.next] = append(into[l.next], l.key)
	}
	for _, l := range sorted {
		if k := into[l.next]; len(k) > 1 {
			return Order{}, fmt.Errorf("work items %s all link to %q; a circuit has one predecessor per zone", strings.Join(k, ", "), l.next)
		}
	}

	var heads []linkItem
	for _, l := range sorted {
		if len(into[l.name]) == 0 {
			heads = append(heads, l)
		}
	}
	if len(heads) > 1 {
		return Order{}, fmt.Errorf("multiple disjoint circuits: chains start at %s", strings.Join(keysOf(heads), ", "))
	}
	start := sorted[0]
	if len(heads) == 1 {
		start = heads[0]
	}

	var out Order
	seen := map[string]bool{}
	cur := start
	for {
		seen[cur.key] = true
		out.Zones = append(out.Zones, Zone{Name: cur.name, Key: cur.key})
		out.Links = append(out.Links, Link{Name: cur.name, Next: cur.next, Key: cur.key, NextKey: nextKey[cur.key]})
		nxt, ok := byName[cur.next]
		if !ok {
			out.Zones = append(out.Zones, Zone{Name: cur.next, Key: tails[cur.next]})
			break
		}
		if nxt[0].key == start.key {
			out.Closed = true
			break
		}
		cur = nxt[0]
	}

	if len(seen) < len(links) {
		var rest []string
		for _, l := range sorted {
			if !seen[l.key] {
				rest = append(rest, l.key)
			}
		}
		return Order{}, fmt.Errorf("multiple disjoint circuits: %s are not connected to %s", strings.Join(rest, ", "), strings.Join(keysOf(seenItems(sorted, seen)), ", "))
	}
	return out, nil
}

func differentNext(ls []linkItem) bool {
	for _, l := range ls[1:] {
		if l.next != ls[0].next {
			return true
		}
	}
	return false
}

func seenItems(ls []linkItem, seen map[string]bool) []linkItem {
	var out []linkItem
	for _, l := range ls {
		if seen[l.key] {
			out = append(out, l)
		}
	}
	return out
}

func keysOf(ls []linkItem) []string {
	keys := make([]string, len(ls))
	for i, l := range ls {
		keys[i] = l.key
	}
	sort.Slice(keys, func(i, j int) bool { return keyLess(keys[i], keys[j]) })
	return keys
}

type linkItem struct {
	key, name, next string
}

func parseLink(it Item) (linkItem, bool) {
	if strings.Count(it.Summary, linkSep) != 1 {
		return linkItem{}, false
	}
	left, right, _ := strings.Cut(it.Summary, linkSep)
	l := linkItem{
		key:  it.Key,
		name: leftName(left),
		next: strings.TrimSpace(quarterToken.ReplaceAllString(right, "")),
	}
	if l.name == "" || l.next == "" {
		return linkItem{}, false
	}
	return l, true
}

func zoneName(summary string) string {
	if strings.Contains(summary, linkSep) {
		return ""
	}
	return leftName(quarterToken.ReplaceAllString(summary, ""))
}

func leftName(left string) string {
	if i := strings.LastIndex(left, ": "); i >= 0 {
		left = left[i+2:]
	}
	return strings.TrimSpace(left)
}
