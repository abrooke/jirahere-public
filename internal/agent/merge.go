package agent

import (
	"errors"
	"fmt"
	"strings"
)

var ErrSpecializationFormat = errors.New("specialization is not a valid Claude agent file")

var ErrEmbeddedFormat = errors.New("embedded agent file is not a valid Claude agent file")

const (
	ConstructBlockScalar       = "block scalar"
	ConstructFlowList          = "flow list"
	ConstructSingleQuoted      = "single-quoted value"
	ConstructUnsupportedEscape = "unsupported escape"
	ConstructBlankLine         = "blank line in frontmatter"
	ConstructCommentLine       = "comment line in frontmatter"
	ConstructBOM               = "byte-order mark"
)

type UnsupportedConstructError struct {
	Construct string
	Key       string
	Line      int
}

func (e *UnsupportedConstructError) Error() string {
	msg := "specialization frontmatter uses an unsupported construct: " + e.Construct
	if e.Key != "" {
		msg += fmt.Sprintf(" (key %q, line %d)", e.Key, e.Line)
	} else {
		msg += fmt.Sprintf(" (line %d)", e.Line)
	}
	return msg
}

func (e *UnsupportedConstructError) Is(target error) bool { return target == ErrSpecializationFormat }

type fmEntry struct {
	key, value string
}

type claudeAgent struct {
	entries []fmEntry
	body    string
}

func (a *claudeAgent) get(key string) (string, bool) {
	for _, e := range a.entries {
		if e.key == key {
			return e.value, true
		}
	}
	return "", false
}

func (a *claudeAgent) set(key, value string) {
	for i := range a.entries {
		if a.entries[i].key == key {
			a.entries[i].value = value
			return
		}
	}
	a.entries = append(a.entries, fmEntry{key, value})
}

func MergeClaude(embedded []byte, specs ...[]byte) ([]byte, error) {
	acc, err := parseClaude(embedded)
	if err != nil {
		return nil, ErrEmbeddedFormat
	}
	for _, raw := range specs {
		spec, err := parseClaude(raw)
		if err != nil {
			return nil, err
		}
		if err := foldClaude(acc, spec); err != nil {
			return nil, err
		}
	}
	return renderClaude(acc), nil
}

func foldClaude(acc, spec *claudeAgent) error {
	for _, e := range spec.entries {
		switch e.key {
		case "name", "description":
			if e.value == "" {
				continue
			}
			left, ok := acc.get(e.key)
			sep := "-"
			if e.key == "description" {
				sep = " "
			}
			if ok && left != "" {
				acc.set(e.key, left+sep+e.value)
			} else {
				acc.set(e.key, e.value)
			}
		case "tools":
			left, _ := acc.get("tools")
			merged, err := unionTools(left, e.value)
			if err != nil {
				return err
			}
			acc.set("tools", merged)
		default:
			acc.set(e.key, e.value)
		}
	}
	acc.body += "\n\n" + spec.body
	return nil
}

func unionTools(left, right string) (string, error) {
	var out []string
	seen := map[string]bool{}
	for i, list := range []string{left, right} {
		if i == 0 && list == "" {
			continue
		}
		for _, tool := range strings.Split(list, ",") {
			tool = strings.TrimSpace(tool)
			if tool == "" || strings.ContainsAny(tool, " \t") {
				return "", ErrSpecializationFormat
			}
			if !seen[tool] {
				seen[tool] = true
				out = append(out, tool)
			}
		}
	}
	return strings.Join(out, ", "), nil
}

func parseClaude(raw []byte) (*claudeAgent, error) {
	text := string(raw)
	text, bom := strings.CutPrefix(text, "\ufeff")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		rest, ok = strings.CutPrefix(text, "---\r\n")
	}
	if !ok {
		return nil, ErrSpecializationFormat
	}

	agent := &claudeAgent{}
	closed := false

	var failure error
	if bom {
		failure = &UnsupportedConstructError{Construct: ConstructBOM, Line: 1}
	}
	lineNo := 1
	for rest != "" {
		lineNo++
		line, tail, _ := strings.Cut(rest, "\n")
		rest = tail
		line = strings.TrimSuffix(line, "\r")
		if line == "---" {
			closed = true
			break
		}
		if failure != nil {
			continue
		}
		key, value, ok := parseFrontmatterLine(line)
		if !ok {
			failure = ErrSpecializationFormat
			if c, k := classifyLine(line); c != "" {
				failure = &UnsupportedConstructError{Construct: c, Key: k, Line: lineNo}
			}
			continue
		}
		if _, dup := agent.get(key); dup {
			failure = ErrSpecializationFormat
			continue
		}
		agent.entries = append(agent.entries, fmEntry{key, value})
	}
	if !closed {
		return nil, ErrSpecializationFormat
	}
	if failure != nil {
		return nil, failure
	}
	agent.body = strings.Trim(rest, "\r\n")
	if strings.TrimSpace(agent.body) == "" {
		return nil, ErrSpecializationFormat
	}
	return agent, nil
}

func classifyLine(line string) (construct, key string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ConstructBlankLine, ""
	}
	if trimmed[0] == '#' {
		return ConstructCommentLine, ""
	}
	k, v, found := strings.Cut(line, ":")
	if !found || !validKey(k) || (v != "" && v[0] != ' ') {
		return "", ""
	}
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return "", ""
	case v[0] == '|' || v[0] == '>':
		return ConstructBlockScalar, k
	case v[0] == '[':
		return ConstructFlowList, k
	case v[0] == '\'':
		return ConstructSingleQuoted, k
	case v[0] == '"' && hasUnsupportedEscape(v):
		return ConstructUnsupportedEscape, k
	}
	return "", ""
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func hasUnsupportedEscape(v string) bool {
	for i := 1; i < len(v); i++ {
		if v[i] != '\\' {
			continue
		}
		i++
		if i >= len(v) || (v[i] != '"' && v[i] != '\\') {
			return true
		}
	}
	return false
}

func parseFrontmatterLine(line string) (key, value string, ok bool) {
	key, value, found := strings.Cut(line, ":")
	if !found || key == "" {
		return "", "", false
	}
	if !validKey(key) {
		return "", "", false
	}
	if value != "" && value[0] != ' ' {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return key, "", true
	}
	if value[0] == '"' {
		unquoted, ok := unquote(value)
		return key, unquoted, ok
	}
	if strings.ContainsAny(value[:1], "[]{}|>'&*!%@`#,") || strings.Contains(value, ": ") || strings.Contains(value, " #") || strings.HasSuffix(value, ":") {
		return "", "", false
	}
	return key, value, true
}

func unquote(s string) (string, bool) {
	if len(s) < 2 || s[len(s)-1] != '"' {
		return "", false
	}
	var b strings.Builder
	inner := s[1 : len(s)-1]
	for i := 0; i < len(inner); i++ {
		switch c := inner[i]; c {
		case '"':
			return "", false
		case '\\':
			i++
			if i >= len(inner) || (inner[i] != '"' && inner[i] != '\\') {
				return "", false
			}
			b.WriteByte(inner[i])
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), true
}

func renderClaude(a *claudeAgent) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	for _, e := range a.entries {
		b.WriteString(e.key + ": " + renderValue(e.key, e.value) + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(a.body)
	b.WriteString("\n")
	return []byte(b.String())
}

func renderValue(key, v string) string {
	if key != "description" && v != "" && plainSafe(v) {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

func plainSafe(v string) bool {
	if strings.ContainsAny(v[:1], "[]{}|>'\"&*!%@`#,- ") || v[len(v)-1] == ' ' || v[len(v)-1] == ':' {
		return false
	}
	return !strings.Contains(v, ": ") && !strings.Contains(v, " #") && !strings.ContainsAny(v, "\r\n\t\\\"")
}
