package rss

import (
	"testing"
	"time"
)

const bdsmlrFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:content="http://purl.org/rss/1.0/modules/content/"
     xmlns:dc="http://purl.org/dc/elements/1.1/"
     xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title><![CDATA[Zastraea]]></title>
    <link>https://bdsmlr.com/blog/zastraeachastity</link>
    <item>
      <title><![CDATA[Post 877982930]]></title>
      <description><![CDATA[<p><br></p>
<p><em>Reblogged from Chastitybitchflr</em></p>]]></description>
      <link>https://bdsmlr.com/post/877982930</link>
      <guid isPermaLink="false">https://bdsmlr.com/post/877982930</guid>
      <dc:creator><![CDATA[zastraeachastity]]></dc:creator>
      <pubDate>Mon, 16 Nov 2026 08:46:37 GMT</pubDate>
      <enclosure url="https://cdn.example/bdsmlr-1.jpg?e=1&amp;t=sig" length="0" type="image/jpeg"/>
      <media:content url="https://cdn.example/bdsmlr-1.jpg?e=1&amp;t=sig" type="image/jpeg" medium="image"/>
    </item>
    <item>
      <title><![CDATA[&lt;p&gt;&lt;br&gt;&lt;/p&gt;]]></title>
      <description><![CDATA[<p>&lt;p&gt;Escaped caption&lt;/p&gt;</p>
<p><em>Reblogged from Fmcaptions</em></p>]]></description>
      <link>https://bdsmlr.com/post/877379045</link>
      <guid isPermaLink="false">https://bdsmlr.com/post/877379045</guid>
      <pubDate>Sun, 15 Nov 2026 10:00:00 GMT</pubDate>
      <enclosure url="https://cdn.example/multi-1.jpg?e=1&amp;t=sig" length="0" type="image/jpeg"/>
      <media:content url="https://cdn.example/multi-1.jpg?e=1&amp;t=sig" type="image/jpeg" medium="image"/>
      <media:content url="https://cdn.example/multi-2.jpg?e=1&amp;t=sig" type="image/jpeg" medium="image"/>
      <media:content url="https://cdn.example/multi-3.jpg?e=1&amp;t=sig" type="image/jpeg" medium="image"/>
      <media:content url="https://cdn.example/multi-4.jpg?e=1&amp;t=sig" type="image/jpeg" medium="image"/>
    </item>
    <item>
      <title><![CDATA[A Video: Cuckold]]></title>
      <description><![CDATA[A Video: Cuckold]]></description>
      <link>https://bdsmlr.com/post/877982798</link>
      <guid isPermaLink="false">https://bdsmlr.com/post/877982798</guid>
      <pubDate>Sat, 14 Nov 2026 10:00:00 GMT</pubDate>
      <enclosure url="https://cdn.example/clip.mp4?e=1&amp;t=sig" length="0" type="video/mp4"/>
      <media:content url="https://cdn.example/clip.mp4?e=1&amp;t=sig" type="video/mp4" medium="video"/>
    </item>
  </channel>
</rss>`

const imaglrFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:content="http://purl.org/rss/1.0/modules/content/"
     xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>Imaglr - zastraea&apos;s Profile</title>
    <link>https://imaglr.com/profile/zastraea</link>
    <item>
      <title>zastraea - image</title>
      <link>https://imaglr.com/post/109448475</link>
      <guid isPermaLink="true">https://imaglr.com/post/109448475</guid>
      <pubDate>Tue, 17 Nov 2026 16:51:04 +0000</pubDate>
      <description><![CDATA[<img src="https://cdn.example/storage/abcdefgh1234extra.png" alt=""/>]]></description>
      <content:encoded><![CDATA[<p>She believes in clear statements</p>
<img src="https://cdn.example/storage/abcdefgh1234extra.png" alt="" />]]></content:encoded>
      <enclosure url="https://cdn.example/storage/abcdefgh1234" type="image/png" length="0"/>
    </item>
    <item>
      <title>zastraea - video</title>
      <link>https://imaglr.com/post/109186641</link>
      <guid isPermaLink="true">https://imaglr.com/post/109186641</guid>
      <pubDate>Mon, 16 Nov 2026 22:26:13 +0000</pubDate>
      <description></description>
      <content:encoded><![CDATA[<video><source src="https://cdn.example/storage/dn1QslgrKJdSeo8EdBj7V2QUK6MMn6SeNOjhPXrQ.mp4" type="video/mp4"/></video>]]></content:encoded>
      <enclosure url="https://cdn.example/storage/dn1QslgrKJdSeo8EdBj7V2QUK.mp4" type="video/mp4" length="0"/>
    </item>
    <item>
      <title>zastraea - image</title>
      <link>https://imaglr.com/post/109000000</link>
      <guid isPermaLink="true">https://imaglr.com/post/109000000</guid>
      <pubDate>Sun, 15 Nov 2026 22:26:13 +0000</pubDate>
      <description></description>
      <content:encoded></content:encoded>
      <enclosure url="https://cdn.example/storage/only-enclosure.jpg" type="image/jpeg" length="0"/>
    </item>
  </channel>
</rss>`

const atomFixture = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example blog</title>
  <link rel="alternate" href="https://example.com/"/>
  <entry>
    <id>tag:example.com,2026:post-1</id>
    <title>A readable post</title>
    <link rel="alternate" href="https://example.com/posts/1"/>
    <link rel="enclosure" href="https://cdn.example/cover.jpg" type="image/jpeg"/>
    <published>2026-11-16T08:46:37Z</published>
    <updated>2026-11-16T09:00:00Z</updated>
    <author><name>Zastraea</name></author>
    <content type="html">&lt;p&gt;Hello &amp;amp; welcome&lt;/p&gt;</content>
  </entry>
</feed>`

func TestParseRSSMediaAndDedupe(t *testing.T) {
	feed, err := parseFeed([]byte(bdsmlrFixture))
	if err != nil {
		t.Fatalf("parseFeed: %v", err)
	}
	if feed.Title != "Zastraea" {
		t.Fatalf("title = %q", feed.Title)
	}
	if feed.SiteURL != "https://bdsmlr.com/blog/zastraeachastity" {
		t.Fatalf("site URL = %q", feed.SiteURL)
	}
	if domain := feedDomain(feed, "https://bdsmlr.zastraea.xyz/zastraeachastity/rss"); domain != "bdsmlr.com" {
		t.Fatalf("domain = %q, want bdsmlr.com", domain)
	}
	if len(feed.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(feed.Items))
	}

	first := feed.Items[0]
	if first.GUID != "https://bdsmlr.com/post/877982930" {
		t.Fatalf("guid = %q", first.GUID)
	}
	if first.Author != "zastraeachastity" {
		t.Fatalf("author = %q", first.Author)
	}
	if got := first.Published.UTC(); !got.Equal(time.Date(2026, 11, 16, 8, 46, 37, 0, time.UTC)) {
		t.Fatalf("published = %v", got)
	}
	if len(first.Media) != 1 {
		t.Fatalf("media = %d, want 1 after dedupe: %+v", len(first.Media), first.Media)
	}
	if first.Media[0].URL != "https://cdn.example/bdsmlr-1.jpg?e=1&t=sig" {
		t.Fatalf("media URL lost its signature: %q", first.Media[0].URL)
	}

	if got := len(feed.Items[1].Media); got != 4 {
		t.Fatalf("multi-image media = %d, want 4", got)
	}

	if title := displayTitle(first); title != "New post" {
		t.Fatalf("displayTitle = %q, want New post", title)
	}
	if text := htmlToText(first.Content); text != "Reblogged from Chastitybitchflr" {
		t.Fatalf("text = %q", text)
	}

	second := htmlToText(feed.Items[1].Content)
	if second != "Escaped caption\n\nReblogged from Fmcaptions" {
		t.Fatalf("escaped text = %q", second)
	}
}

func TestParseImaglrTruncatedVideo(t *testing.T) {
	feed, err := parseFeed([]byte(imaglrFixture))
	if err != nil {
		t.Fatalf("parseFeed: %v", err)
	}
	if domain := feedDomain(feed, "https://imaglr.com/profile/zastraea/rss"); domain != "imaglr.com" {
		t.Fatalf("domain = %q, want imaglr.com", domain)
	}

	image := feed.Items[0]
	if len(image.Media) != 1 {
		t.Fatalf("media = %d, want 1: %+v", len(image.Media), image.Media)
	}
	if image.Media[0].URL != "https://cdn.example/storage/abcdefgh1234extra.png" {
		t.Fatalf("kept %q, want the full URL", image.Media[0].URL)
	}

	video := feed.Items[1]
	if len(video.Media) != 1 {
		t.Fatalf("video media = %d, want 1: %+v", len(video.Media), video.Media)
	}
	if video.Media[0].URL != "https://cdn.example/storage/dn1QslgrKJdSeo8EdBj7V2QUK6MMn6SeNOjhPXrQ.mp4" {
		t.Fatalf("truncated enclosure won: %q", video.Media[0].URL)
	}

	enclosureOnly := feed.Items[2]
	if len(enclosureOnly.Media) != 1 || enclosureOnly.Media[0].URL != "https://cdn.example/storage/only-enclosure.jpg" {
		t.Fatalf("enclosure-only media = %+v", enclosureOnly.Media)
	}

	if title := displayTitle(image); title != "New post" {
		t.Fatalf("displayTitle = %q, want New post", title)
	}
}

func TestParseAtom(t *testing.T) {
	feed, err := parseFeed([]byte(atomFixture))
	if err != nil {
		t.Fatalf("parseFeed: %v", err)
	}
	if feed.Title != "Example blog" {
		t.Fatalf("title = %q", feed.Title)
	}
	if feed.SiteURL != "https://example.com/" {
		t.Fatalf("site URL = %q", feed.SiteURL)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items = %d", len(feed.Items))
	}

	item := feed.Items[0]
	if item.GUID != "tag:example.com,2026:post-1" {
		t.Fatalf("guid = %q", item.GUID)
	}
	if item.Link != "https://example.com/posts/1" {
		t.Fatalf("link = %q", item.Link)
	}
	if item.Author != "Zastraea" {
		t.Fatalf("author = %q", item.Author)
	}
	if got := htmlToText(item.Content); got != "Hello & welcome" {
		t.Fatalf("content = %q", got)
	}
	if len(item.Media) != 1 || item.Media[0].URL != "https://cdn.example/cover.jpg" {
		t.Fatalf("media = %+v", item.Media)
	}
}

func TestParseFeedRejectsHTML(t *testing.T) {
	if _, err := parseFeed([]byte("<html><body>nope</body></html>")); err == nil {
		t.Fatal("expected an error for a non-feed document")
	}
}

func TestNormalizeMediaURL(t *testing.T) {
	base := "https://imaglr.com/post/1"
	cases := map[string]string{
		"//cdn.example/a.jpg":         "https://cdn.example/a.jpg",
		"/storage/a.jpg":              "https://imaglr.com/storage/a.jpg",
		"https://CDN.example/a.jpg?e": "https://cdn.example/a.jpg",
		"javascript:alert(1)":         "",
		"":                            "",
	}
	for input, want := range cases {
		if got := normalizeMediaURL(base, input); got != want {
			t.Errorf("normalizeMediaURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFeedDomain(t *testing.T) {
	cases := []struct {
		name    string
		siteURL string
		feedURL string
		want    string
	}{
		{
			name:    "site link wins over mirror",
			siteURL: "https://bdsmlr.com/blog/zastraeachastity",
			feedURL: "https://bdsmlr.zastraea.xyz/zastraeachastity/rss",
			want:    "bdsmlr.com",
		},
		{
			name:    "www is stripped",
			siteURL: "https://www.imaglr.com/profile/zastraea",
			want:    "imaglr.com",
		},
		{
			name:    "falls back to feed host",
			feedURL: "https://Example.com/feed.xml",
			want:    "example.com",
		},
		{
			name: "no URLs",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := feedDomain(&parsedFeed{SiteURL: tc.siteURL}, tc.feedURL)
			if got != tc.want {
				t.Fatalf("feedDomain = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeFeedURL(t *testing.T) {
	cases := map[string]string{
		"https://bdsmlr.zastraea.xyz/zastraeachastity/rss?include_reblogs=1": "https://bdsmlr.zastraea.xyz/zastraeachastity/rss?include_reblogs=1",
		"bdsmlr.zastraea.xyz/zastraeachastity/rss?include_reblogs=1":         "https://bdsmlr.zastraea.xyz/zastraeachastity/rss?include_reblogs=1",
		"https://imaglr.com/profile/zastraea/rss#top":                        "https://imaglr.com/profile/zastraea/rss",
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

	for _, input := range []string{"", "ftp://example.com/feed", "not a url"} {
		if _, err := normalizeFeedURL(input); err == nil {
			t.Errorf("normalizeFeedURL(%q) unexpectedly succeeded", input)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Minute: "30 minutes",
		90 * time.Minute: "1h 30m",
		time.Hour:        "hour",
		3 * time.Hour:    "3 hours",
	}
	for input, want := range cases {
		if got := humanDuration(input); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", input, got, want)
		}
	}
}

func TestTruncateText(t *testing.T) {
	if got := truncateText("short", 10); got != "short" {
		t.Fatalf("truncateText = %q", got)
	}

	long := "first line that is long enough\nsecond line"
	got := truncateText(long, 30)
	if got[len(got)-3:] != "..." {
		t.Fatalf("truncated text = %q", got)
	}
}
