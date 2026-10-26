package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func decodeCodex(t *testing.T, b []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if _, err := toml.Decode(string(b), &m); err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, b)
	}
	return m
}

func embeddedWith(t *testing.T, extra ...string) []byte {
	t.Helper()
	return append(codexEmbedded(t), []byte("\n"+strings.Join(extra, "\n")+"\n")...)
}

func nestedSpec(lines ...string) []byte {
	return codexSpec(append([]string{`developer_instructions = "X"`, `[profiles.p]`}, lines...)...)
}

func profileP(m map[string]any) map[string]any {
	return m["profiles"].(map[string]any)["p"].(map[string]any)
}

func TestMergeCodex_NestedNoCounterpartInOrdering(t *testing.T) {
	for _, line := range []string{
		`sandbox_mode = "read-only"`, `sandbox_mode = "danger-full-access"`,
		`approval_policy = "never"`, `web_search = "indexed"`,
	} {
		m := mergeCodexMap(t, nestedSpec(line))
		k, v, _ := strings.Cut(line, " = ")
		if got := profileP(m)[k]; got != strings.Trim(v, `"`) {
			t.Errorf("%s: got %v", line, got)
		}
	}
}

func TestMergeCodex_NestedRejectedAtAnyDepth(t *testing.T) {
	bad := []string{
		`sandbox_mode = "yolo"`, `sandbox_mode = true`,
		`approval_policy = "untrusted"`, `approval_policy = "on-failure"`,
		`approval_policy = { granular = { sandbox_approval = true } }`,
		`web_search = "everything"`,
	}
	for _, line := range bad {
		for _, shape := range []string{"profiles.p", "profiles.p.deep.er"} {
			spec := codexSpec(`developer_instructions = "SECRET-BODY"`, `[`+shape+`]`, line)
			out, err := MergeCodex(codexEmbedded(t), spec)
			if !errors.Is(err, ErrCodexSpecializationSecurity) || out != nil || strings.Contains(err.Error(), "SECRET") {
				t.Errorf("%s in [%s]: err=%v out=%q", line, shape, err, out)
			}
		}
	}
	for _, spec := range [][]byte{
		codexSpec(`developer_instructions = "X"`, `[profiles.p.approval_policy.granular]`, `sandbox_approval = true`),
		codexSpec(`developer_instructions = "X"`, `[[hooks]]`, `sandbox_mode = "yolo"`),
	} {
		if out, err := MergeCodex(codexEmbedded(t), spec); !errors.Is(err, ErrCodexSpecializationSecurity) || out != nil {
			t.Errorf("err=%v out=%q", err, out)
		}
	}
}

func TestMergeCodex_NestedDangerOverEmbeddedNoneOrReadOnly(t *testing.T) {
	emb := embeddedWith(t, `[profiles.p]`, `sandbox_mode = "read-only"`)
	out, err := MergeCodex(emb, nestedSpec(`sandbox_mode = "danger-full-access"`))
	if err != nil {
		t.Fatal(err)
	}
	if got := profileP(decodeCodex(t, out))["sandbox_mode"]; got != "danger-full-access" {
		t.Errorf("over read-only: got %v", got)
	}
	m := mergeCodexMap(t, nestedSpec(`sandbox_mode = "danger-full-access"`))
	if got := profileP(m)["sandbox_mode"]; got != "danger-full-access" {
		t.Errorf("over none: got %v", got)
	}
}

func TestMergeCodex_NestedBothSidesBothDirections(t *testing.T) {
	for key, order := range codexSecurityOrderings {
		for i, e := range order {
			for j, s := range order {
				emb := embeddedWith(t, `[profiles.p]`, key+` = "`+e+`"`)
				out, err := MergeCodex(emb, nestedSpec(key+` = "`+s+`"`))
				if err != nil {
					t.Fatal(err)
				}
				want := order[max(i, j)]
				if got := profileP(decodeCodex(t, out))[key]; got != want {
					t.Errorf("%s embedded %s spec %s: got %v want %s", key, e, s, got, want)
				}
			}
		}
	}
}

func TestMergeCodex_NestedAcrossSpecsKeepsMorePermissive(t *testing.T) {
	m := mergeCodexMap(t,
		nestedSpec(`web_search = "live"`, `other = 1`),
		nestedSpec(`web_search = "cached"`, `other = 2`))
	p := profileP(m)
	if p["web_search"] != "live" || p["other"] != int64(2) {
		t.Errorf("got %v", p)
	}
}

func TestMergeCodex_EmbeddedNestedUnsafeIsEmbeddedError(t *testing.T) {
	emb := embeddedWith(t, `[profiles.p]`, `sandbox_mode = "yolo"`)
	out, err := MergeCodex(emb, nestedSpec(`web_search = "live"`))
	if !errors.Is(err, ErrCodexEmbeddedFormat) || out != nil {
		t.Errorf("err=%v out=%q", err, out)
	}
}

func TestMergeCodex_NetworkAccessSpecTrueOverEmbeddedFalse(t *testing.T) {
	m := mergeCodexMap(t, codexSpec(`developer_instructions = "X"`, `[sandbox_workspace_write]`, `network_access = true`))
	if m["sandbox_workspace_write"].(map[string]any)["network_access"] != true {
		t.Errorf("got %v", m["sandbox_workspace_write"])
	}
}

func TestMergeCodex_InstructionsJoinWhitespaceAndInteriorVerbatim(t *testing.T) {
	interior := "\n\nA  line\twith tab  \n\n\n  indented\n"
	spec1 := codexSpec("developer_instructions = '''\n\n\nHEAD" + interior + "TAIL\n\n\n'''")
	spec2 := codexSpec(`developer_instructions = "\n\nNEXT\n\n"`)
	di := mergeCodexMap(t, spec1, spec2)["developer_instructions"].(string)
	if want := "HEAD" + interior + "TAIL" + "\n\nNEXT\n"; !strings.HasSuffix(di, want) {
		t.Errorf("join/interior not as specified: %q", di)
	}
	if strings.Contains(di, "TAIL\n\n\n") || strings.Contains(di, "\n\n\n\nNEXT") {
		t.Errorf("boundary whitespace not normalized: %q", di)
	}
}
