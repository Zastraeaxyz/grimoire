package rss

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBdsmlrPostID(t *testing.T) {
	cases := map[string]int64{
		"https://bdsmlr.com/post/877982914":         877982914,
		"http://bdsmlr.com/post/123/":               123,
		"https://www.bdsmlr.com/post/456?from=feed": 456,
	}
	for input, want := range cases {
		got, ok := bdsmlrPostID(input)
		if !ok || got != want {
			t.Errorf("bdsmlrPostID(%q) = %d, %v, want %d, true", input, got, ok, want)
		}
	}

	for _, input := range []string{
		"",
		"https://imaglr.com/post/123",
		"https://bdsmlr.com/blog/zastraeachastity",
		"https://bdsmlr.com/post/not-a-number",
		"https://bdsmlr.com/post/",
	} {
		if got, ok := bdsmlrPostID(input); ok {
			t.Errorf("bdsmlrPostID(%q) = %d, true, want false", input, got)
		}
	}
}

const bdsmlrDetailFixture = `{
  "post": {
    "id": 877982914,
    "mediaRepresentation": {
      "items": [
        {
          "original": {"url": "https://cdn.example/x.gif", "mimeType": "image/gif"},
          "alternates": [
            {"url": "https://cdn.example/x.mp4", "mimeType": "video/mp4"},
            {"url": "https://cdn.example/x.mp4", "mimeType": "video/mp4"}
          ],
          "poster": null,
          "preview": null
        },
        {
          "original": {"url": "https://cdn.example/y.jpeg", "mimeType": null},
          "alternates": []
        },
        {
          "original": null,
          "alternates": [],
          "poster": {"url": "https://cdn.example/p.jpg", "mimeType": "image/jpeg"}
        }
      ]
    }
  }
}`

func TestParseBdsmlrMediaPrefersAlternates(t *testing.T) {
	media := parseBdsmlrMedia([]byte(bdsmlrDetailFixture))
	if len(media) != 3 {
		t.Fatalf("media = %d, want 3: %+v", len(media), media)
	}
	if media[0].URL != "https://cdn.example/x.mp4" {
		t.Fatalf("first media = %q, want the MP4 alternate", media[0].URL)
	}
	if media[1].URL != "https://cdn.example/y.jpeg" {
		t.Fatalf("second media = %q", media[1].URL)
	}
	if media[2].URL != "https://cdn.example/p.jpg" {
		t.Fatalf("third media = %q, want the poster fallback", media[2].URL)
	}
}

func TestResolveBdsmlrMediaPostsPostID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var payload map[string]int64
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("request body: %v", err)
		}
		if payload["post_id"] != 877982914 {
			t.Errorf("post_id = %d", payload["post_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, bdsmlrDetailFixture)
	}))
	defer server.Close()

	previous := bdsmlrAPIEndpoint
	bdsmlrAPIEndpoint = server.URL
	t.Cleanup(func() { bdsmlrAPIEndpoint = previous })

	f := testFeature()
	media := f.resolveBdsmlrMedia(context.Background(), "https://bdsmlr.com/post/877982914")
	if len(media) != 3 {
		t.Fatalf("media = %d, want 3", len(media))
	}
	if !strings.HasSuffix(media[0].URL, ".mp4") {
		t.Fatalf("media[0] = %q", media[0].URL)
	}
}

func TestResolveBdsmlrMediaIgnoresForeignPosts(t *testing.T) {
	f := testFeature()
	if media := f.resolveBdsmlrMedia(context.Background(), "https://imaglr.com/post/1"); media != nil {
		t.Fatalf("media = %+v, want nil", media)
	}
}
