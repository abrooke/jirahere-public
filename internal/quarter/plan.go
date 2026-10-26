package quarter

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const planMarker = "_Plan"

func FindPlanEpic(items []QuarterIssue, quarter string) (QuarterIssue, bool, error) {
	if strings.TrimSpace(quarter) == "" {
		return QuarterIssue{}, false, errors.New("cannot find the plan epic: the quarter label is empty")
	}
	token := planMarker + " " + quarter
	var matches []QuarterIssue
	for _, it := range items {
		if containsToken(it.Summary, token) {
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
		"ambiguous plan epic: %d work items have %q in their summary (%s); keep exactly one",
		len(matches), token, strings.Join(keys, ", "),
	)
}

func containsToken(s, token string) bool {
	for off := 0; ; {
		i := strings.Index(s[off:], token)
		if i < 0 {
			return false
		}
		end := off + i + len(token)
		if end == len(s) {
			return true
		}
		r, _ := utf8.DecodeRuneInString(s[end:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
		off += i + 1
	}
}
