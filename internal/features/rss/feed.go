package rss

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"net/url"
	"path"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

// Media is one image or video attached to a feed item.
type Media struct {
	URL  string
	Type string
}

// FeedItem is a normalized entry from an RSS or Atom feed.
type FeedItem struct {
	GUID      string
	Title     string
	Link      string
	Author    string
	Content   string
	Summary   string
	Published time.Time
	Media     []Media

	// Reddit marks a post read from a reddit listing or post detail. Gallery
	// marks a reddit gallery, which only shows its cover until the reader
	// asks for the full post.
	Reddit  bool
	Gallery bool
}

// parsedFeed is the result of decoding a document.
type parsedFeed struct {
	Title   string
	SiteURL string
	Items   []FeedItem
}

// parseFeed decodes an RSS 2.0 or Atom document.
func parseFeed(data []byte) (*parsedFeed, error) {
	root, err := rootElement(data)
	if err != nil {
		return nil, err
	}

	switch root {
	case "rss":
		return parseRSS(data)
	case "feed":
		return parseAtom(data)
	default:
		return nil, fmt.Errorf("unsupported feed root element <%s>", root)
	}
}

func rootElement(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("reading feed: %w", err)
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name.Local, nil
		}
	}
}

// rssDocument covers the subset of RSS 2.0 the bot needs.
type rssDocument struct {
	Channel struct {
		Title string           `xml:"title"`
		Links []rssChannelLink `xml:"link"`
		Items []rssItem        `xml:"item"`
	} `xml:"channel"`
}

// rssChannelLink captures both the plain <link> and namespaced siblings such
// as atom:link. A scalar field is skipped when namespaced links are present.
type rssChannelLink struct {
	Href  string `xml:"href,attr"`
	Rel   string `xml:"rel,attr"`
	Value string `xml:",chardata"`
}

type rssItem struct {
	Title       string     `xml:"title"`
	Link        string     `xml:"link"`
	GUID        rssGUID    `xml:"guid"`
	Description string     `xml:"description"`
	Encoded     string     `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	PubDate     string     `xml:"pubDate"`
	Creator     string     `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Enclosure   rssMedia   `xml:"enclosure"`
	Media       []rssMedia `xml:"http://search.yahoo.com/mrss/ content"`
	Thumbnail   []rssMedia `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink string `xml:"isPermaLink,attr"`
}

type rssMedia struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Medium string `xml:"medium,attr"`
}

func parseRSS(data []byte) (*parsedFeed, error) {
	var doc rssDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decoding RSS: %w", err)
	}

	feed := &parsedFeed{
		Title:   cleanTitle(doc.Channel.Title),
		SiteURL: rssSiteURL(doc.Channel.Links),
	}
	for _, raw := range doc.Channel.Items {
		item := FeedItem{
			Title:     raw.Title,
			Link:      strings.TrimSpace(raw.Link),
			Author:    strings.TrimSpace(raw.Creator),
			Content:   raw.Encoded,
			Summary:   raw.Description,
			Published: parseTime(raw.PubDate),
		}
		if item.Content == "" {
			item.Content = raw.Description
		}

		guid := strings.TrimSpace(raw.GUID.Value)
		if guid == "" {
			guid = item.Link
		}
		// Some feeds only expose non-permalink IDs; the GUID is still a
		// stable dedupe key, so prefer it over the link when present.
		if guid == "" {
			continue
		}
		item.GUID = guid
		if item.Link == "" && raw.GUID.IsPermaLink != "false" {
			item.Link = guid
		}

		var media []Media
		for _, m := range raw.Media {
			media = append(media, Media{URL: m.URL, Type: m.Type})
		}
		for _, m := range raw.Thumbnail {
			media = append(media, Media{URL: m.URL, Type: m.Type})
		}
		if raw.Enclosure.URL != "" {
			media = append(media, Media{URL: raw.Enclosure.URL, Type: raw.Enclosure.Type})
		}
		item.Media = collectMedia(item, media)
		feed.Items = append(feed.Items, item)
	}
	return feed, nil
}

// Atom document types. Each element is fully namespaced because Atom
// documents do not use a default namespace for their children.

type atomDocument struct {
	Title   atomText    `xml:"http://www.w3.org/2005/Atom title"`
	Links   []atomLink  `xml:"http://www.w3.org/2005/Atom link"`
	Entries []atomEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

type atomEntry struct {
	ID        string     `xml:"http://www.w3.org/2005/Atom id"`
	Title     atomText   `xml:"http://www.w3.org/2005/Atom title"`
	Links     []atomLink `xml:"http://www.w3.org/2005/Atom link"`
	Published string     `xml:"http://www.w3.org/2005/Atom published"`
	Updated   string     `xml:"http://www.w3.org/2005/Atom updated"`
	Summary   atomText   `xml:"http://www.w3.org/2005/Atom summary"`
	Content   atomText   `xml:"http://www.w3.org/2005/Atom content"`
	Author    struct {
		Name atomText `xml:"http://www.w3.org/2005/Atom name"`
	} `xml:"http://www.w3.org/2005/Atom author"`
	Media     []rssMedia `xml:"http://search.yahoo.com/mrss/ content"`
	Thumbnail []rssMedia `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr"`
}

type atomText struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
	Inner string `xml:",innerxml"`
}

// rssSiteURL picks the channel's plain link, which points at the blog rather
// than the feed. Namespaced atom:link elements are ignored unless no plain
// link exists.
func rssSiteURL(links []rssChannelLink) string {
	for _, link := range links {
		if link.Href == "" && strings.TrimSpace(link.Value) != "" {
			return strings.TrimSpace(link.Value)
		}
	}
	for _, link := range links {
		if link.Rel == "alternate" && link.Href != "" {
			return strings.TrimSpace(link.Href)
		}
	}
	return ""
}

// atomSiteURL picks the feed's public HTML page, not its feed URL.
func atomSiteURL(links []atomLink) string {
	for _, link := range links {
		if (link.Rel == "" || link.Rel == "alternate") && link.Href != "" {
			return strings.TrimSpace(link.Href)
		}
	}
	for _, link := range links {
		if link.Rel == "self" && link.Href != "" {
			return strings.TrimSpace(link.Href)
		}
	}
	return ""
}

// feedDomain is the site a feed belongs to, for example bdsmlr.com even when
// the feed itself is served from a mirror.
func feedDomain(feed *parsedFeed, feedURL string) string {
	for _, candidate := range []string{feed.SiteURL, feedURL} {
		if candidate == "" {
			continue
		}
		u, err := url.Parse(candidate)
		if err != nil || u.Host == "" {
			continue
		}
		return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	}
	return ""
}

func (t atomText) text() string {
	if strings.TrimSpace(t.Value) != "" {
		return t.Value
	}
	return t.Inner
}

func parseAtom(data []byte) (*parsedFeed, error) {
	var doc atomDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decoding Atom: %w", err)
	}

	feed := &parsedFeed{
		Title:   cleanTitle(doc.Title.text()),
		SiteURL: atomSiteURL(doc.Links),
	}
	for _, raw := range doc.Entries {
		item := FeedItem{
			Title:     raw.Title.text(),
			Author:    strings.TrimSpace(raw.Author.Name.text()),
			Content:   raw.Content.text(),
			Summary:   raw.Summary.text(),
			Published: parseTime(raw.Published),
		}
		if item.Content == "" {
			item.Content = item.Summary
		}

		for _, link := range raw.Links {
			if item.Link == "" && (link.Rel == "" || link.Rel == "alternate") {
				item.Link = strings.TrimSpace(link.Href)
			}
		}
		if item.Link == "" {
			for _, link := range raw.Links {
				if link.Rel == "related" || link.Rel == "self" {
					continue
				}
				item.Link = strings.TrimSpace(link.Href)
				break
			}
		}
		if item.Published.IsZero() {
			item.Published = parseTime(raw.Updated)
		}

		guid := strings.TrimSpace(raw.ID)
		if guid == "" {
			guid = item.Link
		}
		if guid == "" {
			continue
		}
		item.GUID = guid

		var media []Media
		for _, m := range raw.Media {
			media = append(media, Media{URL: m.URL, Type: m.Type})
		}
		for _, m := range raw.Thumbnail {
			media = append(media, Media{URL: m.URL, Type: m.Type})
		}
		for _, link := range raw.Links {
			if link.Rel == "enclosure" && link.Href != "" {
				media = append(media, Media{URL: link.Href, Type: link.Type})
			}
		}
		item.Media = collectMedia(item, media)
		feed.Items = append(feed.Items, item)
	}
	return feed, nil
}

// collectMedia merges declared enclosures with media embedded in the post
// body, then removes duplicates and truncated variants.
func collectMedia(item FeedItem, declared []Media) []Media {
	var all []Media

	// Media inside the body keeps the author's order, which matters for
	// multi-image posts.
	bodies := []string{item.Content}
	if item.Summary != item.Content {
		bodies = append(bodies, item.Summary)
	}
	for _, body := range bodies {
		for _, url := range mediaFromHTML(body, 0) {
			all = append(all, Media{URL: url})
		}
	}

	for _, m := range declared {
		if m.URL == "" {
			continue
		}
		all = append(all, Media{URL: m.URL, Type: m.Type})
	}

	return dedupeMedia(item.Link, all)
}

// dedupeMedia drops exact duplicates and picks the longer URL when one is a
// truncated prefix of another. Imaglr's video enclosures are cut short and
// 404, while the full URL in content:encoded works. The URL itself keeps its
// query string because bdsmlr CDN links are signed.
func dedupeMedia(baseURL string, in []Media) []Media {
	type entry struct {
		media Media
		key   string
		stem  string
	}

	out := make([]entry, 0, len(in))
	for _, m := range in {
		key := normalizeMediaURL(baseURL, m.URL)
		if key == "" {
			continue
		}
		stem := mediaStem(key)

		skip := false
		for i, kept := range out {
			switch {
			case key == kept.key:
				skip = true
			case stem != "" && kept.stem != "" && strings.HasPrefix(stem, kept.stem):
				out[i] = entry{media: m, key: key, stem: stem}
				skip = true
			case stem != "" && kept.stem != "" && strings.HasPrefix(kept.stem, stem):
				skip = true
			}
			if skip {
				break
			}
		}
		if !skip {
			out = append(out, entry{media: m, key: key, stem: stem})
		}
	}

	media := make([]Media, len(out))
	for i, e := range out {
		media[i] = e.media
	}
	return media
}

// mediaStem strips the file extension so a truncated filename can be matched
// against the full one. Short names are left alone to avoid treating two
// unrelated files as the same.
func mediaStem(key string) string {
	u, err := url.Parse(key)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	ext := path.Ext(base)
	if len(strings.TrimSuffix(base, ext)) < 8 {
		return ""
	}
	if ext == "" {
		return key
	}
	u.Path = strings.TrimSuffix(u.Path, ext)
	return u.String()
}

// normalizeMediaURL makes a src absolute and strips the query, which for
// signed CDN links contains a per-request signature rather than identity.
func normalizeMediaURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else if strings.HasPrefix(raw, "/") && baseURL != "" {
		if base, err := url.Parse(baseURL); err == nil {
			raw = base.Scheme + "://" + base.Host + raw
		}
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// mediaFromHTML returns image and video sources in document order. Nested
// escaped HTML, as bdsmlr produces for reblogs, is handled with one retry.
func mediaFromHTML(raw string, depth int) []string {
	if strings.TrimSpace(raw) == "" || depth > 2 {
		return nil
	}

	doc, err := xhtml.Parse(strings.NewReader(raw))
	if err != nil {
		return nil
	}

	var urls []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "img", "source", "video":
				src := attr(n, "src")
				if src == "" {
					src = attr(n, "data-src")
				}
				if src != "" {
					urls = append(urls, src)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(urls) == 0 {
		// Escaped markup shows up as text, not elements.
		text := html.UnescapeString(raw)
		if text != raw && (strings.Contains(text, "<img") || strings.Contains(text, "<video") || strings.Contains(text, "<source")) {
			return mediaFromHTML(text, depth+1)
		}
	}
	return urls
}

func attr(n *xhtml.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

// cleanTitle drops machine-generated titles such as bdsmlr's "Post 123456"
// and imaglr's "user - image".
func cleanTitle(raw string) string {
	title := strings.TrimSpace(htmlToText(raw))
	if title == "" {
		return ""
	}
	lower := strings.ToLower(title)
	if strings.HasPrefix(lower, "post ") && isDigits(strings.TrimPrefix(lower, "post ")) {
		return ""
	}
	for _, suffix := range []string{" - image", " - video", " - text", " - audio"} {
		if strings.HasSuffix(lower, suffix) {
			return ""
		}
	}
	return title
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseTime accepts the date formats seen in RSS and Atom feeds.
func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
