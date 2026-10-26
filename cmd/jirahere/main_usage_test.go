package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const usageRowDescOffset = 19

func usageRowDescriptions(t *testing.T, out string) map[string]string {
	t.Helper()
	rows := map[string]string{}
	inCommands := false
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "commands:" {
			inCommands = true
			continue
		}
		if !inCommands || line == "" {
			continue
		}
		if len(line) <= usageRowDescOffset || !strings.HasPrefix(line, "  ") ||
			line[usageRowDescOffset-2:usageRowDescOffset] != "  " ||
			line[usageRowDescOffset] == ' ' {
			t.Fatalf("usage row does not follow the indent/name-column/gap layout: %q", line)
		}
		name := strings.TrimRight(line[2:usageRowDescOffset-2], " ")
		rows[name] = line[usageRowDescOffset:]
	}
	return rows
}

func overLengthRows(rows map[string]string, limit int) []string {
	var bad []string
	for name, desc := range rows {
		if n := utf8.RuneCountInString(desc); n > limit {
			bad = append(bad, fmt.Sprintf("%s: %d runes", name, n))
		}
	}
	sort.Strings(bad)
	return bad
}

func TestUsage_DescriptionsWithinLimit(t *testing.T) {
	var buf bytes.Buffer
	if err := usage(&buf); err != nil {
		t.Fatalf("usage: %v", err)
	}
	rows := usageRowDescriptions(t, buf.String())
	if len(rows) == 0 {
		t.Fatal("parsed no command rows from usage() output")
	}
	for _, bad := range overLengthRows(rows, maxListDescriptionRunes) {
		t.Errorf("usage() row description exceeds %d runes (%s)", maxListDescriptionRunes, bad)
	}
}

func TestUsage_LimitCheckFailsOnOverLengthRow(t *testing.T) {
	row := func(desc string) map[string]string {
		return usageRowDescriptions(t, "commands:\n  "+fmt.Sprintf("%-15s", "demo")+"  "+desc+"\n")
	}
	at := strings.Repeat("x", maxListDescriptionRunes)
	if got := overLengthRows(row(at), maxListDescriptionRunes); len(got) != 0 {
		t.Errorf("60-rune description flagged: %v", got)
	}
	if got := overLengthRows(row(at+"x"), maxListDescriptionRunes); len(got) != 1 {
		t.Errorf("61-rune description not flagged exactly once: %v", got)
	}
	multibyte := strings.Repeat("é", maxListDescriptionRunes)
	if got := overLengthRows(row(multibyte), maxListDescriptionRunes); len(got) != 0 {
		t.Errorf("60-rune multibyte description flagged (counting bytes?): %v", got)
	}
}

func TestRootHelp_ListsDescriptionSet(t *testing.T) {
	stdout, stderr, code := runJirahereProcess(t, "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("--help: exit %d, stderr %q; want 0 and empty", code, stderr)
	}
	rows := usageRowDescriptions(t, stdout)
	desc, ok := rows["description set"]
	if !ok || desc == "" {
		t.Fatalf("root help does not list a `description set` row:\n%s", stdout)
	}
	if n := utf8.RuneCountInString(desc); n > maxListDescriptionRunes {
		t.Errorf("description set row is %d runes, cap %d", n, maxListDescriptionRunes)
	}
	lines := strings.Split(stdout, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "  description get ") {
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "  description set ") {
				t.Errorf("`description set` row does not directly follow `description get`")
			}
			return
		}
	}
	t.Error("root help lacks the `description get` row")
}
