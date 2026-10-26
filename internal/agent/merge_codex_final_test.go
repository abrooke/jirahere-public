package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestMergeCodex_ExemptWebSearchPathsCarried(t *testing.T) {
	for name, lines := range map[string][]string{
		"tools bool":      {`[tools]`, `web_search = true`},
		"tools table":     {`[tools.web_search]`, `context_size = "high"`},
		"features bool":   {`[features]`, `web_search = true`},
		"profile tools":   {`[profiles.p.tools]`, `web_search = true`},
		"profile feature": {`[profiles.p.features]`, `web_search = true`},
	} {
		spec := codexSpec(append([]string{`developer_instructions = "X"`}, lines...)...)
		m := mergeCodexMap(t, spec)
		if m["tools"] == nil && m["features"] == nil && m["profiles"] == nil {
			t.Errorf("%s: not carried: %v", name, m)
		}
	}
	m := mergeCodexMap(t, codexSpec(`developer_instructions = "X"`, `[tools]`, `web_search = true`))
	if m["tools"].(map[string]any)["web_search"] != true {
		t.Errorf("tools.web_search = %v", m["tools"])
	}
	m = mergeCodexMap(t, codexSpec(`developer_instructions = "X"`, `[features]`, `web_search = true`))
	if m["features"].(map[string]any)["web_search"] != true {
		t.Errorf("features.web_search = %v", m["features"])
	}
}

func TestMergeCodex_ExemptWebSearchLaterWins(t *testing.T) {
	emb := embeddedWith(t, `[tools]`, `web_search = true`)
	out := mustMerge(t, emb, codexSpec(`developer_instructions = "X"`, `[tools]`, `web_search = false`))
	if decodeCodex(t, out)["tools"].(map[string]any)["web_search"] != false {
		t.Errorf("later side did not win:\n%s", out)
	}
}

func TestMergeCodex_WebSearchElsewhereStillRejected(t *testing.T) {
	for name, lines := range map[string][]string{
		"top level":     {`web_search = true`},
		"other table":   {`[other]`, `web_search = true`},
		"profile":       {`[profiles.p]`, `web_search = true`},
		"nested tools":  {`[tools.sub]`, `web_search = true`},
		"array element": {`[[tools]]`, `web_search = true`},
	} {
		spec := codexSpec(append([]string{`developer_instructions = "SECRET-BODY"`}, lines...)...)
		out, err := MergeCodex(codexEmbedded(t), spec)
		if !errors.Is(err, ErrCodexSpecializationSecurity) || out != nil || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: err=%v out=%q", name, err, out)
		}
	}
}

func TestMergeCodex_ArrayReplacedWholesale(t *testing.T) {
	emb := embeddedWith(t, `list = [1, 2, 3]`, `[[hooks]]`, `name = "a"`, `[[hooks]]`, `name = "b"`)
	m := decodeCodex(t, mustMerge(t, emb, codexSpec(`developer_instructions = "X"`, `list = [9]`, `[[hooks]]`, `name = "c"`)))
	if l := m["list"].([]any); len(l) != 1 || l[0] != int64(9) {
		t.Errorf("list = %v", l)
	}
	if h := m["hooks"].([]map[string]any); len(h) != 1 || h[0]["name"] != "c" {
		t.Errorf("hooks = %v", h)
	}
}

func TestMergeCodex_SecurityKeyInArrayOfTablesValidateOnly(t *testing.T) {
	emb := embeddedWith(t, `[[hooks]]`, `sandbox_mode = "danger-full-access"`)
	m := decodeCodex(t, mustMerge(t, emb, codexSpec(`developer_instructions = "X"`, `[[hooks]]`, `sandbox_mode = "read-only"`)))
	if h := m["hooks"].([]map[string]any); len(h) != 1 || h[0]["sandbox_mode"] != "read-only" {
		t.Errorf("hooks = %v", h)
	}
	spec := codexSpec(`developer_instructions = "SECRET-BODY"`, `[[hooks]]`, `sandbox_mode = "yolo"`)
	if out, err := MergeCodex(emb, spec); !errors.Is(err, ErrCodexSpecializationSecurity) || out != nil {
		t.Errorf("err=%v out=%q", err, out)
	}
}

func TestMergeCodex_TypeMismatchLaterWins(t *testing.T) {
	emb := embeddedWith(t, `[profiles.p]`, `sandbox_mode = "danger-full-access"`, `[arr]`, `k = 1`)
	m := decodeCodex(t, mustMerge(t, emb, codexSpec(`developer_instructions = "X"`, `profiles = "none"`, `arr = [1, 2]`)))
	if m["profiles"] != "none" {
		t.Errorf("profiles = %v", m["profiles"])
	}
	if l, ok := m["arr"].([]any); !ok || len(l) != 2 {
		t.Errorf("arr = %v", m["arr"])
	}
	emb2 := embeddedWith(t, `profiles = "x"`)
	m = decodeCodex(t, mustMerge(t, emb2, nestedSpec(`sandbox_mode = "read-only"`)))
	if profileP(m)["sandbox_mode"] != "read-only" {
		t.Errorf("profiles = %v", m["profiles"])
	}
	if out, err := MergeCodex(emb2, nestedSpec(`sandbox_mode = "yolo"`)); !errors.Is(err, ErrCodexSpecializationSecurity) || out != nil {
		t.Errorf("err=%v out=%q", err, out)
	}
}

func TestMergeCodex_SecurityKeySyntaxVariants(t *testing.T) {
	good := map[string][]string{
		"inline":     {`profiles = { p = { sandbox_mode = "danger-full-access" } }`},
		"dotted":     {`profiles.p.sandbox_mode = "danger-full-access"`},
		"quoted":     {`[profiles.p]`, `"sandbox_mode" = "danger-full-access"`},
		"quoted tbl": {`[profiles."p"]`, `'sandbox_mode' = "danger-full-access"`},
	}
	for name, lines := range good {
		m := decodeCodex(t, mustMerge(t, embeddedWith(t, `[profiles.p]`, `sandbox_mode = "read-only"`),
			codexSpec(append([]string{`developer_instructions = "X"`}, lines...)...)))
		if profileP(m)["sandbox_mode"] != "danger-full-access" {
			t.Errorf("%s: %v", name, m["profiles"])
		}
	}
	bad := map[string][]string{
		"inline": {`profiles = { p = { sandbox_mode = "yolo" } }`},
		"dotted": {`profiles.p.approval_policy = "untrusted"`},
		"quoted": {`[profiles.p]`, `"web_search" = "everything"`},
	}
	for name, lines := range bad {
		out, err := MergeCodex(codexEmbedded(t), codexSpec(append([]string{`developer_instructions = "X"`}, lines...)...))
		if !errors.Is(err, ErrCodexSpecializationSecurity) || out != nil {
			t.Errorf("%s: err=%v out=%q", name, err, out)
		}
	}
}

func TestMergeCodex_RenderFailureFromSpecIsSpecFormatError(t *testing.T) {
	orig := renderCodexFn
	defer func() { renderCodexFn = orig }()

	renderCodexFn = func(emb []byte, m map[string]any) ([]byte, error) {
		if _, ok := m["mixed"]; ok {
			return nil, errors.New("encoder: mixed types SECRET-BODY")
		}
		return orig(emb, m)
	}
	spec := codexSpec(`developer_instructions = "SECRET-BODY"`, `mixed = [1, "a"]`)
	out, err := MergeCodex(codexEmbedded(t), spec)
	if !errors.Is(err, ErrCodexSpecializationFormat) || out != nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("spec-caused: err=%v out=%q", err, out)
	}

	renderCodexFn = func([]byte, map[string]any) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := MergeCodex(codexEmbedded(t), codexSpec(`developer_instructions = "X"`)); !errors.Is(err, ErrCodexEmbeddedFormat) {
		t.Errorf("embedded: err=%v", err)
	}
}

func mustMerge(t *testing.T, emb []byte, specs ...[]byte) []byte {
	t.Helper()
	out, err := MergeCodex(emb, specs...)
	if err != nil {
		t.Fatalf("MergeCodex: %v", err)
	}
	return out
}
