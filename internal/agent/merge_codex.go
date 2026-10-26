package agent

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

var ErrCodexSpecializationFormat = errors.New("specialization is not a valid Codex agent config")

var ErrCodexSpecializationSecurity = errors.New("specialization has a security setting that cannot be merged")

var ErrCodexEmbeddedFormat = errors.New("embedded agent file is not a valid Codex agent config")

var codexSecurityOrderings = map[string][]string{
	"sandbox_mode":    {"read-only", "workspace-write", "danger-full-access"},
	"approval_policy": {"on-request", "never"},
	"web_search":      {"disabled", "cached", "indexed", "live"},
}

func MergeCodex(embedded []byte, specs ...[]byte) ([]byte, error) {
	acc := map[string]any{}
	if _, err := toml.Decode(string(embedded), &acc); err != nil {
		return nil, ErrCodexEmbeddedFormat
	}
	if err := checkCodexAgent(acc); err != nil {
		return nil, ErrCodexEmbeddedFormat
	}
	for _, raw := range specs {
		spec := map[string]any{}
		if _, err := toml.Decode(string(raw), &spec); err != nil {
			return nil, ErrCodexSpecializationFormat
		}
		if err := checkCodexAgent(spec); err != nil {
			return nil, err
		}
		if err := foldCodex(acc, spec); err != nil {
			return nil, err
		}
	}
	out, err := renderCodexFn(embedded, acc)
	if err != nil {
		if len(specs) > 0 {

			if _, embErr := renderCodexFn(embedded, embeddedOnly(embedded)); embErr == nil {
				return nil, ErrCodexSpecializationFormat
			}
		}
		return nil, ErrCodexEmbeddedFormat
	}
	return out, nil
}

var renderCodexFn = renderCodex

func embeddedOnly(embedded []byte) map[string]any {
	m := map[string]any{}
	_, _ = toml.Decode(string(embedded), &m)
	return m
}

func checkCodexAgent(m map[string]any) error {
	di, ok := m["developer_instructions"].(string)
	if !ok || strings.TrimSpace(di) == "" {
		return ErrCodexSpecializationFormat
	}
	if d, ok := m["description"]; ok {
		if _, isStr := d.(string); !isStr {
			return ErrCodexSpecializationFormat
		}
	}
	return checkCodexSecurity("", m)
}

func codexExemptKey(parent, key string) bool {
	return key == "web_search" && (parent == "tools" || parent == "features")
}

func checkCodexSecurity(parent string, v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if _, ok := codexSecurityOrderings[k]; ok && !codexExemptKey(parent, k) {
				if _, err := codexRank(k, child); err != nil {
					return err
				}
			}
			if err := checkCodexSecurity(k, child); err != nil {
				return err
			}
		}
	case []map[string]any:
		for _, child := range t {
			if err := checkCodexSecurity("", child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range t {
			if err := checkCodexSecurity("", child); err != nil {
				return err
			}
		}
	}
	return nil
}

func codexRank(key string, v any) (int, error) {
	s, ok := v.(string)
	if !ok {
		return 0, ErrCodexSpecializationSecurity
	}
	for i, allowed := range codexSecurityOrderings[key] {
		if s == allowed {
			return i, nil
		}
	}
	return 0, ErrCodexSpecializationSecurity
}

func foldCodex(acc, spec map[string]any) error {
	for key, sv := range spec {
		switch key {
		case "developer_instructions":
			left, _ := acc[key].(string)
			acc[key] = strings.TrimRight(left, "\r\n") + "\n\n" + strings.TrimLeft(sv.(string), "\r\n")
		case "description":
			if sv.(string) == "" {
				continue
			}
			if left, _ := acc[key].(string); left != "" {
				acc[key] = left + " " + sv.(string)
			} else {
				acc[key] = sv
			}
		default:
			acc[key] = mergeCodexValue("", key, acc[key], sv)
		}
	}
	return nil
}

func mergeCodexValue(parent, key string, left, right any) any {
	if _, ok := codexSecurityOrderings[key]; ok && !codexExemptKey(parent, key) && left != nil {
		lr, lerr := codexRank(key, left)
		rr, rerr := codexRank(key, right)
		if lerr == nil && rerr == nil && rr <= lr {
			return left
		}
	}
	lt, lok := left.(map[string]any)
	rt, rok := right.(map[string]any)
	if !lok || !rok {
		return right
	}
	for k, v := range rt {
		lt[k] = mergeCodexValue(key, k, lt[k], v)
	}
	return lt
}

func renderCodex(embedded []byte, m map[string]any) ([]byte, error) {
	var b bytes.Buffer
	for _, line := range strings.SplitAfter(string(embedded), "\n") {
		if !strings.HasPrefix(line, "#") {
			break
		}
		b.WriteString(line)
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	if di, _ := m["developer_instructions"].(string); literalSafe(di) {
		fmt.Fprintf(&b, "developer_instructions = '''\n%s\n'''\n\n", strings.TrimRight(di, "\n"))
		rest := make(map[string]any, len(m))
		for k, v := range m {
			if k != "developer_instructions" {
				rest[k] = v
			}
		}
		m = rest
	}
	if err := toml.NewEncoder(&b).Encode(m); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func literalSafe(s string) bool {
	if strings.Contains(s, "'''") || strings.HasSuffix(s, "'") {
		return false
	}
	for _, r := range s {
		if r == '\t' || r == '\n' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
