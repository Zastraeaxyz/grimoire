package rss

import (
	"strings"

	xhtml "golang.org/x/net/html"
)

// htmlToText renders a feed's HTML body as plain text suitable for a Discord
// embed. Images and video tags are dropped because they become attachments,
// and escaped markup from reblogs is unwrapped.
func htmlToText(raw string) string {
	return normalizeText(htmlText(raw, 0))
}

func htmlText(raw string, depth int) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if depth > 2 {
		return raw
	}

	doc, err := xhtml.Parse(strings.NewReader(raw))
	if err != nil {
		return raw
	}

	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		switch n.Type {
		case xhtml.TextNode:
			data := n.Data
			if depth < 2 && looksLikeMarkup(data) {
				data = htmlText(data, depth+1)
			}
			b.WriteString(data)
			return

		case xhtml.ElementNode:
			switch n.Data {
			case "script", "style", "img", "video", "source", "iframe", "audio", "svg":
				return
			case "br":
				b.WriteString("\n")
				return
			}
			block := isBlockTag(n.Data)
			if block {
				b.WriteString("\n")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			if block {
				b.WriteString("\n")
			}
			return
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return b.String()
}

func looksLikeMarkup(s string) bool {
	return strings.Contains(s, "<img") ||
		strings.Contains(s, "<video") ||
		strings.Contains(s, "<p>") ||
		strings.Contains(s, "<br") ||
		strings.Contains(s, "</")
}

func isBlockTag(tag string) bool {
	switch tag {
	case "p", "div", "li", "ul", "ol", "h1", "h2", "h3", "h4", "h5", "h6",
		"blockquote", "tr", "table", "section", "article", "header", "footer":
		return true
	}
	return false
}

// normalizeText trims each line, collapses runs of spaces, and keeps at most
// one blank line between paragraphs.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	pendingBlank := false

	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if len(out) > 0 {
				pendingBlank = true
			}
			continue
		}
		if pendingBlank {
			out = append(out, "")
			pendingBlank = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// truncateText cuts at a rune boundary, preferring a line break near the end
// so a truncated paragraph does not end mid-word.
func truncateText(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	cut := string(runes[:limit])
	if idx := strings.LastIndex(cut, "\n"); idx > limit/2 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut) + "..."
}
