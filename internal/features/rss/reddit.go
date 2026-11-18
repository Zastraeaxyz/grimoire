package rss

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/bwmarrin/discordgo"
)

// Reddit retired the .rss listing endpoints. Its listing pages now require a
// signed-in session, so subreddit and user feeds are read from the HTML of
// old.reddit.com's newest-first listing. A browser identity is required: the
// bot's own User-Agent is rejected outright.
const (
	defaultRedditUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"
	redditWebBase          = "https://www.reddit.com"
	redditMinGap           = 2 * time.Second
	redditMaxListingBytes  = 5 << 20
	redditMaxPostBytes     = 2 << 20
)

// redditBase is the origin scraped for listings and post details. It is a var
// so tests can point the requests at a local server.
var redditBase = "https://old.reddit.com"

// errRedditSession means Reddit refused the request because there is no usable
// signed-in session. It is never retried differently; the operator has to set
// or refresh REDDIT_SESSION.
var errRedditSession = errors.New(
	"reddit needs a session cookie: set REDDIT_SESSION to the reddit_session value from a logged-in browser",
)

var redditHosts = map[string]struct{}{
	"reddit.com":     {},
	"www.reddit.com": {},
	"old.reddit.com": {},
	"np.reddit.com":  {},
	"m.reddit.com":   {},
}

// redditTarget is a subreddit, user, or search posts feed.
type redditTarget struct {
	Kind    string // "r", "u", or "search"
	Name    string // subreddit or username, lowercased
	FeedURL string // canonical URL stored in the database
	HomeURL string // human-facing page
	Title   string // "r/cats", "u/name", or "search: <query>"
}

// redditTarget resolves a stored feed URL to a scrape target. It accepts the
// historical .rss shapes as well as plain listing and search URLs, and rejects
// post permalinks.
func redditTargetFor(raw string) (redditTarget, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || !isRedditHost(u.Hostname()) {
		return redditTarget{}, false
	}
	return redditTargetFromURL(u)
}

func redditTargetFromURL(u *url.URL) (redditTarget, bool) {
	if u == nil || !isRedditHost(u.Hostname()) {
		return redditTarget{}, false
	}
	if target, ok := redditSearchTarget(u); ok {
		return target, true
	}
	if isRedditSearchPath(u.Path) {
		// A search path without a query is not a feed.
		return redditTarget{}, false
	}

	parts := pathSegments(u.Path)
	if len(parts) < 2 {
		return redditTarget{}, false
	}

	switch strings.ToLower(parts[0]) {
	case "r":
		name := strings.ToLower(strings.TrimSuffix(parts[1], ".rss"))
		if name == "" || !subredditListingOK(parts[2:]) {
			return redditTarget{}, false
		}
		return redditTarget{
			Kind:    "r",
			Name:    name,
			FeedURL: redditWebBase + "/r/" + name,
			HomeURL: redditWebBase + "/r/" + name,
			Title:   "r/" + name,
		}, true

	case "user", "u":
		name := strings.ToLower(strings.TrimSuffix(parts[1], ".rss"))
		if name == "" || !userListingOK(parts[2:]) {
			return redditTarget{}, false
		}
		return redditTarget{
			Kind:    "u",
			Name:    name,
			FeedURL: redditWebBase + "/user/" + name,
			HomeURL: redditWebBase + "/user/" + name,
			Title:   "u/" + name,
		}, true
	}
	return redditTarget{}, false
}

// redditSearchTarget canonicalizes a reddit search URL. A search feed follows
// the newest matches for the query, so any sort and page cursor in the URL is
// replaced by sort=new and t=all.
func redditSearchTarget(u *url.URL) (redditTarget, bool) {
	if u == nil || !isRedditSearchPath(u.Path) {
		return redditTarget{}, false
	}

	query := u.Query()
	q := strings.TrimSpace(query.Get("q"))
	if sub := subredditFromSearchPath(u.Path); sub != "" && !hasSubredditToken(q, sub) {
		q = strings.TrimSpace("subreddit:" + sub + " " + q)
	}
	if q == "" {
		return redditTarget{}, false
	}

	// Keep only parameters that change what is matched. Sort, time, and any
	// paging cursor belong to the feed's own newest-first fetch.
	params := url.Values{}
	params.Set("q", q)
	params.Set("sort", "new")
	params.Set("t", "all")
	for _, key := range []string{"include_over_18", "type"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			params.Set(key, value)
		}
	}

	canonical := &url.URL{
		Scheme:   "https",
		Host:     "old.reddit.com",
		Path:     "/search",
		RawQuery: params.Encode(),
	}
	return redditTarget{
		Kind:    "search",
		FeedURL: canonical.String(),
		HomeURL: redditWebBase,
		Title:   "search: " + q,
	}, true
}

// isRedditSearchPath reports whether a reddit path contains a search segment.
func isRedditSearchPath(path string) bool {
	for _, segment := range pathSegments(path) {
		if strings.EqualFold(segment, "search") {
			return true
		}
	}
	return false
}

// subredditFromSearchPath returns the subreddit in /r/{sub}/search.
func subredditFromSearchPath(path string) string {
	parts := pathSegments(path)
	if len(parts) >= 3 && strings.EqualFold(parts[0], "r") && strings.EqualFold(parts[2], "search") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// hasSubredditToken reports whether q already scopes to sub, so folding a
// /r/{sub}/search path does not duplicate the term.
func hasSubredditToken(q, sub string) bool {
	want := "subreddit:" + strings.ToLower(strings.TrimSpace(sub))
	for _, token := range strings.Fields(q) {
		if strings.EqualFold(token, want) {
			return true
		}
	}
	return false
}

// subredditListingOK reports whether the segments after /r/{sub} describe a
// posts listing. Sorting suffixes are accepted but ignored: a feed always
// follows the newest posts.
func subredditListingOK(rest []string) bool {
	switch len(rest) {
	case 0:
		return true
	case 1:
		switch strings.ToLower(rest[0]) {
		case ".rss", "new", "new.rss", "hot", "hot.rss", "top", "top.rss",
			"rising", "rising.rss", "controversial", "controversial.rss":
			return true
		}
	case 2:
		switch strings.ToLower(rest[0]) {
		case "new", "hot", "top", "rising", "controversial":
			if strings.EqualFold(rest[1], ".rss") {
				return true
			}
		}
	}
	return false
}

// userListingOK reports whether the segments after /user/{name} describe the
// submitted posts listing.
func userListingOK(rest []string) bool {
	switch len(rest) {
	case 0:
		return true
	case 1:
		switch strings.ToLower(rest[0]) {
		case ".rss", "submitted", "submitted.rss":
			return true
		}
	case 2:
		if strings.EqualFold(rest[0], "submitted") {
			switch strings.ToLower(rest[1]) {
			case ".rss", "new", "new.rss", "hot", "hot.rss", "top", "top.rss":
				return true
			}
		}
	}
	return false
}

func pathSegments(path string) []string {
	raw := strings.Split(strings.Trim(path, "/"), "/")
	out := raw[:0]
	for _, s := range raw {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isRedditHost(host string) bool {
	_, ok := redditHosts[strings.ToLower(host)]
	return ok
}

// isRedditOwned reports whether a URL points at reddit's own media or pages.
// A post's destination URL is only shown to the reader when it leaves reddit.
func isRedditOwned(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "reddit.com", "www.reddit.com", "old.reddit.com", "i.reddit.com",
		"i.redd.it", "v.redd.it", "preview.redd.it", "external-preview.redd.it",
		"redd.it":
		return true
	}
	return false
}

// redditListingURL builds the newest-first listing request for a target.
func redditListingURL(target redditTarget, includeNSFW bool) string {
	origin, err := url.Parse(redditBase)
	if err != nil || origin.Host == "" {
		origin = &url.URL{Scheme: "https", Host: "old.reddit.com"}
	}
	u := &url.URL{Scheme: origin.Scheme, Host: origin.Host}

	switch target.Kind {
	case "r":
		u.Path = "/r/" + target.Name + "/new/"
	case "u":
		u.Path = "/user/" + target.Name + "/submitted/"
		query := url.Values{}
		query.Set("sort", "new")
		if includeNSFW {
			query.Set("include_over_18", "on")
		}
		u.RawQuery = query.Encode()
	case "search":
		// A search URL is already canonical. The operator's config only
		// fills in include_over_18 when the URL did not decide it.
		canonical, err := url.Parse(target.FeedURL)
		if err != nil || canonical.Host == "" {
			return target.FeedURL
		}
		query := canonical.Query()
		if includeNSFW && query.Get("include_over_18") == "" {
			query.Set("include_over_18", "on")
		}
		u.Path = canonical.Path
		u.RawQuery = query.Encode()
		return u.String()
	}
	return u.String()
}

// redditPostURL is the JSON endpoint for one post. It powers the "Load full
// post" button and returns galleries, videos and full-size images.
func redditPostURL(postID string) string {
	return strings.TrimRight(redditBase, "/") + "/comments/" + url.PathEscape(postID) + ".json?raw_json=1"
}

// redditRequest performs one authenticated request with the configured
// browser identity and enforces a minimum gap between requests. Reddit counts
// every listing and post request against the same account budget.
func (f *Feature) redditRequest(ctx context.Context, endpoint, accept string, limit int64) ([]byte, error) {
	if f.redditSession == "" {
		return nil, errRedditSession
	}
	if err := f.waitForReddit(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building reddit request: %w", err)
	}
	req.Header.Set("User-Agent", f.redditUserAgent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", f.redditCookie())

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching reddit: %w", err)
	}
	defer resp.Body.Close()

	if resp.Request != nil && resp.Request.URL != nil &&
		strings.HasPrefix(strings.ToLower(resp.Request.URL.Path), "/login") {
		return nil, errRedditSession
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, errRedditSession
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("reddit rate limited the request; it will try again later")
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, redditNotFound(resp, endpoint)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("reddit returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading reddit response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("reddit response is larger than %d bytes", limit)
	}
	if isRedditLoginWall(body) {
		return nil, errRedditSession
	}
	return body, nil
}

// redditCookie builds the Cookie header. A bare cookie value and a whole
// "reddit_session=..." header are both accepted, so the operator can paste
// whichever the browser gives them.
func (f *Feature) redditCookie() string {
	cookie := normalizeRedditCookie(f.redditSession)
	if f.redditIncludeNSFW && !strings.Contains(strings.ToLower(cookie), "over18=") {
		cookie += "; over18=1"
	}
	return cookie
}

func normalizeRedditCookie(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(raw), "reddit_session=") {
		return raw
	}
	return "reddit_session=" + raw
}

// waitForReddit blocks until the minimum gap since the previous reddit
// request has elapsed. The mutex serializes every reddit call.
func (f *Feature) waitForReddit(ctx context.Context) error {
	f.redditMu.Lock()
	defer f.redditMu.Unlock()

	if !f.redditLastRequest.IsZero() {
		if wait := redditMinGap - time.Since(f.redditLastRequest); wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	f.redditLastRequest = time.Now()
	return nil
}

// redditNotFound turns a missing listing into a readable reason when reddit
// says the community or profile was banned, deleted or suspended.
func redditNotFound(resp *http.Response, endpoint string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	text := strings.ToLower(string(body))
	for _, state := range []string{"banned", "deleted", "suspended"} {
		if strings.Contains(text, "has been "+state) {
			return fmt.Errorf("reddit reports that this feed has been %s", state)
		}
	}
	return fmt.Errorf("reddit returned HTTP %d for %s", resp.StatusCode, endpoint)
}

// isRedditLoginWall recognizes the sign-in interstitial reddit serves to a
// missing or expired session, as well as its block page.
func isRedditLoginWall(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, `id="login-form"`) ||
		strings.Contains(s, `id="login"`) ||
		strings.Contains(s, "you must be logged in") ||
		strings.Contains(s, "whoa there, pardner") ||
		strings.Contains(s, "<title>welcome to reddit</title>") ||
		strings.Contains(s, "<title>blocked</title>")
}

// warnMissingRedditSession tells the operator once at startup that existing
// reddit feeds cannot work until a cookie is configured.
func (f *Feature) warnMissingRedditSession() {
	if f.redditSession != "" || f.store == nil {
		return
	}
	feeds, err := f.store.ListFeeds(f.ctx, f.guildID)
	if err != nil {
		return
	}
	for _, feed := range feeds {
		if _, ok := redditTargetFor(feed.URL); ok {
			f.log.Warn("reddit feed needs a session cookie; set REDDIT_SESSION",
				"feed_id", feed.ID,
				"url", feed.URL,
			)
		}
	}
}

// fetchReddit loads a subreddit, user, or search listing and normalizes it
// into the same shape as an RSS document.
func (f *Feature) fetchReddit(ctx context.Context, target redditTarget) (*parsedFeed, error) {
	endpoint := redditListingURL(target, f.redditIncludeNSFW)
	body, err := f.redditRequest(ctx, endpoint, "text/html,application/xhtml+xml", redditMaxListingBytes)
	if err != nil {
		return nil, err
	}

	parse := parseRedditListing
	if target.Kind == "search" {
		parse = parseRedditSearch
	}
	feed, err := parse(body)
	if err != nil {
		return nil, err
	}
	if len(feed.Items) == 0 && isRedditAgeGate(body) {
		return nil, errors.New("this reddit feed is age-restricted: set REDDIT_INCLUDE_NSFW=true to follow it")
	}
	feed.Title = target.Title
	feed.SiteURL = target.HomeURL
	return feed, nil
}

// isRedditAgeGate recognizes the consent page reddit serves for NSFW
// communities when the over18 cookie is absent.
func isRedditAgeGate(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "over18-gate") ||
		strings.Contains(s, "<title>over 18</title>") ||
		strings.Contains(s, "are you over 18")
}

// parseRedditListing extracts the posts from an old.reddit listing page in
// page order. Only t3_ post blocks are kept.
func parseRedditListing(body []byte) (*parsedFeed, error) {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parsing reddit listing: %w", err)
	}

	feed := &parsedFeed{}
	seen := make(map[string]struct{})
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "div" && hasClassAttr(n, "thing") {
			if item, ok := redditListingItem(n); ok {
				if _, exists := seen[item.GUID]; !exists {
					seen[item.GUID] = struct{}{}
					feed.Items = append(feed.Items, item)
				}
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return feed, nil
}

// redditListingItem builds one feed item from a div.thing block.
func redditListingItem(n *xhtml.Node) (FeedItem, bool) {
	fullname := attrByName(n, "data-fullname")
	if !strings.HasPrefix(fullname, "t3_") {
		return FeedItem{}, false
	}
	id := strings.TrimSpace(strings.TrimPrefix(fullname, "t3_"))
	if id == "" {
		return FeedItem{}, false
	}

	title := ""
	if anchor := firstMatching(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "a" && hasClassAttr(x, "title")
	}); anchor != nil {
		title = strings.TrimSpace(textContent(anchor))
	}

	link := redditPermalink(attrByName(n, "data-permalink"))
	if link == "" {
		link = redditWebBase + "/comments/" + id
	}

	dest := strings.TrimSpace(html.UnescapeString(attrByName(n, "data-url")))
	if dest == link || strings.EqualFold(attrByName(n, "data-type"), "self") {
		dest = ""
	}

	author := strings.TrimSpace(attrByName(n, "data-author"))

	var bodyHTML string
	if body := firstMatching(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "div" && hasClassAttr(x, "usertext-body")
	}); body != nil {
		bodyHTML = strings.TrimSpace(innerHTMLOf(body))
	}

	content := bodyHTML
	if dest != "" && !isRedditOwned(dest) {
		content = strings.TrimSpace(content + "\n<p>" + html.EscapeString(dest) + "</p>")
	}

	gallery := isRedditGallery(dest)
	thumbnail := redditThumbnail(n)
	var declared []Media
	switch {
	case isImageDestination(dest):
		declared = append(declared, Media{URL: dest})
	case gallery:
		if cover := redditGalleryCover(thumbnail); cover != "" {
			declared = append(declared, Media{URL: cover})
		} else if thumbnail != "" {
			declared = append(declared, Media{URL: thumbnail})
		}
	case thumbnail != "":
		// Videos and external link posts only expose a preview image in the
		// listing. The button loads the real media on demand.
		declared = append(declared, Media{URL: thumbnail})
	}

	item := FeedItem{
		GUID:      fullname,
		Title:     title,
		Link:      link,
		Author:    author,
		Content:   content,
		Published: redditPublishedAt(n),
		Reddit:    true,
		Gallery:   gallery,
	}
	item.Media = collectMedia(item, declared)
	return item, true
}

// parseRedditSearch extracts the post results from an old.reddit search page
// in page order. Search results share the t3_ identity and the post fields of
// a listing, but use their own markup.
func parseRedditSearch(body []byte) (*parsedFeed, error) {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parsing reddit search: %w", err)
	}

	feed := &parsedFeed{}
	seen := make(map[string]struct{})
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "div" && hasClassAttr(n, "search-result-link") {
			if item, ok := redditSearchItem(n); ok {
				if _, exists := seen[item.GUID]; !exists {
					seen[item.GUID] = struct{}{}
					feed.Items = append(feed.Items, item)
				}
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return feed, nil
}

// redditSearchItem builds one item from a search-result-link block.
func redditSearchItem(n *xhtml.Node) (FeedItem, bool) {
	fullname := firstAttrByName(n, "data-fullname")
	if !strings.HasPrefix(fullname, "t3_") {
		return FeedItem{}, false
	}
	id := strings.TrimSpace(strings.TrimPrefix(fullname, "t3_"))
	if id == "" {
		return FeedItem{}, false
	}

	title := ""
	var titleHref string
	if anchor := firstMatching(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "a" && hasClassAttr(x, "search-title")
	}); anchor != nil {
		title = strings.TrimSpace(textContent(anchor))
		titleHref = attrByName(anchor, "href")
	}

	link := redditPermalink(firstAttrByName(n, "data-permalink"))
	if link == "" {
		link = redditPermalink(titleHref)
	}
	if link == "" {
		link = redditWebBase + "/comments/" + id
	}

	dest := strings.TrimSpace(html.UnescapeString(firstAttrByName(n, "data-url")))
	if dest == "" || dest == link {
		dest = redditSearchDestination(n)
	}
	if dest == link {
		dest = ""
	}

	author := ""
	if span := firstMatching(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "span" && hasClassAttr(x, "search-author")
	}); span != nil {
		if anchor := firstMatching(span, func(x *xhtml.Node) bool {
			return x.Type == xhtml.ElementNode && x.Data == "a"
		}); anchor != nil {
			author = strings.TrimSpace(textContent(anchor))
		}
	}
	if author == "" {
		author = strings.TrimSpace(firstAttrByName(n, "data-author"))
	}

	content := ""
	if dest != "" && !isRedditOwned(dest) {
		content = "<p>" + html.EscapeString(dest) + "</p>"
	}

	gallery := isRedditGallery(dest)
	thumbnail := redditSearchThumbnail(n)
	var declared []Media
	switch {
	case isImageDestination(dest):
		declared = append(declared, Media{URL: dest})
	case gallery:
		if cover := redditGalleryCover(thumbnail); cover != "" {
			declared = append(declared, Media{URL: cover})
		} else if thumbnail != "" {
			declared = append(declared, Media{URL: thumbnail})
		}
	case thumbnail != "":
		declared = append(declared, Media{URL: thumbnail})
	}

	item := FeedItem{
		GUID:      fullname,
		Title:     title,
		Link:      link,
		Author:    author,
		Content:   content,
		Published: redditPublishedAt(n),
		Reddit:    true,
		Gallery:   gallery,
	}
	item.Media = collectMedia(item, declared)
	return item, true
}

// redditSearchDestination returns the external destination linked from a
// result's footer. Self posts link back to their own permalink, which is not
// useful as a destination.
func redditSearchDestination(n *xhtml.Node) string {
	footer := firstMatching(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "div" && hasClassAttr(x, "search-result-footer")
	})
	if footer == nil {
		return ""
	}
	anchor := firstMatching(footer, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "a"
	})
	if anchor == nil {
		return ""
	}
	raw := strings.TrimSpace(html.UnescapeString(attrByName(anchor, "href")))
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

// redditSearchThumbnail returns the first reddit preview image in a result,
// which gives galleries and videos a cover in the listing post.
func redditSearchThumbnail(n *xhtml.Node) string {
	var found string
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if found != "" {
			return
		}
		if x.Type == xhtml.ElementNode && x.Data == "img" {
			raw := strings.TrimSpace(html.UnescapeString(attrByName(x, "src")))
			if strings.HasPrefix(raw, "//") {
				raw = "https:" + raw
			}
			if u, err := url.Parse(raw); err == nil {
				switch strings.ToLower(u.Hostname()) {
				case "preview.redd.it", "external-preview.redd.it":
					found = u.String()
					return
				}
			}
		}
		for child := x.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

// redditPublishedAt reads the post time from the tagline's <time> element,
// falling back to data-timestamp. Reddit has used both seconds and
// milliseconds there, so the magnitude decides.
func redditPublishedAt(n *xhtml.Node) time.Time {
	if t := firstMatching(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "time"
	}); t != nil {
		if raw := strings.TrimSpace(attrByName(t, "datetime")); raw != "" {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				return parsed
			}
		}
	}

	raw := strings.TrimSpace(firstAttrByName(n, "data-timestamp"))
	if raw == "" {
		return time.Time{}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return time.Time{}
	}
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

// redditPermalink normalizes a post permalink, absolute or relative, onto the
// www host. Query strings and fragments are dropped.
func redditPermalink(raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Host == "" {
		if !strings.Contains(u.Path, "/comments/") {
			return ""
		}
		return redditWebBase + u.Path
	}
	if !isRedditHost(u.Hostname()) || !strings.Contains(u.Path, "/comments/") {
		return ""
	}
	return redditWebBase + u.Path
}

// redditThumbnail returns reddit's preview image for a listing post. Only
// reddit's preview hosts qualify: the listing also carries site icons and
// "self"/"default" placeholders that are not content.
func redditThumbnail(n *xhtml.Node) string {
	raw := ""
	if anchor := firstMatching(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "a" && hasClassAttr(x, "thumbnail")
	}); anchor != nil {
		if img := firstMatching(anchor, func(x *xhtml.Node) bool {
			return x.Type == xhtml.ElementNode && x.Data == "img"
		}); img != nil {
			raw = attrByName(img, "src")
		}
	}
	if raw == "" {
		raw = attrByName(n, "data-thumbnail")
	}

	raw = strings.TrimSpace(html.UnescapeString(raw))
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "preview.redd.it", "external-preview.redd.it":
		return u.String()
	}
	return ""
}

// redditGalleryCover derives the full-size first image of a gallery from its
// square cover thumbnail: preview.redd.it/<id>.<ext> -> i.redd.it/<id>.<ext>.
func redditGalleryCover(thumbnail string) string {
	u, err := url.Parse(thumbnail)
	if err != nil || !strings.EqualFold(u.Hostname(), "preview.redd.it") {
		return ""
	}
	parts := pathSegments(u.Path)
	if len(parts) != 1 {
		return ""
	}
	name := parts[0]
	dot := strings.LastIndex(name, ".")
	if dot <= 0 || dot == len(name)-1 {
		return ""
	}
	return "https://i.redd.it/" + name
}

func isRedditGallery(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !isRedditHost(u.Hostname()) {
		return false
	}
	parts := pathSegments(u.Path)
	return len(parts) >= 1 && strings.EqualFold(parts[0], "gallery")
}

// isImageDestination reports whether a post's destination is a direct image.
func isImageDestination(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "i.redd.it", "i.imgur.com":
		return true
	}
	switch strings.ToLower(pathExt(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp":
		return true
	}
	return false
}

func pathExt(path string) string {
	if dot := strings.LastIndex(path, "."); dot >= 0 {
		return path[dot:]
	}
	return ""
}

// redditPostIDFromGUID returns the post id in a t3_<id> fullname.
func redditPostIDFromGUID(guid string) string {
	if !strings.HasPrefix(guid, "t3_") {
		return ""
	}
	return strings.TrimPrefix(guid, "t3_")
}

// redditLoadButton renders the per-post component. The custom ID carries only
// the post id, so buttons keep working across restarts. Any member may press
// it; the component router does not require admin rights.
func redditLoadButton(item FeedItem) []discordgo.MessageComponent {
	id := redditPostIDFromGUID(item.GUID)
	if id == "" {
		return nil
	}
	label := "Load full post"
	if item.Gallery {
		label = "Load gallery"
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    label,
				Style:    discordgo.SecondaryButton,
				CustomID: "reddit:load:" + id,
			},
		}},
	}
}

// redditPostData is the subset of a post's JSON representation the bot needs.
type redditPostData struct {
	ID                    string  `json:"id"`
	Title                 string  `json:"title"`
	SelfTextHTML          string  `json:"selftext_html"`
	URL                   string  `json:"url"`
	Permalink             string  `json:"permalink"`
	Author                string  `json:"author"`
	SubredditNamePrefixed string  `json:"subreddit_name_prefixed"`
	CreatedUTC            float64 `json:"created_utc"`
	IsGallery             bool    `json:"is_gallery"`

	GalleryData struct {
		Items []struct {
			MediaID string `json:"media_id"`
		} `json:"items"`
	} `json:"gallery_data"`

	MediaMetadata map[string]struct {
		Status string `json:"status"`
		E      string `json:"e"`
		M      string `json:"m"`
		S      struct {
			U   string `json:"u"`
			GIF string `json:"gif"`
			MP4 string `json:"mp4"`
		} `json:"s"`
	} `json:"media_metadata"`

	SecureMedia *redditMedia `json:"secure_media"`
	Media       *redditMedia `json:"media"`

	Preview struct {
		Images []struct {
			Source struct {
				URL string `json:"url"`
			} `json:"source"`
		} `json:"images"`
	} `json:"preview"`
}

type redditMedia struct {
	RedditVideo *struct {
		FallbackURL string `json:"fallback_url"`
		HLSURL      string `json:"hls_url"`
	} `json:"reddit_video"`
}

type redditPostEnvelope []struct {
	Data struct {
		Children []struct {
			Data redditPostData `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// fetchRedditPost loads one post's full detail. It is only called from the
// "Load full post" button, because it costs an extra request per post.
func (f *Feature) fetchRedditPost(ctx context.Context, postID string) (FeedItem, error) {
	body, err := f.redditRequest(ctx, redditPostURL(postID), "application/json", redditMaxPostBytes)
	if err != nil {
		return FeedItem{}, err
	}
	return parseRedditPost(body)
}

// parseRedditPost turns reddit's post JSON into an item with every gallery
// image and the video's MP4, so the caller can post the complete media set.
func parseRedditPost(body []byte) (FeedItem, error) {
	var envelope redditPostEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return FeedItem{}, fmt.Errorf("reading reddit post: %w", err)
	}
	if len(envelope) == 0 || len(envelope[0].Data.Children) == 0 {
		return FeedItem{}, errors.New("reddit returned no post data")
	}
	data := envelope[0].Data.Children[0].Data

	link := redditPermalink(data.Permalink)
	if link == "" && data.ID != "" {
		link = redditWebBase + "/comments/" + data.ID
	}

	content := strings.TrimSpace(stdHTMLCommentStrip(data.SelfTextHTML))
	dest := strings.TrimSpace(html.UnescapeString(data.URL))
	if dest != "" && !isRedditOwned(dest) {
		content = strings.TrimSpace(content + "\n<p>" + html.EscapeString(dest) + "</p>")
	}

	isGallery := data.IsGallery || len(data.GalleryData.Items) > 0
	item := FeedItem{
		Title:   strings.TrimSpace(data.Title),
		Link:    link,
		Author:  strings.TrimSpace(data.Author),
		Content: content,
		Reddit:  true,
		Gallery: isGallery,
	}
	if data.CreatedUTC > 0 {
		item.Published = time.Unix(int64(data.CreatedUTC), 0).UTC()
	}

	var declared []Media
	switch {
	case isGallery:
		declared = append(declared, redditGalleryMedia(data)...)
	default:
		if video := redditVideoURL(data); video != "" {
			declared = append(declared, Media{URL: video, Type: "video/mp4"})
		} else if isImageDestination(dest) {
			declared = append(declared, Media{URL: dest})
		}
	}
	if len(declared) == 0 {
		if preview := redditPreviewImage(data); preview != "" {
			declared = append(declared, Media{URL: preview})
		}
	}
	item.Media = dedupeMedia(item.Link, declared)
	return item, nil
}

func redditGalleryMedia(data redditPostData) []Media {
	ids := make([]string, 0, len(data.GalleryData.Items))
	for _, entry := range data.GalleryData.Items {
		if entry.MediaID != "" {
			ids = append(ids, entry.MediaID)
		}
	}
	if len(ids) == 0 {
		// gallery_data should be present, but media_metadata alone is better
		// than nothing. Sort the keys so the order is stable.
		for id := range data.MediaMetadata {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}

	var media []Media
	for _, id := range ids {
		entry, ok := data.MediaMetadata[id]
		if !ok || strings.EqualFold(entry.Status, "failed") {
			continue
		}
		raw := entry.S.U
		if entry.S.MP4 != "" && (entry.E == "AnimatedImage" || entry.M == "image/gif") {
			raw = entry.S.MP4
		}
		raw = strings.TrimSpace(html.UnescapeString(raw))
		if raw == "" {
			continue
		}
		mime := entry.M
		if mime == "" && entry.S.MP4 != "" && raw == entry.S.MP4 {
			mime = "video/mp4"
		}
		media = append(media, Media{URL: raw, Type: mime})
	}
	return media
}

func redditVideoURL(data redditPostData) string {
	for _, media := range []*redditMedia{data.SecureMedia, data.Media} {
		if media == nil || media.RedditVideo == nil {
			continue
		}
		if raw := strings.TrimSpace(html.UnescapeString(media.RedditVideo.FallbackURL)); raw != "" {
			return raw
		}
	}
	return ""
}

func redditPreviewImage(data redditPostData) string {
	for _, image := range data.Preview.Images {
		if raw := strings.TrimSpace(html.UnescapeString(image.Source.URL)); raw != "" {
			return raw
		}
	}
	return ""
}

// stdHTMLCommentStrip removes the SC_OFF/SC_ON markers reddit wraps around
// self text HTML.
func stdHTMLCommentStrip(raw string) string {
	raw = strings.ReplaceAll(raw, "<!-- SC_OFF -->", "")
	raw = strings.ReplaceAll(raw, "<!-- SC_ON -->", "")
	return raw
}

// --- small DOM helpers ---

func hasClassAttr(n *xhtml.Node, want string) bool {
	for _, attr := range n.Attr {
		if !strings.EqualFold(attr.Key, "class") {
			continue
		}
		for _, class := range strings.Fields(attr.Val) {
			if class == want {
				return true
			}
		}
	}
	return false
}

// firstAttrByName returns the first occurrence of an attribute on n or one of
// its descendants. Search result wrappers keep their data-* attributes on a
// nested element rather than on the wrapper itself.
func firstAttrByName(n *xhtml.Node, key string) string {
	if value := attrByName(n, key); value != "" {
		return value
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if value := firstAttrByName(child, key); value != "" {
			return value
		}
	}
	return ""
}

func attrByName(n *xhtml.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func firstMatching(n *xhtml.Node, pred func(*xhtml.Node) bool) *xhtml.Node {
	if pred(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := firstMatching(child, pred); found != nil {
			return found
		}
	}
	return nil
}

func textContent(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.TextNode {
			b.WriteString(x.Data)
		}
		for child := x.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func innerHTMLOf(n *xhtml.Node) string {
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		_ = xhtml.Render(&b, child)
	}
	return b.String()
}
