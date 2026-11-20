package rss

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func redgifsTestAPI(t *testing.T, mediaURL string) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()

	var authCalls, gifCalls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/auth/temporary":
			authCalls.Add(1)
			_, _ = io.WriteString(w, `{"token":"tok"}`)
		case "/v2/gifs/abc123":
			gifCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"urls":{"hd":"`+mediaURL+`/hd.mp4","sd":"`+mediaURL+`/sd.mp4"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)

	restore := redgifsAPIBase
	redgifsAPIBase = api.URL
	t.Cleanup(func() { redgifsAPIBase = restore })

	return api, &authCalls, &gifCalls
}

func redgifsTestMedia(t *testing.T) *httptest.Server {
	t.Helper()

	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hd.mp4":
			http.Error(w, "unavailable", http.StatusBadGateway)
		case "/sd.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "sd-video")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(media.Close)
	return media
}

func TestRedgifsMediaFallsBackToSDAndCachesToken(t *testing.T) {
	media := redgifsTestMedia(t)
	api, authCalls, gifCalls := redgifsTestAPI(t, media.URL)

	f := testFeature()
	f.httpClient = api.Client()

	item := FeedItem{RedgifsURL: "https://www.redgifs.com/watch/abc123"}
	files := f.redgifsMediaFor(context.Background(), item)
	if len(files) != 1 {
		t.Fatalf("files = %d, want the SD fallback", len(files))
	}
	if files[0].ContentType != "video/mp4" || string(files[0].Data) != "sd-video" {
		t.Errorf("media = %+v", files[0])
	}
	if files[0].FileName != "media-1.mp4" {
		t.Errorf("file name = %q", files[0].FileName)
	}

	// A second post reuses the cached token.
	if files := f.redgifsMediaFor(context.Background(), item); len(files) != 1 {
		t.Fatalf("second files = %d", len(files))
	}
	if got := authCalls.Load(); got != 1 {
		t.Errorf("auth calls = %d, want 1", got)
	}
	if got := gifCalls.Load(); got != 2 {
		t.Errorf("gif calls = %d, want 2", got)
	}
}

func TestRedgifsMediaRefreshesRejectedToken(t *testing.T) {
	media := redgifsTestMedia(t)

	var authCalls, gifCalls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/auth/temporary":
			authCalls.Add(1)
			_, _ = io.WriteString(w, `{"token":"fresh"}`)
		case "/v2/gifs/abc123":
			gifCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer fresh" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"urls":{"sd":"`+media.URL+`/sd.mp4"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	restore := redgifsAPIBase
	redgifsAPIBase = api.URL
	t.Cleanup(func() { redgifsAPIBase = restore })

	f := testFeature()
	f.httpClient = api.Client()
	f.redgifsToken = "stale"
	f.redgifsTokenExpiry = time.Now().Add(time.Hour)

	files := f.redgifsMediaFor(context.Background(), FeedItem{RedgifsURL: "https://www.redgifs.com/watch/abc123"})
	if len(files) != 1 {
		t.Fatalf("files = %d, want the refreshed video", len(files))
	}
	if got := authCalls.Load(); got != 1 {
		t.Errorf("auth calls = %d, want 1 refresh", got)
	}
	if got := gifCalls.Load(); got != 2 {
		t.Errorf("gif calls = %d, want the rejected try plus the retry", got)
	}
}

func TestRedgifsMediaKeepsStillFrameOnFailure(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	defer api.Close()

	restore := redgifsAPIBase
	redgifsAPIBase = api.URL
	t.Cleanup(func() { redgifsAPIBase = restore })

	f := testFeature()
	f.httpClient = api.Client()

	if files := f.redgifsMediaFor(context.Background(), FeedItem{RedgifsURL: "https://www.redgifs.com/watch/abc123"}); files != nil {
		t.Fatalf("files = %+v, want nil so the still frame stays", files)
	}
}
