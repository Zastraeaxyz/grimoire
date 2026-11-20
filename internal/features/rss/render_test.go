package rss

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/store"
)

func testFeature() *Feature {
	return &Feature{
		log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		maxAttachmentBytes: 1 << 20,
		httpClient:         &http.Client{},
	}
}

func TestBuildMessageUsesFeedAndPostMetadata(t *testing.T) {
	f := testFeature()
	feed := &store.Feed{
		Title: "Imaglr - zastraea's Profile",
		URL:   "https://imaglr.com/profile/zastraea/rss",
	}
	item := FeedItem{
		Title:      "zastraea - image",
		Link:       "https://imaglr.com/post/109448475",
		Content:    "<p>Clear statements</p><img src=\"https://cdn.example/a.jpg\"/>",
		Categories: []string{"cats", "#keyholder"},
		Published:  time.Date(2026, 11, 17, 16, 51, 4, 0, time.UTC),
	}

	messages := f.buildMessages(context.Background(), feed, item)
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	msg := messages[0]
	if len(msg.Embeds) != 1 {
		t.Fatalf("embeds = %d", len(msg.Embeds))
	}
	embed := msg.Embeds[0]

	if embed.Author == nil || embed.Author.Name != "zastraea" {
		t.Fatalf("author = %+v", embed.Author)
	}
	if embed.Title != "New post" {
		t.Fatalf("title = %q", embed.Title)
	}
	if embed.Description != "Clear statements\n\n#cats #keyholder" {
		t.Fatalf("description = %q", embed.Description)
	}
	if embed.URL != item.Link {
		t.Fatalf("url = %q", embed.URL)
	}
	if embed.Timestamp != "2026-11-17T16:51:04Z" {
		t.Fatalf("timestamp = %q", embed.Timestamp)
	}
	if embed.Footer == nil || embed.Footer.Text != "imaglr.com" {
		t.Fatalf("footer = %+v", embed.Footer)
	}
	if len(msg.Files) != 0 {
		t.Fatalf("files = %d, want 0 without a reachable CDN", len(msg.Files))
	}
}

func TestBuildMessageLinksTheRedditAuthor(t *testing.T) {
	f := testFeature()
	feed := &store.Feed{Title: "r/cats", URL: "https://www.reddit.com/r/cats"}
	item := FeedItem{
		GUID:   "t3_a",
		Title:  "A post",
		Link:   "https://www.reddit.com/r/cats/comments/a/x/",
		Author: "alice",
		Reddit: true,
	}

	messages := f.buildMessages(context.Background(), feed, item)
	embed := messages[0].Embeds[0]
	if embed.Author == nil || embed.Author.Name != "u/alice" {
		t.Fatalf("author = %+v", embed.Author)
	}
	if embed.Author.URL != "https://www.reddit.com/user/alice" {
		t.Errorf("author URL = %q", embed.Author.URL)
	}
	if embed.Footer == nil || embed.Footer.Text != "r/cats \u2022 reddit.com" {
		t.Errorf("footer = %+v", embed.Footer)
	}
	if len(messages[0].Components) != 0 {
		t.Errorf("components = %+v, want none", messages[0].Components)
	}
}

func TestBuildMessagesEmbedsASingleImage(t *testing.T) {
	server := imageServer(t)
	defer server.Close()

	f := testFeature()
	f.httpClient = server.Client()

	messages := f.buildMessages(context.Background(), &store.Feed{Title: "Feed"}, FeedItem{
		GUID:  "one-image",
		Media: []Media{{URL: server.URL + "/1.png"}},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	msg := messages[0]
	if msg.Embeds[0].Image == nil || msg.Embeds[0].Image.URL != "attachment://media-1.png" {
		t.Fatalf("embed image = %+v", msg.Embeds[0].Image)
	}
	if len(msg.Files) != 1 || msg.Files[0].Name != "media-1.png" {
		t.Fatalf("files = %+v", msg.Files)
	}
}

func TestBuildMessagesKeepsGalleryAsAttachments(t *testing.T) {
	server := imageServer(t)
	defer server.Close()

	f := testFeature()
	f.httpClient = server.Client()

	messages := f.buildMessages(context.Background(), &store.Feed{Title: "Feed"}, FeedItem{
		GUID: "gallery",
		Media: []Media{
			{URL: server.URL + "/1.jpg"},
			{URL: server.URL + "/2.jpg"},
			{URL: server.URL + "/3.jpg"},
		},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	msg := messages[0]
	if msg.Embeds[0].Image != nil {
		t.Fatalf("gallery got an embed image: %+v", msg.Embeds[0].Image)
	}
	if len(msg.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(msg.Files))
	}
}

func TestBuildMessagesChunksLargeGalleries(t *testing.T) {
	server := imageServer(t)
	defer server.Close()

	f := testFeature()
	f.httpClient = server.Client()

	media := make([]Media, 12)
	for i := range media {
		media[i] = Media{URL: server.URL + "/image.jpg"}
	}

	messages := f.buildMessages(context.Background(), &store.Feed{Title: "Feed"}, FeedItem{
		GUID:  "big-gallery",
		Media: media,
	})
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if len(messages[0].Files) != maxAttachments || len(messages[0].Embeds) != 1 {
		t.Fatalf("first message = %d files, %d embeds", len(messages[0].Files), len(messages[0].Embeds))
	}
	if len(messages[1].Files) != 2 || len(messages[1].Embeds) != 0 {
		t.Fatalf("second message = %d files, %d embeds", len(messages[1].Files), len(messages[1].Embeds))
	}
}

func TestClearAttachmentImages(t *testing.T) {
	embeds := []*discordgo.MessageEmbed{{
		Image:     &discordgo.MessageEmbedImage{URL: "attachment://media-1.png"},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: "attachment://media-2.png"},
	}}
	clearAttachmentImages(embeds)
	if embeds[0].Image != nil || embeds[0].Thumbnail != nil {
		t.Fatalf("attachment images were kept: %+v", embeds[0])
	}
}

// imageServer serves a tiny valid image for any path.
func imageServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".png") {
			w.Header().Set("Content-Type", "image/png")
		} else {
			w.Header().Set("Content-Type", "image/jpeg")
		}
		_, _ = io.WriteString(w, "image-bytes")
	}))
}

func TestEmbedDescriptionIncludesEveryCategory(t *testing.T) {
	cases := []struct {
		name string
		item FeedItem
		want string
	}{
		{
			name: "body and tags",
			item: FeedItem{Content: "<p>Hi</p>", Categories: []string{"one", "two"}},
			want: "Hi\n\n#one #two",
		},
		{
			name: "tags without a body",
			item: FeedItem{Categories: []string{"one", "two"}},
			want: "#one #two",
		},
		{
			name: "summary body and tags",
			item: FeedItem{Summary: "Fallback", Categories: []string{"one"}},
			want: "Fallback\n\n#one",
		},
		{
			name: "no categories",
			item: FeedItem{Content: "<p>Hi</p>"},
			want: "Hi",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := embedDescription(tc.item); got != tc.want {
				t.Fatalf("embedDescription = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEmbedDescriptionKeepsTagsWhenBodyIsLong(t *testing.T) {
	body := strings.Repeat("word ", embedDescriptionLimit)
	got := embedDescription(FeedItem{Content: body, Categories: []string{"one", "two"}})

	if !strings.HasSuffix(got, "\n\n#one #two") {
		t.Fatalf("description lost its tags: %q", got[len(got)-40:])
	}
	if runes := len([]rune(got)); runes > embedDescriptionLimit {
		t.Fatalf("description = %d runes, limit %d", runes, embedDescriptionLimit)
	}
}

func TestFooterText(t *testing.T) {
	cases := []struct {
		name string
		feed *store.Feed
		item FeedItem
		want string
	}{
		{
			name: "plain feed keeps the site",
			feed: &store.Feed{Title: "Feed"},
			item: FeedItem{Link: "https://imaglr.com/post/1"},
			want: "imaglr.com",
		},
		{
			name: "reddit post names its feed",
			feed: &store.Feed{Title: "r/cats"},
			item: FeedItem{
				Link:   "https://www.reddit.com/r/cats/comments/a/x/",
				Author: "alice",
				Reddit: true,
			},
			want: "r/cats \u2022 reddit.com",
		},
		{
			name: "reddit gallery is called out",
			feed: &store.Feed{Title: "r/cats"},
			item: FeedItem{
				Link:    "https://www.reddit.com/r/cats/comments/a/x/",
				Author:  "alice",
				Reddit:  true,
				Gallery: true,
			},
			want: "Gallery \u2022 r/cats \u2022 reddit.com",
		},
		{
			name: "reddit post without an author",
			feed: &store.Feed{Title: "r/cats"},
			item: FeedItem{
				Link:   "https://www.reddit.com/r/cats/comments/a/x/",
				Reddit: true,
			},
			want: "r/cats \u2022 reddit.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := footerText(tc.feed, tc.item); got != tc.want {
				t.Fatalf("footerText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCleanFeedTitle(t *testing.T) {
	cases := map[string]string{
		"Imaglr - zastraea's Profile": "zastraea",
		"Zastraea":                    "Zastraea",
		"Example - Blog":              "Example - Blog",
		"":                            "",
	}
	for input, want := range cases {
		if got := cleanFeedTitle(input); got != want {
			t.Errorf("cleanFeedTitle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFeedLabel(t *testing.T) {
	feed := &store.Feed{
		Title:  "Imaglr - zastraea's Profile",
		Domain: "imaglr.com",
	}
	if got := feedLabel(feed); got != "zastraea on imaglr.com" {
		t.Fatalf("feedLabel = %q", got)
	}

	feed.Domain = ""
	if got := feedLabel(feed); got != "zastraea" {
		t.Fatalf("feedLabel without domain = %q", got)
	}
}
