package adf

import (
	"encoding/json"
	"strings"
)

type node struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content []node `json:"content"`
	Attrs   struct {
		Text string `json:"text"`
	} `json:"attrs"`
}

func PlainText(doc json.RawMessage) string {
	if len(doc) == 0 {
		return ""
	}

	var root node
	if err := json.Unmarshal(doc, &root); err != nil {
		return ""
	}

	var paragraphs []string
	for _, child := range root.Content {
		switch child.Type {
		case "paragraph":
			paragraphs = append(paragraphs, paragraphText(child))
		case "blockquote":
			for _, grandchild := range child.Content {
				if grandchild.Type != "paragraph" {
					continue
				}
				paragraphs = append(paragraphs, paragraphText(grandchild))
			}
		}
	}

	var b strings.Builder
	for i, p := range paragraphs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p)
	}
	return sanitize(b.String())
}

func paragraphText(p node) string {
	var b strings.Builder
	for _, child := range p.Content {
		switch child.Type {
		case "text":
			b.WriteString(child.Text)
		case "mention":
			if child.Attrs.Text != "" {
				b.WriteString(child.Attrs.Text)
			}
		case "hardBreak":
			b.WriteString("\n")
		}
	}
	return b.String()
}

type adfInlineNode struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type adfBlockNode struct {
	Type    string          `json:"type"`
	Content []adfInlineNode `json:"content,omitempty"`
}

type adfDocNode struct {
	Type    string         `json:"type"`
	Version int            `json:"version"`
	Content []adfBlockNode `json:"content"`
}

func FromPlainText(s string) json.RawMessage {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	doc := adfDocNode{Type: "doc", Version: 1, Content: make([]adfBlockNode, 0)}

	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		inline := make([]adfInlineNode, 0, len(para)*2-1)
		for i, line := range para {
			if i > 0 {
				inline = append(inline, adfInlineNode{Type: "hardBreak"})
			}
			inline = append(inline, adfInlineNode{Type: "text", Text: line})
		}
		doc.Content = append(doc.Content, adfBlockNode{Type: "paragraph", Content: inline})
		para = nil
	}

	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		para = append(para, line)
	}
	flush()

	encoded, err := json.Marshal(doc)
	if err != nil {

		return json.RawMessage(`{"type":"doc","version":1,"content":[]}`)
	}
	return encoded
}

func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' {
			b.WriteRune(r)
			continue
		}
		switch {
		case r < 0x20:
		case r >= 0x80 && r <= 0x9F:
		case r >= 0x202A && r <= 0x202E:
		case r >= 0x2066 && r <= 0x2069:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
