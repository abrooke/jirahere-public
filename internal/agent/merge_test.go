package agent

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type fm struct {
	keys map[string]string
	body string
}

func parseMerged(t *testing.T, b []byte) fm {
	t.Helper()
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("output lacks leading frontmatter delimiter: %.40q", s)
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatalf("output frontmatter unterminated")
	}
	out := fm{keys: map[string]string{}, body: rest[end+5:]}
	for _, line := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("frontmatter line %q has no colon", line)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(v[1 : len(v)-1])
		}
		if _, dup := out.keys[k]; dup {
			t.Fatalf("duplicate frontmatter key %q in output", k)
		}
		out.keys[k] = v
	}
	return out
}

func tools(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func spec(fmLines, body string) []byte {
	return []byte("---\n" + fmLines + "\n---\n\n" + body)
}

func embeddedClaude(t *testing.T, name string) ([]byte, fm) {
	t.Helper()
	_, b, err := Asset(name, "claude")
	if err != nil {
		t.Fatal(err)
	}
	return b, parseMerged(t, b)
}

func TestMergeClaude_SingleSpecialization(t *testing.T) {
	emb, e := embeddedClaude(t, "jirasistant")
	s := spec("name: spec\ndescription: Spec desc.\nmodel: opus\ntools: Bash, Task, Read, Write", "SPEC BODY\n")
	got, err := MergeClaude(emb, s)
	if err != nil {
		t.Fatalf("MergeClaude: %v", err)
	}
	m := parseMerged(t, got)
	if m.keys["name"] != "jirasistant-spec" {
		t.Errorf("name = %q", m.keys["name"])
	}
	if want := e.keys["description"] + " Spec desc."; m.keys["description"] != want {
		t.Errorf("description = %q, want %q", m.keys["description"], want)
	}
	if m.keys["model"] != "opus" {
		t.Errorf("model = %q, want spec value opus", m.keys["model"])
	}

	wantTools := append(tools(e.keys["tools"]), "Task", "Write")
	if g := tools(m.keys["tools"]); !eq(g, wantTools) {
		t.Errorf("tools = %v, want %v", g, wantTools)
	}
	wantBody := strings.Trim(e.body, "\n") + "\n\n" + "SPEC BODY"
	if strings.Trim(m.body, "\n") != wantBody {
		t.Errorf("body mismatch:\n got %.120q\nwant embedded + blank line + spec body", m.body)
	}
	if strings.Count(m.body, "SPEC BODY") != 1 {
		t.Errorf("spec body must appear exactly once")
	}
}

func TestMergeClaude_MultipleSpecializationsFlagOrder(t *testing.T) {
	emb, e := embeddedClaude(t, "jirasistant-req")
	s1 := spec("name: one\ndescription: First.\ntools: Task", "BODY-ONE\n")
	s2 := spec("name: two\ndescription: Second.\ntools: Task, Agent, Read", "BODY-TWO\n")
	got, err := MergeClaude(emb, s1, s2)
	if err != nil {
		t.Fatal(err)
	}
	m := parseMerged(t, got)
	if m.keys["name"] != "jirasistant-req-one-two" {
		t.Errorf("name = %q, want jirasistant-req-one-two", m.keys["name"])
	}
	if want := e.keys["description"] + " First. Second."; m.keys["description"] != want {
		t.Errorf("description = %q", m.keys["description"])
	}
	wantTools := append(tools(e.keys["tools"]), "Task", "Agent")
	if g := tools(m.keys["tools"]); !eq(g, wantTools) {
		t.Errorf("tools = %v, want %v", g, wantTools)
	}
	want := strings.Trim(e.body, "\n") + "\n\nBODY-ONE\n\nBODY-TWO"
	if strings.Trim(m.body, "\n") != want {
		t.Errorf("body not embedded + one + two separated by single blank lines")
	}

	rev, err := MergeClaude(emb, s2, s1)
	if err != nil {
		t.Fatal(err)
	}
	r := parseMerged(t, rev)
	if r.keys["name"] != "jirasistant-req-two-one" || strings.Index(r.body, "BODY-TWO") > strings.Index(r.body, "BODY-ONE") {
		t.Errorf("flag order not honored: name=%q", r.keys["name"])
	}
}

func TestMergeClaude_ModelPrecedence(t *testing.T) {
	emb, e := embeddedClaude(t, "jirasistant")
	if e.keys["model"] == "" {
		t.Fatal("embedded asset has no model; test premise broken")
	}
	withModel := func(m string) []byte {
		return spec("name: s\ndescription: d\nmodel: "+m, "B\n")
	}
	noModel := spec("name: s\ndescription: d", "B\n")
	tests := []struct {
		name  string
		specs [][]byte
		want  string
	}{
		{"spec wins", [][]byte{withModel("opus")}, "opus"},
		{"omitted keeps embedded", [][]byte{noModel}, e.keys["model"]},
		{"later omitted keeps earlier spec", [][]byte{withModel("opus"), noModel}, "opus"},
		{"later spec wins", [][]byte{withModel("opus"), withModel("sonnet")}, "sonnet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeClaude(emb, tt.specs...)
			if err != nil {
				t.Fatal(err)
			}
			if m := parseMerged(t, got); m.keys["model"] != tt.want {
				t.Errorf("model = %q, want %q", m.keys["model"], tt.want)
			}
		})
	}
}

func TestMergeClaude_ToolsEdgeCases(t *testing.T) {
	emb, e := embeddedClaude(t, "jirasistant")
	t.Run("spec without tools keeps embedded", func(t *testing.T) {
		got, err := MergeClaude(emb, spec("name: s\ndescription: d", "B\n"))
		if err != nil {
			t.Fatal(err)
		}
		if g := tools(parseMerged(t, got).keys["tools"]); !eq(g, tools(e.keys["tools"])) {
			t.Errorf("tools = %v", g)
		}
	})
	t.Run("duplicates inside spec collapse", func(t *testing.T) {
		got, err := MergeClaude(emb, spec("name: s\ndescription: d\ntools: Task,Task, Agent", "B\n"))
		if err != nil {
			t.Fatal(err)
		}
		want := append(tools(e.keys["tools"]), "Task", "Agent")
		if g := tools(parseMerged(t, got).keys["tools"]); !eq(g, want) {
			t.Errorf("tools = %v, want %v", g, want)
		}
	})
	for _, bad := range []string{"Read,,Bash", "Read, Bash,", "Read Write", "Web Fetch, Read"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			_, err := MergeClaude(emb, spec("name: s\ndescription: d\ntools: "+bad, "SECRET-BODY\n"))
			if !errors.Is(err, ErrSpecializationFormat) {
				t.Fatalf("err = %v, want ErrSpecializationFormat", err)
			}
			if strings.Contains(err.Error(), "SECRET-BODY") {
				t.Errorf("error leaks body: %v", err)
			}
		})
	}
}

func TestMergeClaude_OtherKeys(t *testing.T) {
	emb, _ := embeddedClaude(t, "jirasistant")
	got, err := MergeClaude(emb,
		spec("name: a\ndescription: d\ncolor: blue\nextra: only-in-one", "B1\n"),
		spec("name: b\ndescription: d\ncolor: red", "B2\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := parseMerged(t, got)
	if m.keys["extra"] != "only-in-one" {
		t.Errorf("unknown key not carried through: %v", m.keys)
	}
	if m.keys["color"] != "red" {
		t.Errorf("duplicate non-listed key: color = %q, want later spec's red", m.keys["color"])
	}
}

func TestMergeClaude_Rejections(t *testing.T) {
	emb, _ := embeddedClaude(t, "jirasistant")
	tests := []struct {
		name string
		spec string
	}{
		{"no frontmatter", "SECRET-BODY only\n"},
		{"leading blank before ---", "\n---\nname: s\n---\n\nSECRET-BODY\n"},
		{"unterminated", "---\nname: s\ndescription: d\n\nSECRET-BODY\n"},
		{"unparseable line", "---\nthis is not a mapping SECRET-BODY\n---\n\nSECRET-BODY\n"},
		{"unclosed flow", "---\nname: [s\n---\n\nSECRET-BODY\n"},
		{"empty body", "---\nname: s\ndescription: d\n---\n"},
		{"whitespace body", "---\nname: s\ndescription: d\n---\n \n\t\n"},
		{"empty file", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeClaude(emb, []byte(tt.spec))
			if !errors.Is(err, ErrSpecializationFormat) {
				t.Fatalf("err = %v, want ErrSpecializationFormat", err)
			}
			if got != nil {
				t.Errorf("returned %d bytes with error, want nil", len(got))
			}
			if strings.Contains(err.Error(), "SECRET-BODY") {
				t.Errorf("error leaks body: %v", err)
			}
		})
	}
	t.Run("second spec bad rejects whole merge", func(t *testing.T) {
		_, err := MergeClaude(emb, spec("name: a\ndescription: d", "OK\n"), []byte("no frontmatter"))
		if !errors.Is(err, ErrSpecializationFormat) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestMergeClaude_NoSpecsAndImmutability(t *testing.T) {
	emb, _ := embeddedClaude(t, "jirasistant")
	orig := append([]byte(nil), emb...)
	s := spec("name: s\ndescription: d\ntools: Task", "B\n")
	sOrig := append([]byte(nil), s...)
	if _, err := MergeClaude(emb, s); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(emb, orig) || !bytes.Equal(s, sOrig) {
		t.Error("MergeClaude mutated its inputs")
	}

	_, again, err := Asset("jirasistant", "claude")
	if err != nil || !bytes.Equal(again, orig) {
		t.Errorf("embedded asset changed after merge (err=%v)", err)
	}
}

func TestMergeClaude_EmptyNameDescriptionTreatedAsOmitted(t *testing.T) {
	emb, e := embeddedClaude(t, "jirasistant")
	for _, fmLines := range []string{
		"name:\ndescription:",
		"name: \"\"\ndescription: \"\"",
	} {
		got, err := MergeClaude(emb, spec(fmLines, "BODY\n"))
		if err != nil {
			t.Fatalf("MergeClaude(%q): %v", fmLines, err)
		}
		m := parseMerged(t, got)
		if m.keys["name"] != e.keys["name"] {
			t.Errorf("%q: name = %q, want unchanged %q", fmLines, m.keys["name"], e.keys["name"])
		}
		if m.keys["description"] != e.keys["description"] {
			t.Errorf("%q: description = %q, want unchanged %q", fmLines, m.keys["description"], e.keys["description"])
		}
	}
}

func TestMergeClaude_MalformedEmbeddedIsDistinctError(t *testing.T) {
	_, err := MergeClaude([]byte("no frontmatter"), spec("name: x", "B\n"))
	if !errors.Is(err, ErrEmbeddedFormat) || errors.Is(err, ErrSpecializationFormat) {
		t.Errorf("err = %v, want ErrEmbeddedFormat only", err)
	}
}

func TestMergeClaude_UnsupportedConstructNamed(t *testing.T) {
	emb, _ := embeddedClaude(t, "jirasistant")
	tests := []struct {
		name, spec, construct, key string
		line                       int
	}{
		{"block scalar", "---\nname: s\ndescription: |\n  SECRET-VALUE\n---\n\nSECRET-BODY\n", ConstructBlockScalar, "description", 3},
		{"folded scalar", "---\nname: s\ndescription: >\n  SECRET-VALUE\n---\n\nSECRET-BODY\n", ConstructBlockScalar, "description", 3},
		{"flow list", "---\nname: s\ntools: [SECRET-VALUE, B]\n---\n\nSECRET-BODY\n", ConstructFlowList, "tools", 3},
		{"single-quoted", "---\nname: 'SECRET-VALUE'\n---\n\nSECRET-BODY\n", ConstructSingleQuoted, "name", 2},
		{"unsupported escape", "---\nname: s\ndescription: \"SECRET-VALUE\\n\"\n---\n\nSECRET-BODY\n", ConstructUnsupportedEscape, "description", 3},
		{"blank line", "---\nname: s\n\ndescription: SECRET-VALUE\n---\n\nSECRET-BODY\n", ConstructBlankLine, "", 3},
		{"comment line", "---\nname: s\n# SECRET-VALUE\n---\n\nSECRET-BODY\n", ConstructCommentLine, "", 3},
		{"byte-order mark", "\xef\xbb\xbf---\nname: SECRET-VALUE\n---\n\nSECRET-BODY\n", ConstructBOM, "", 1},
		{"CRLF line numbers", "---\r\nname: s\r\ntools: [SECRET-VALUE]\r\n---\r\n\r\nSECRET-BODY\r\n", ConstructFlowList, "tools", 3},
		{"two categories: first line wins", "---\nname: 'SECRET-VALUE'\ntools: [A]\n---\n\nSECRET-BODY\n", ConstructSingleQuoted, "name", 2},
		{"BOM plus later construct reports the BOM", "\xef\xbb\xbf---\ndescription: |\n---\n\nSECRET-BODY\n", ConstructBOM, "", 1},
		{"BOM plus single-quoted reports the BOM", "\xef\xbb\xbf---\nname: 'q'\n---\n\nSECRET-BODY\n", ConstructBOM, "", 1},
		{"BOM plus empty body reports the BOM", "\xef\xbb\xbf---\nname: s\n---\n", ConstructBOM, "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeClaude(emb, []byte(tt.spec))
			if got != nil || !errors.Is(err, ErrSpecializationFormat) {
				t.Fatalf("got=%d bytes err=%v, want nil + ErrSpecializationFormat", len(got), err)
			}
			var ue *UnsupportedConstructError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want *UnsupportedConstructError", err)
			}
			if ue.Construct != tt.construct || ue.Key != tt.key || ue.Line != tt.line {
				t.Errorf("got %+v, want %s/%q/line %d", *ue, tt.construct, tt.key, tt.line)
			}
			if !strings.Contains(err.Error(), tt.construct) || strings.Contains(err.Error(), "SECRET") {
				t.Errorf("error text %q must name the construct and carry no value/body", err)
			}
		})
	}
	for _, generic := range []string{
		"SECRET-BODY\n",
		"---\nthis is not a mapping SECRET-BODY\n---\n\nSECRET-BODY\n",
		"---\nname: \"SECRET-VALUE\n---\n\nSECRET-BODY\n",
		"---\nname: s\ndescription: d\ntools: Read Write\n---\n\nSECRET-BODY\n",
		"---\nname: s\nname: t\n---\n\nSECRET-BODY\n",
		"---\nname: s\n---\n",
		"\xef\xbb\xbf---\nname: 'q'\nSECRET-BODY\n",
		"---\nname: s\n\ndescription: d\nSECRET-BODY\n",
		"---\nname: s\n\nNote: 'q'\nExample: [x]\n",
		"---\nname: s\nNote: 'q'\nExample: [x]\n",
	} {
		_, err := MergeClaude(emb, []byte(generic))
		var ue *UnsupportedConstructError
		if !errors.Is(err, ErrSpecializationFormat) || errors.As(err, &ue) || err.Error() != ErrSpecializationFormat.Error() {
			t.Errorf("%q: err = %v, want the generic error", generic, err)
		}
		if strings.Contains(err.Error(), "Note") || strings.Contains(err.Error(), "Example") || strings.Contains(err.Error(), "blank") {
			t.Errorf("%q: err = %v names a body word or construct", generic, err)
		}
	}
}
