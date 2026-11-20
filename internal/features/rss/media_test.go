package rss

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadMediaSkipsBrokenAndOversized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, strings.Repeat("p", 512))
		case "/big.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(make([]byte, 4096))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html></html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	f := &Feature{
		log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		maxAttachmentBytes: 1024,
		httpClient:         server.Client(),
	}

	item := FeedItem{
		GUID: "item-1",
		Media: []Media{
			{URL: server.URL + "/good.png"},
			{URL: server.URL + "/missing.jpg"},
			{URL: server.URL + "/big.jpg"},
			{URL: server.URL + "/page"},
			{URL: server.URL + "/good.png"},
		},
	}

	files := f.downloadMedia(context.Background(), item)
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	if files[0].FileName != "media-1.png" {
		t.Fatalf("file name = %q", files[0].FileName)
	}
	if files[1].FileName != "media-5.png" {
		t.Fatalf("file name = %q", files[1].FileName)
	}
	if len(files[0].Data) != 512 {
		t.Fatalf("data length = %d", len(files[0].Data))
	}
}

func TestDownloadMediaKeepsAllMediaForChunking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "jpeg")
	}))
	defer server.Close()

	media := make([]Media, 15)
	for i := range media {
		media[i] = Media{URL: server.URL + "/image.jpg"}
	}

	f := &Feature{
		log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		maxAttachmentBytes: 1 << 20,
		httpClient:         server.Client(),
	}

	if files := f.downloadMedia(context.Background(), FeedItem{Media: media}); len(files) != len(media) {
		t.Fatalf("files = %d, want %d; galleries must survive chunking", len(files), len(media))
	}
}

func TestDownloadedMediaIsImage(t *testing.T) {
	cases := []struct {
		name  string
		media downloadedMedia
		want  bool
	}{
		{"image content type", downloadedMedia{ContentType: "image/webp", FileName: "a.bin"}, true},
		{"video content type", downloadedMedia{ContentType: "video/mp4", FileName: "a.jpg"}, false},
		{"octet stream with image extension", downloadedMedia{ContentType: "application/octet-stream", FileName: "a.png"}, true},
		{"octet stream with video extension", downloadedMedia{ContentType: "application/octet-stream", FileName: "a.mp4"}, false},
		{"unknown type with image extension", downloadedMedia{FileName: "a.jpeg"}, true},
	}
	for _, tc := range cases {
		if got := tc.media.isImage(); got != tc.want {
			t.Errorf("%s: isImage = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestExtensionFor(t *testing.T) {
	cases := []struct {
		contentType string
		url         string
		want        string
	}{
		{"image/jpeg", "https://cdn.example/a.png", ".jpg"},
		{"image/png", "", ".png"},
		{"video/mp4", "", ".mp4"},
		{"image/gif", "", ".gif"},
		{"application/octet-stream", "https://cdn.example/a.jpg?e=1&t=sig", ".jpg"},
	}
	for _, tc := range cases {
		if got := extensionFor(tc.contentType, tc.url); got != tc.want {
			t.Errorf("extensionFor(%q, %q) = %q, want %q", tc.contentType, tc.url, got, tc.want)
		}
	}
}
