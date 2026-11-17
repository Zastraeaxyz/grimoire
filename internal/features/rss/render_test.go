package rss

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

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
		Title:     "zastraea - image",
		Link:      "https://imaglr.com/post/109448475",
		Content:   "<p>Clear statements</p><img src=\"https://cdn.example/a.jpg\"/>",
		Published: time.Date(2026, 11, 17, 16, 51, 4, 0, time.UTC),
	}

	msg := f.buildMessage(context.Background(), feed, item)
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
	if embed.Description != "Clear statements" {
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

func TestFooterText(t *testing.T) {
	cases := []struct {
		name string
		item FeedItem
		want string
	}{
		{
			name: "plain feed keeps the site",
			item: FeedItem{Link: "https://imaglr.com/post/1"},
			want: "imaglr.com",
		},
		{
			name: "reddit post names the author",
			item: FeedItem{
				Link:   "https://www.reddit.com/r/cats/comments/a/x/",
				Author: "alice",
				Reddit: true,
			},
			want: "u/alice \u2022 reddit.com",
		},
		{
			name: "reddit gallery is called out",
			item: FeedItem{
				Link:    "https://www.reddit.com/r/cats/comments/a/x/",
				Author:  "alice",
				Reddit:  true,
				Gallery: true,
			},
			want: "Gallery \u2022 u/alice \u2022 reddit.com",
		},
		{
			name: "reddit post without an author",
			item: FeedItem{
				Link:   "https://www.reddit.com/r/cats/comments/a/x/",
				Reddit: true,
			},
			want: "reddit.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := footerText(tc.item); got != tc.want {
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
