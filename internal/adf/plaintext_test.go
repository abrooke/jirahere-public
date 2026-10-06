package adf

import (
	"encoding/json"
	"testing"
)

func TestPlainText(t *testing.T) {
	tests := []struct {
		name string
		doc  json.RawMessage
		want string
	}{
		{
			name: "plain single paragraph",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Original description text."}]}]}`),
			want: "Original description text.",
		},
		{
			name: "multiple paragraphs separated by a blank line",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"First paragraph."}]},
				{"type":"paragraph","content":[{"type":"text","text":"Second paragraph."}]}
			]}`),
			want: "First paragraph.\n\nSecond paragraph.",
		},
		{
			name: "bold, italic, and link marks flattened to plain text",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[
					{"type":"text","text":"This is "},
					{"type":"text","text":"bold","marks":[{"type":"strong"}]},
					{"type":"text","text":" and "},
					{"type":"text","text":"italic","marks":[{"type":"em"}]},
					{"type":"text","text":" and a "},
					{"type":"text","text":"link","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},
					{"type":"text","text":"."}
				]}
			]}`),
			want: "This is bold and italic and a link.",
		},
		{
			name: "blockquote paragraphs recursed into and treated as top-level",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"Before the quote."}]},
				{"type":"blockquote","content":[
					{"type":"paragraph","content":[{"type":"text","text":"Quoted line one."}]},
					{"type":"paragraph","content":[{"type":"text","text":"Quoted line two."}]}
				]},
				{"type":"paragraph","content":[{"type":"text","text":"After the quote."}]}
			]}`),
			want: "Before the quote.\n\nQuoted line one.\n\nQuoted line two.\n\nAfter the quote.",
		},
		{
			name: "mention node emits attrs.text, surrounding plain text kept",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[
					{"type":"text","text":"Assigned to "},
					{"type":"mention","attrs":{"id":"123","text":"@Alice"}},
					{"type":"text","text":" for review."}
				]}
			]}`),
			want: "Assigned to @Alice for review.",
		},
		{
			name: "mention node without attrs.text skipped silently",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[
					{"type":"text","text":"Assigned to "},
					{"type":"mention","attrs":{"id":"123"}},
					{"type":"text","text":" for review."}
				]}
			]}`),
			want: "Assigned to  for review.",
		},
		{
			name: "embedded control and escape bytes stripped, newline preserved",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u001b[31mRED\u001b[0mafter\u0007bell\nnext line"}]}]}`),
			want: "before[31mRED[0mafterbell\nnext line",
		},
		{
			name: "embedded C1 control character stripped, surrounding text intact",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u009bCSI\u009bafter"}]}]}`),
			want: "beforeCSIafter",
		},
		{
			name: "embedded C1 control range edges (U+0080 and U+009F) stripped, surrounding text intact",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u0080X\u009fafter"}]}]}`),
			want: "beforeXafter",
		},
		{
			name: "embedded bidi override character stripped, surrounding text intact",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u202eRLO\u202cafter"}]}]}`),
			want: "beforeRLOafter",
		},
		{
			name: "embedded bidi override range lower edge (U+202A) stripped, surrounding text intact",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u202aLRE\u202aafter"}]}]}`),
			want: "beforeLREafter",
		},
		{
			name: "embedded bidi isolate range edges (U+2066 and U+2069) stripped, surrounding text intact",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u2066LRI\u2069after"}]}]}`),
			want: "beforeLRIafter",
		},
		{
			name: "DEL byte survives unstripped (explicitly out of scope)",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u007fDEL\u007fafter"}]}]}`),
			want: "before\u007fDEL\u007fafter",
		},
		{
			name: "implicit bidi mark survives unstripped (explicitly out of scope)",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"before\u200eLRM\u200eafter"}]}]}`),
			want: "before\u200eLRM\u200eafter",
		},
		{
			name: "hardBreak produces a newline",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[
					{"type":"text","text":"Line one"},
					{"type":"hardBreak"},
					{"type":"text","text":"Line two"}
				]}
			]}`),
			want: "Line one\nLine two",
		},
		{
			name: "nested text two levels inside a non-paragraph container not extracted",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"Before the table."}]},
				{"type":"table","content":[
					{"type":"tableRow","content":[
						{"type":"tableCell","content":[
							{"type":"paragraph","content":[{"type":"text","text":"Hidden cell text."}]}
						]}
					]}
				]},
				{"type":"paragraph","content":[{"type":"text","text":"After the table."}]}
			]}`),
			want: "Before the table.\n\nAfter the table.",
		},
		{
			name: "empty document",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[]}`),
			want: "",
		},
		{
			name: "nil raw message",
			doc:  nil,
			want: "",
		},
		{
			name: "unrecognized node type skipped, surrounding paragraphs kept",
			doc: json.RawMessage(`{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"Before the table."}]},
				{"type":"table","content":[{"type":"tableRow","content":[]}]},
				{"type":"paragraph","content":[{"type":"text","text":"After the table."}]}
			]}`),
			want: "Before the table.\n\nAfter the table.",
		},
		{
			name: "malformed JSON returns empty string, not an error",
			doc:  json.RawMessage(`{not valid json`),
			want: "",
		},
		{
			name: "empty raw message",
			doc:  json.RawMessage(``),
			want: "",
		},
		{
			name: "no-paragraph document (only unsupported node types)",
			doc:  json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title"}]}]}`),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlainText(tt.doc); got != tt.want {
				t.Errorf("PlainText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromPlainText(t *testing.T) {
	const emptyDoc = `{"type":"doc","version":1,"content":[]}`

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty input yields an empty document",
			in:   "",
			want: emptyDoc,
		},
		{
			name: "whitespace-only input yields an empty document",
			in:   "   \n\t\n   ",
			want: emptyDoc,
		},
		{
			name: "single paragraph",
			in:   "Original description text.",
			want: `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Original description text."}]}]}`,
		},
		{
			name: "multiple paragraphs separated by a blank line",
			in:   "First paragraph.\n\nSecond paragraph.",
			want: `{"type":"doc","version":1,"content":[` +
				`{"type":"paragraph","content":[{"type":"text","text":"First paragraph."}]},` +
				`{"type":"paragraph","content":[{"type":"text","text":"Second paragraph."}]}]}`,
		},
		{
			name: "single newline within a paragraph is a hard break",
			in:   "Line one\nLine two",
			want: `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[` +
				`{"type":"text","text":"Line one"},{"type":"hardBreak"},{"type":"text","text":"Line two"}]}]}`,
		},
		{
			name: "mixed blank-line breaks and single-newline hard breaks",
			in:   "A\nB\n\nC\nD",
			want: `{"type":"doc","version":1,"content":[` +
				`{"type":"paragraph","content":[{"type":"text","text":"A"},{"type":"hardBreak"},{"type":"text","text":"B"}]},` +
				`{"type":"paragraph","content":[{"type":"text","text":"C"},{"type":"hardBreak"},{"type":"text","text":"D"}]}]}`,
		},
		{
			name: "runs of blank lines collapse and leading/trailing blanks are dropped",
			in:   "\n\n\nA\n\n\n\nB\n\n",
			want: `{"type":"doc","version":1,"content":[` +
				`{"type":"paragraph","content":[{"type":"text","text":"A"}]},` +
				`{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}`,
		},
		{
			name: "a whitespace-only line acts as a paragraph separator",
			in:   "A\n \t \nB",
			want: `{"type":"doc","version":1,"content":[` +
				`{"type":"paragraph","content":[{"type":"text","text":"A"}]},` +
				`{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}`,
		},
		{
			name: "CRLF and lone CR are normalized to LF",
			in:   "A\r\nB\r\n\r\nC\rD",
			want: `{"type":"doc","version":1,"content":[` +
				`{"type":"paragraph","content":[{"type":"text","text":"A"},{"type":"hardBreak"},{"type":"text","text":"B"}]},` +
				`{"type":"paragraph","content":[{"type":"text","text":"C"},{"type":"hardBreak"},{"type":"text","text":"D"}]}]}`,
		},
		{
			name: "indentation inside a paragraph line is preserved",
			in:   "head\n    indented tail",
			want: `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[` +
				`{"type":"text","text":"head"},{"type":"hardBreak"},{"type":"text","text":"    indented tail"}]}]}`,
		},
		{

			name: "markdown-looking characters pass through literally",
			in:   "*not* bold, # not a heading, [not](a-link), a_b, x|y",
			want: `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":` +
				`"*not* bold, # not a heading, [not](a-link), a_b, x|y"}]}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(FromPlainText(tt.in))
			if got != tt.want {
				t.Errorf("FromPlainText(%q) =\n  %s\nwant\n  %s", tt.in, got, tt.want)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("FromPlainText(%q) produced invalid JSON: %s", tt.in, got)
			}
		})
	}
}

func TestFromPlainText_LiteralTextSurvivesPlainText(t *testing.T) {
	in := "# Heading *stars* and _under_\n[link](http://example.com)"
	want := "# Heading *stars* and _under_\n[link](http://example.com)"
	if got := PlainText(FromPlainText(in)); got != want {
		t.Errorf("PlainText(FromPlainText(%q)) = %q, want %q", in, got, want)
	}
}

func TestFromPlainText_HTMLCharsDecodeLiterally(t *testing.T) {
	const in = "a <b> tag & an &amp; entity <not/> closed"
	raw := FromPlainText(in)
	if !json.Valid(raw) {
		t.Fatalf("FromPlainText(%q) is not valid JSON: %s", in, raw)
	}
	if got := PlainText(raw); got != in {
		t.Errorf("PlainText(FromPlainText(%q)) = %q, want the literal characters back", in, got)
	}
}
