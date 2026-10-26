package agent

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func codexSpec(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func codexEmbedded(t *testing.T) []byte {
	t.Helper()
	_, b, err := Asset("jirasistant", "codex")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mergeCodexMap(t *testing.T, specs ...[]byte) map[string]any {
	t.Helper()
	out, err := MergeCodex(codexEmbedded(t), specs...)
	if err != nil {
		t.Fatalf("MergeCodex: %v", err)
	}
	m := map[string]any{}
	if _, err := toml.Decode(string(out), &m); err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, out)
	}
	return m
}

const specDI = `developer_instructions = '''
SPEC-ONE
line two
'''`

func TestMergeCodex_SingleSpecialization(t *testing.T) {
	emb := codexEmbedded(t)
	var embM map[string]any
	if _, err := toml.Decode(string(emb), &embM); err != nil {
		t.Fatal(err)
	}
	m := mergeCodexMap(t, codexSpec(`description = "Spec desc."`, `model = "gpt-x"`, specDI))
	di := m["developer_instructions"].(string)
	embDI := strings.TrimRight(embM["developer_instructions"].(string), "\n")
	if want := embDI + "\n\nSPEC-ONE\nline two\n"; di != want {
		t.Errorf("developer_instructions not embedded + blank line + spec")
	}
	if want := embM["description"].(string) + " Spec desc."; m["description"] != want {
		t.Errorf("description = %q", m["description"])
	}
	if m["model"] != "gpt-x" {
		t.Errorf("model = %v", m["model"])
	}
	if m["sandbox_mode"] != "read-only" || m["approval_policy"] != "on-request" || m["web_search"] != "live" {
		t.Errorf("security fields changed without spec: %v %v %v", m["sandbox_mode"], m["approval_policy"], m["web_search"])
	}
	if sw := m["sandbox_workspace_write"].(map[string]any); sw["network_access"] != false {
		t.Errorf("network_access not carried: %v", sw)
	}
}

func TestMergeCodex_MultipleSpecializationsFlagOrder(t *testing.T) {
	a := codexSpec(`developer_instructions = "A-TEXT"`, `description = "A."`, `model = "m1"`)
	b := codexSpec(`developer_instructions = "B-TEXT"`, `description = "B."`)
	m := mergeCodexMap(t, a, b)
	di := m["developer_instructions"].(string)
	if strings.Index(di, "You are jirasistant") >= strings.Index(di, "A-TEXT") || strings.Index(di, "A-TEXT") >= strings.Index(di, "B-TEXT") {
		t.Error("developer_instructions not in flag order")
	}
	if !strings.HasSuffix(m["description"].(string), " A. B.") {
		t.Errorf("description = %q", m["description"])
	}
	if m["model"] != "m1" {
		t.Errorf("model omitted in later spec must keep earlier: %v", m["model"])
	}
	m2 := mergeCodexMap(t, a, codexSpec(`developer_instructions = "B"`, `model = "m2"`))
	if m2["model"] != "m2" {
		t.Errorf("later model must win: %v", m2["model"])
	}
}

func TestMergeCodex_SecurityOrderingBothDirections(t *testing.T) {

	tests := []struct {
		key, spec, want string
	}{
		{"sandbox_mode", "workspace-write", "workspace-write"},
		{"sandbox_mode", "danger-full-access", "danger-full-access"},
		{"sandbox_mode", "read-only", "read-only"},
		{"approval_policy", "never", "never"},
		{"approval_policy", "on-request", "on-request"},
		{"web_search", "disabled", "live"},
		{"web_search", "cached", "live"},
		{"web_search", "indexed", "live"},
		{"web_search", "live", "live"},
	}
	for _, tt := range tests {
		m := mergeCodexMap(t, codexSpec(`developer_instructions = "X"`, tt.key+` = "`+tt.spec+`"`))
		if m[tt.key] != tt.want {
			t.Errorf("%s spec %q: got %v, want %s", tt.key, tt.spec, m[tt.key], tt.want)
		}
	}

	m := mergeCodexMap(t,
		codexSpec(`developer_instructions = "X"`, `sandbox_mode = "danger-full-access"`, `approval_policy = "never"`),
		codexSpec(`developer_instructions = "Y"`, `sandbox_mode = "workspace-write"`, `approval_policy = "on-request"`))
	if m["sandbox_mode"] != "danger-full-access" || m["approval_policy"] != "never" {
		t.Errorf("fold lost the more permissive value: %v %v", m["sandbox_mode"], m["approval_policy"])
	}
}

func TestMergeCodex_EmbeddedLowerThanEveryStep(t *testing.T) {

	for _, pair := range [][2]string{{"read-only", "workspace-write"}, {"workspace-write", "danger-full-access"}} {
		m := mergeCodexMap(t,
			codexSpec(`developer_instructions = "X"`, `sandbox_mode = "`+pair[0]+`"`),
			codexSpec(`developer_instructions = "Y"`, `sandbox_mode = "`+pair[1]+`"`))
		if m["sandbox_mode"] != pair[1] {
			t.Errorf("%v: got %v", pair, m["sandbox_mode"])
		}
	}

	order := codexSecurityOrderings["web_search"]
	for i := range order {
		for j := range order {
			acc := map[string]any{"web_search": order[i]}
			if err := foldCodex(acc, map[string]any{"web_search": order[j]}); err != nil {
				t.Fatal(err)
			}
			want := order[max(i, j)]
			if acc["web_search"] != want {
				t.Errorf("fold(%s,%s) = %v, want %s", order[i], order[j], acc["web_search"], want)
			}
		}
	}
}

func TestMergeCodex_AbsentSecurityFieldLeavesEarlier(t *testing.T) {
	m := mergeCodexMap(t, codexSpec(`developer_instructions = "X"`))
	if m["sandbox_mode"] != "read-only" || m["approval_policy"] != "on-request" || m["web_search"] != "live" {
		t.Errorf("security fields changed: %v", m)
	}
}

func TestMergeCodex_OtherKeysCarriedAndTablesMerged(t *testing.T) {
	m := mergeCodexMap(t, codexSpec(
		`developer_instructions = "X"`,
		`custom_key = "v"`,
		`nickname_candidates = ["a", "b"]`,
		`[sandbox_workspace_write]`,
		`network_access = true`,
		`writable_roots = ["/tmp/x"]`,
		`[tools.extra]`,
		`enabled = true`,
	))
	if m["custom_key"] != "v" {
		t.Errorf("unknown scalar not carried")
	}
	if arr, ok := m["nickname_candidates"].([]any); !ok || len(arr) != 2 {
		t.Errorf("array not carried: %v", m["nickname_candidates"])
	}
	sw := m["sandbox_workspace_write"].(map[string]any)
	if sw["network_access"] != true {
		t.Errorf("scalar in both: spec must win, got %v", sw["network_access"])
	}
	if _, ok := sw["writable_roots"]; !ok {
		t.Errorf("table key from spec not merged in")
	}
	if tools := m["tools"].(map[string]any); tools["extra"].(map[string]any)["enabled"] != true {
		t.Errorf("new table not carried")
	}
}

func TestMergeCodex_Rejections(t *testing.T) {
	tests := []struct {
		name string
		spec []byte
		want error
	}{
		{"invalid TOML", codexSpec(`developer_instructions = "SECRET-BODY`), ErrCodexSpecializationFormat},
		{"claude file", []byte("---\nname: s\n---\n\nSECRET-BODY\n"), ErrCodexSpecializationFormat},
		{"no developer_instructions", codexSpec(`description = "SECRET-BODY"`), ErrCodexSpecializationFormat},
		{"blank developer_instructions", codexSpec(`developer_instructions = "  "`), ErrCodexSpecializationFormat},
		{"non-string developer_instructions", codexSpec(`developer_instructions = 5`), ErrCodexSpecializationFormat},
		{"non-string description", codexSpec(`developer_instructions = "X"`, `description = 5`), ErrCodexSpecializationFormat},
		{"granular table", codexSpec(`developer_instructions = "X"`, `[approval_policy.granular]`, `sandbox_approval = true`), ErrCodexSpecializationSecurity},
		{"untrusted", codexSpec(`developer_instructions = "X"`, `approval_policy = "untrusted"`), ErrCodexSpecializationSecurity},
		{"on-failure", codexSpec(`developer_instructions = "X"`, `approval_policy = "on-failure"`), ErrCodexSpecializationSecurity},
		{"unknown sandbox", codexSpec(`developer_instructions = "X"`, `sandbox_mode = "yolo"`), ErrCodexSpecializationSecurity},
		{"unknown web_search", codexSpec(`developer_instructions = "X"`, `web_search = "everything"`), ErrCodexSpecializationSecurity},
		{"non-string sandbox", codexSpec(`developer_instructions = "X"`, `sandbox_mode = true`), ErrCodexSpecializationSecurity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MergeCodex(codexEmbedded(t), tt.spec)
			if !errors.Is(err, tt.want) || out != nil {
				t.Fatalf("err=%v out=%q, want %v and nil", err, out, tt.want)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Errorf("error leaks content: %v", err)
			}
		})
	}

	if out, err := MergeCodex(codexEmbedded(t), codexSpec(`developer_instructions = "X"`), codexSpec(`sandbox_mode = 1`)); err == nil || out != nil {
		t.Errorf("second bad spec: err=%v out=%q", err, out)
	}
}

func TestMergeCodex_MalformedEmbeddedIsDistinctError(t *testing.T) {
	_, err := MergeCodex([]byte("SECRET not toml ="), codexSpec(`developer_instructions = "X"`))
	if !errors.Is(err, ErrCodexEmbeddedFormat) || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("err = %v", err)
	}
}

func TestMergeCodex_RenderRoundTripAndHeader(t *testing.T) {
	emb := codexEmbedded(t)
	out, err := MergeCodex(emb, codexSpec(`developer_instructions = "X"`))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "# jirasistant profile for Codex.") {
		t.Errorf("embedded header comment not preserved: %.60q", s)
	}
	if !strings.Contains(s, "developer_instructions = '''\n") {
		t.Errorf("developer_instructions not rendered as ''' literal")
	}
	if strings.Contains(s, "Native Codex web search") {
		t.Errorf("non-header embedded comment carried")
	}

	var m1, m2 map[string]any
	if _, err := toml.Decode(s, &m1); err != nil {
		t.Fatal(err)
	}
	out2, err := MergeCodex(out, codexSpec(`developer_instructions = "Y"`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := toml.Decode(string(out2), &m2); err != nil {
		t.Fatal(err)
	}
}

func TestMergeCodex_LiteralUnsafeInstructionsFallBackToEscapedString(t *testing.T) {
	m := mergeCodexMap(t, codexSpec(`developer_instructions = "has ''' inside and \ttab"`))
	if di := m["developer_instructions"].(string); !strings.Contains(di, "has ''' inside and \ttab") {
		t.Errorf("text lost: %q", di)
	}
}

func TestMergeCodex_NoSpecsAndImmutability(t *testing.T) {
	emb := codexEmbedded(t)
	snap := append([]byte(nil), emb...)
	spec := codexSpec(`developer_instructions = "X"`)
	specSnap := append([]byte(nil), spec...)
	if _, err := MergeCodex(emb, spec); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(emb, snap) || !bytes.Equal(spec, specSnap) {
		t.Error("inputs modified")
	}
	if _, err := MergeCodex(emb); err != nil {
		t.Errorf("no specs: %v", err)
	}
}
