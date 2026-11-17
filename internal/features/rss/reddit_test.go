package rss

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// redditListingFixture mirrors old.reddit's /new listing markup: each post is
// a div.thing with data-* attributes, a title anchor, a tagline time, and an
// optional usertext-body for self posts.
const redditListingFixture = `<!doctype html><html><body>
<div class=" thing id-t3_aaa odd link " data-fullname="t3_aaa" data-type="link" data-author="alice" data-subreddit="golang" data-permalink="/r/golang/comments/aaa/some_title/" data-url="https://example.com/article">
  <a class="thumbnail may-blank" href="https://example.com/article"><img src="//external-preview.redd.it/example123.jpeg?width=140&amp;height=140&amp;crop=1:1,smart&amp;s=sig"></a>
  <div class="entry">
    <p class="title"><a class="title may-blank" href="https://example.com/article">Some &amp; Title</a></p>
    <p class="tagline">submitted <time datetime="2026-11-16T08:46:37+00:00">then</time> by <a class="author" href="/user/alice">alice</a></p>
  </div>
</div>
<div class=" thing id-t3_bbb even link self" data-fullname="t3_bbb" data-type="self" data-author="bob" data-subreddit="lounge" data-permalink="/r/lounge/comments/bbb/hello/" data-url="https://www.reddit.com/r/lounge/comments/bbb/hello/" data-timestamp="1700000001000">
  <div class="entry">
    <p class="title"><a class="title" href="https://www.reddit.com/r/lounge/comments/bbb/hello/">Hello</a></p>
    <div class="expando"><div class="usertext-body may-blank-within md-container"><div class="md"><p>Self text</p></div></div></div>
  </div>
</div>
<div class=" thing id-t3_img odd link" data-fullname="t3_img" data-author="carol" data-subreddit="pics" data-permalink="/r/pics/comments/img/pic/" data-url="https://i.redd.it/pic1.jpeg" data-timestamp="1700000002">
  <div class="entry"><p class="title"><a class="title" href="https://i.redd.it/pic1.jpeg">Pic</a></p></div>
</div>
<div class=" thing id-t3_gal odd link" data-fullname="t3_gal" data-author="dave" data-subreddit="pics" data-permalink="/r/pics/comments/gal/gallery/" data-url="https://www.reddit.com/gallery/gal" data-timestamp="1700000003">
  <a class="thumbnail" href="https://www.reddit.com/gallery/gal"><img src="//preview.redd.it/cover1.jpeg?width=140&amp;crop=1:1,smart&amp;s=sig"></a>
  <div class="entry"><p class="title"><a class="title">Gallery</a></p></div>
</div>
<div class=" thing id-t3_vid even link" data-fullname="t3_vid" data-author="erin" data-subreddit="videos" data-permalink="/r/videos/comments/vid/clip/" data-url="https://v.redd.it/vid1" data-timestamp="1700000004">
  <a class="thumbnail" href="https://v.redd.it/vid1"><img src="//preview.redd.it/vthumb.jpeg?width=140&amp;s=sig"></a>
  <div class="entry"><p class="title"><a class="title">Clip</a></p></div>
</div>
<div class=" thing id-t3_aaa odd link" data-fullname="t3_aaa" data-author="alice" data-permalink="/r/golang/comments/aaa/some_title/" data-url="https://example.com/article">
  <p class="title"><a class="title">Duplicate</a></p>
</div>
</body></html>`

func TestRedditTarget(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		kind string
		name string
		feed string
		home string
	}{
		{"https://reddit.com/r/cats", true, "r", "cats", "https://www.reddit.com/r/cats", "https://www.reddit.com/r/cats"},
		{"https://www.reddit.com/r/Cats/", true, "r", "cats", "https://www.reddit.com/r/cats", "https://www.reddit.com/r/cats"},
		{"https://www.reddit.com/r/cats/.rss", true, "r", "cats", "https://www.reddit.com/r/cats", "https://www.reddit.com/r/cats"},
		{"https://www.reddit.com/r/cats.rss", true, "r", "cats", "https://www.reddit.com/r/cats", "https://www.reddit.com/r/cats"},
		{"https://old.reddit.com/r/cats/new/", true, "r", "cats", "https://www.reddit.com/r/cats", "https://www.reddit.com/r/cats"},
		{"https://old.reddit.com/r/cats/new/.rss", true, "r", "cats", "https://www.reddit.com/r/cats", "https://www.reddit.com/r/cats"},
		{"https://www.reddit.com/r/cats/top/.rss", true, "r", "cats", "https://www.reddit.com/r/cats", "https://www.reddit.com/r/cats"},
		{"https://www.reddit.com/user/sam", true, "u", "sam", "https://www.reddit.com/user/sam", "https://www.reddit.com/user/sam"},
		{"https://www.reddit.com/u/sam/submitted.rss", true, "u", "sam", "https://www.reddit.com/user/sam", "https://www.reddit.com/user/sam"},
		{"https://old.reddit.com/user/sam/submitted/?sort=new", true, "u", "sam", "https://www.reddit.com/user/sam", "https://www.reddit.com/user/sam"},
		{"https://www.reddit.com/r/cats/comments/abc/x/", false, "", "", "", ""},
		{"https://www.reddit.com/r/cats/search?q=x", false, "", "", "", ""},
		{"https://www.reddit.com/", false, "", "", "", ""},
		{"https://example.com/r/cats", false, "", "", "", ""},
	}
	for _, tc := range cases {
		got, ok := redditTargetFor(tc.in)
		if ok != tc.ok {
			t.Errorf("redditTargetFor(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Kind != tc.kind || got.Name != tc.name || got.FeedURL != tc.feed || got.HomeURL != tc.home {
			t.Errorf("redditTargetFor(%q) = %+v", tc.in, got)
		}
	}
}

func TestParseRedditListing(t *testing.T) {
	feed, err := parseRedditListing([]byte(redditListingFixture))
	if err != nil {
		t.Fatalf("parseRedditListing: %v", err)
	}
	if len(feed.Items) != 5 {
		t.Fatalf("items = %d, want 5 (duplicate dropped)", len(feed.Items))
	}

	link := feed.Items[0]
	if link.GUID != "t3_aaa" {
		t.Errorf("GUID = %q", link.GUID)
	}
	if link.Title != "Some & Title" {
		t.Errorf("Title = %q", link.Title)
	}
	if link.Link != "https://www.reddit.com/r/golang/comments/aaa/some_title/" {
		t.Errorf("Link = %q", link.Link)
	}
	if link.Author != "alice" {
		t.Errorf("Author = %q", link.Author)
	}
	if got := htmlToText(link.Content); got != "https://example.com/article" {
		t.Errorf("content = %q", got)
	}
	if len(link.Media) != 1 || !strings.HasPrefix(link.Media[0].URL, "https://external-preview.redd.it/example123.jpeg") {
		t.Errorf("media = %+v", link.Media)
	}
	wantTime := time.Date(2026, 11, 16, 8, 46, 37, 0, time.UTC)
	if !link.Published.Equal(wantTime) {
		t.Errorf("Published = %v, want %v", link.Published, wantTime)
	}

	self := feed.Items[1]
	if got := htmlToText(self.Content); got != "Self text" {
		t.Errorf("self content = %q", got)
	}
	if len(self.Media) != 0 {
		t.Errorf("self media = %+v", self.Media)
	}
	if !self.Published.Equal(time.UnixMilli(1700000001000).UTC()) {
		t.Errorf("self published = %v", self.Published)
	}

	image := feed.Items[2]
	if len(image.Media) != 1 || image.Media[0].URL != "https://i.redd.it/pic1.jpeg" {
		t.Errorf("image media = %+v", image.Media)
	}

	gallery := feed.Items[3]
	if len(gallery.Media) != 1 || gallery.Media[0].URL != "https://i.redd.it/cover1.jpeg" {
		t.Errorf("gallery media = %+v", gallery.Media)
	}
	if !gallery.Gallery {
		t.Error("gallery post was not marked as a gallery")
	}
	if !gallery.Reddit {
		t.Error("gallery post was not marked as reddit")
	}
	if link.Gallery {
		t.Error("link post was marked as a gallery")
	}

	video := feed.Items[4]
	if len(video.Media) != 1 || !strings.HasPrefix(video.Media[0].URL, "https://preview.redd.it/vthumb.jpeg") {
		t.Errorf("video media = %+v", video.Media)
	}
}

func TestNormalizeFeedURLReddit(t *testing.T) {
	cases := map[string]string{
		"https://reddit.com/r/cats":                        "https://www.reddit.com/r/cats",
		"reddit.com/r/cats":                                "https://www.reddit.com/r/cats",
		"https://www.reddit.com/r/Cats/":                   "https://www.reddit.com/r/cats",
		"https://old.reddit.com/r/cats/new/.rss":           "https://www.reddit.com/r/cats",
		"https://www.reddit.com/r/cats/.rss?sort=hot":      "https://www.reddit.com/r/cats",
		"https://www.reddit.com/user/sam/submitted.rss":    "https://www.reddit.com/user/sam",
		"https://old.reddit.com/u/sam/submitted/?sort=new": "https://www.reddit.com/user/sam",
	}
	for input, want := range cases {
		got, err := normalizeFeedURL(input)
		if err != nil {
			t.Fatalf("normalizeFeedURL(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("normalizeFeedURL(%q) = %q, want %q", input, got, want)
		}
	}

	for _, input := range []string{
		"https://www.reddit.com/r/cats/comments/abc/x/",
		"https://www.reddit.com/r/cats/search?q=x",
		"https://www.reddit.com/",
	} {
		if _, err := normalizeFeedURL(input); err == nil {
			t.Errorf("normalizeFeedURL(%q) unexpectedly succeeded", input)
		}
	}
}

func TestRedditListingURL(t *testing.T) {
	target, ok := redditTargetFor("https://www.reddit.com/r/golang")
	if !ok {
		t.Fatal("target not resolved")
	}
	if got := redditListingURL(target, true); !strings.HasSuffix(got, "/r/golang/new/") {
		t.Errorf("subreddit listing = %q", got)
	}

	user, ok := redditTargetFor("https://www.reddit.com/user/sam")
	if !ok {
		t.Fatal("user target not resolved")
	}
	got := redditListingURL(user, true)
	if !strings.Contains(got, "/user/sam/submitted/") {
		t.Errorf("user listing = %q", got)
	}
	if !strings.Contains(got, "sort=new") || !strings.Contains(got, "include_over_18=on") {
		t.Errorf("user listing query = %q", got)
	}
	if plain := redditListingURL(user, false); strings.Contains(plain, "include_over_18") {
		t.Errorf("NSFW should be optional: %q", plain)
	}
}

func TestFetchRedditListing(t *testing.T) {
	var gotPath, gotCookie, gotAgent, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCookie = r.Header.Get("Cookie")
		gotAgent = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, redditListingFixture)
	}))
	defer server.Close()

	restore := redditBase
	redditBase = server.URL
	t.Cleanup(func() { redditBase = restore })

	f := testFeature()
	f.redditSession = "abc"
	f.redditUserAgent = defaultRedditUserAgent
	f.redditIncludeNSFW = true

	target, ok := redditTargetFor("https://www.reddit.com/r/golang")
	if !ok {
		t.Fatal("target not resolved")
	}
	feed, err := f.fetchReddit(context.Background(), target)
	if err != nil {
		t.Fatalf("fetchReddit: %v", err)
	}
	if gotPath != "/r/golang/new/" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotCookie, "reddit_session=abc") || !strings.Contains(gotCookie, "over18=1") {
		t.Errorf("cookie = %q", gotCookie)
	}
	if gotAgent != defaultRedditUserAgent {
		t.Errorf("user agent = %q", gotAgent)
	}
	if !strings.Contains(gotAccept, "text/html") {
		t.Errorf("accept = %q", gotAccept)
	}
	if feed.Title != "r/golang" || feed.SiteURL != "https://www.reddit.com/r/golang" {
		t.Errorf("feed = %+v", feed)
	}
	if len(feed.Items) != 5 {
		t.Errorf("items = %d", len(feed.Items))
	}
}

func TestFetchRedditUserListing(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `<html><body><div id="siteTable"></div></body></html>`)
	}))
	defer server.Close()

	restore := redditBase
	redditBase = server.URL
	t.Cleanup(func() { redditBase = restore })

	f := testFeature()
	f.redditSession = "abc"
	f.redditUserAgent = defaultRedditUserAgent
	f.redditIncludeNSFW = true

	target, _ := redditTargetFor("https://www.reddit.com/user/sam")
	if _, err := f.fetchReddit(context.Background(), target); err != nil {
		t.Fatalf("fetchReddit: %v", err)
	}
	if gotPath != "/user/sam/submitted/" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "sort=new") || !strings.Contains(gotQuery, "include_over_18=on") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestFetchRedditMissingSession(t *testing.T) {
	f := testFeature()
	target, _ := redditTargetFor("https://www.reddit.com/r/golang")
	if _, err := f.fetchReddit(context.Background(), target); !errors.Is(err, errRedditSession) {
		t.Fatalf("err = %v, want errRedditSession", err)
	}
}

func TestFetchRedditLoginWall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><title>Welcome to Reddit</title></head><body><div id="login-form"></div></body></html>`)
	}))
	defer server.Close()

	restore := redditBase
	redditBase = server.URL
	t.Cleanup(func() { redditBase = restore })

	f := testFeature()
	f.redditSession = "expired"
	f.redditUserAgent = defaultRedditUserAgent
	target, _ := redditTargetFor("https://www.reddit.com/r/golang")
	if _, err := f.fetchReddit(context.Background(), target); !errors.Is(err, errRedditSession) {
		t.Fatalf("err = %v, want errRedditSession", err)
	}
}

func TestFetchRedditAgeGate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><title>over 18</title></head><body><div class="over18-gate">Are you over 18?</div></body></html>`)
	}))
	defer server.Close()

	restore := redditBase
	redditBase = server.URL
	t.Cleanup(func() { redditBase = restore })

	f := testFeature()
	f.redditSession = "abc"
	f.redditUserAgent = defaultRedditUserAgent
	f.redditIncludeNSFW = false

	target, _ := redditTargetFor("https://www.reddit.com/r/golang")
	_, err := f.fetchReddit(context.Background(), target)
	if err == nil || !strings.Contains(err.Error(), "REDDIT_INCLUDE_NSFW") {
		t.Fatalf("err = %v, want an NSFW hint", err)
	}
}

func TestFetchRedditStatusErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusTooManyRequests, "", "rate limited"},
		{http.StatusNotFound, `<p>This community has been banned</p>`, "been banned"},
		{http.StatusForbidden, "", "session cookie"},
		{http.StatusInternalServerError, "", "HTTP 500"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			restore := redditBase
			redditBase = server.URL
			t.Cleanup(func() { redditBase = restore })

			f := testFeature()
			f.redditSession = "abc"
			f.redditUserAgent = defaultRedditUserAgent
			target, _ := redditTargetFor("https://www.reddit.com/r/golang")
			_, err := f.fetchReddit(context.Background(), target)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

const redditPostFixture = `[
  {"data": {"children": [{"data": {
    "id": "gal1",
    "title": "A gallery",
    "permalink": "/r/pics/comments/gal1/a_gallery/",
    "author": "carol",
    "subreddit_name_prefixed": "r/pics",
    "created_utc": 1700000000,
    "is_gallery": true,
    "selftext_html": "<!-- SC_OFF --><div class=\"md\"><p>Hello</p></div><!-- SC_ON -->",
    "gallery_data": {"items": [{"media_id": "m1"}, {"media_id": "m2"}]},
    "media_metadata": {
      "m1": {"status": "valid", "e": "Image", "m": "image/jpeg", "s": {"u": "https://preview.redd.it/m1.jpeg?width=1080&amp;s=sig"}},
      "m2": {"status": "valid", "e": "AnimatedImage", "m": "image/gif", "s": {"u": "https://preview.redd.it/m2.gif?width=1080&amp;s=sig", "mp4": "https://preview.redd.it/m2.mp4?width=1080&amp;s=sig"}}
    }
  }}]}}
]`

const redditVideoFixture = `[
  {"data": {"children": [{"data": {
    "id": "vid1",
    "title": "A clip",
    "permalink": "/r/videos/comments/vid1/a_clip/",
    "author": "dave",
    "created_utc": 1700000000,
    "selftext_html": null,
    "url": "https://v.redd.it/vid1",
    "secure_media": {"reddit_video": {"fallback_url": "https://v.redd.it/vid1/DASH_720.mp4?source=fallback"}}
  }}]}}
]`

const redditImageFixture = `[
  {"data": {"children": [{"data": {
    "id": "img1",
    "title": "A picture",
    "permalink": "/r/pics/comments/img1/a_picture/",
    "author": "erin",
    "created_utc": 1700000000,
    "url": "https://i.redd.it/pic1.jpeg",
    "preview": {"images": [{"source": {"url": "https://preview.redd.it/pic1.jpeg?width=1080&amp;s=sig"}}]}
  }}]}}
]`

func TestParseRedditPostGallery(t *testing.T) {
	item, err := parseRedditPost([]byte(redditPostFixture))
	if err != nil {
		t.Fatalf("parseRedditPost: %v", err)
	}
	if item.Title != "A gallery" {
		t.Errorf("Title = %q", item.Title)
	}
	if item.Link != "https://www.reddit.com/r/pics/comments/gal1/a_gallery/" {
		t.Errorf("Link = %q", item.Link)
	}
	if got := strings.TrimSpace(htmlToText(item.Content)); got != "Hello" {
		t.Errorf("content = %q", got)
	}
	if len(item.Media) != 2 {
		t.Fatalf("media = %d, want 2: %+v", len(item.Media), item.Media)
	}
	if !item.Gallery || !item.Reddit {
		t.Errorf("gallery flags = %v/%v", item.Gallery, item.Reddit)
	}
	if item.Media[0].URL != "https://preview.redd.it/m1.jpeg?width=1080&s=sig" {
		t.Errorf("media[0] = %q", item.Media[0].URL)
	}
	if item.Media[1].URL != "https://preview.redd.it/m2.mp4?width=1080&s=sig" {
		t.Errorf("media[1] = %q, want the MP4 for an animated image", item.Media[1].URL)
	}
}

func TestParseRedditPostVideo(t *testing.T) {
	item, err := parseRedditPost([]byte(redditVideoFixture))
	if err != nil {
		t.Fatalf("parseRedditPost: %v", err)
	}
	if len(item.Media) != 1 || item.Media[0].URL != "https://v.redd.it/vid1/DASH_720.mp4?source=fallback" {
		t.Fatalf("media = %+v", item.Media)
	}
}

func TestParseRedditPostImage(t *testing.T) {
	item, err := parseRedditPost([]byte(redditImageFixture))
	if err != nil {
		t.Fatalf("parseRedditPost: %v", err)
	}
	if len(item.Media) != 1 || item.Media[0].URL != "https://i.redd.it/pic1.jpeg" {
		t.Fatalf("media = %+v", item.Media)
	}
}

func TestRedditPostIDFromCustomID(t *testing.T) {
	cases := map[string]string{
		"reddit:load:abc123": "abc123",
		"reddit:load:ABC123": "abc123",
		"reddit:load:":       "",
		"reddit:nope:abc":    "",
		"reddit:load:ab-c":   "",
	}
	for input, want := range cases {
		got, ok := redditPostIDFromCustomID(input)
		if want == "" {
			if ok {
				t.Errorf("redditPostIDFromCustomID(%q) = %q, want invalid", input, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("redditPostIDFromCustomID(%q) = %q, %v, want %q", input, got, ok, want)
		}
	}
}

func TestRedditLoadButton(t *testing.T) {
	components := redditLoadButton(FeedItem{GUID: "t3_abc123"})
	if len(components) != 1 {
		t.Fatalf("components = %d", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("component type = %T", components[0])
	}
	button, ok := row.Components[0].(discordgo.Button)
	if !ok {
		t.Fatalf("button type = %T", row.Components[0])
	}
	if button.Label != "Load full post" || button.CustomID != "reddit:load:abc123" {
		t.Errorf("button = %+v", button)
	}

	gallery := redditLoadButton(FeedItem{GUID: "t3_abc123", Gallery: true})
	row, ok = gallery[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("gallery component type = %T", gallery[0])
	}
	button, ok = row.Components[0].(discordgo.Button)
	if !ok {
		t.Fatalf("gallery button type = %T", row.Components[0])
	}
	if button.Label != "Load gallery" {
		t.Errorf("gallery button label = %q", button.Label)
	}

	if redditLoadButton(FeedItem{GUID: "not-a-reddit-post"}) != nil {
		t.Error("non-reddit items must not get the button")
	}
}
