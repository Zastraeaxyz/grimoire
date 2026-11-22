package stats

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUmamiStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/api/websites/site-id/stats" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("startAt"); got != "1000" {
			t.Errorf("startAt = %q", got)
		}
		if got := r.URL.Query().Get("endAt"); got != "2000" {
			t.Errorf("endAt = %q", got)
		}
		_, _ = io.WriteString(w, `{
			"pageviews": 120, "visitors": 40, "visits": 55,
			"bounces": 11, "totaltime": 3600,
			"comparison": {"pageviews": 60, "visitors": 20, "visits": 30, "bounces": 5, "totaltime": 1800}
		}`)
	}))
	defer srv.Close()

	client := newUmamiClient(srv.URL, "site-id", "test-key")
	got, err := client.stats(context.Background(), time.UnixMilli(1000), time.UnixMilli(2000))
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Pageviews != 120 || got.Visitors != 40 || got.Visits != 55 || got.Bounces != 11 || got.TotalTime != 3600 {
		t.Fatalf("stats = %+v", got)
	}
	if got.Comparison.Visitors != 20 {
		t.Fatalf("comparison visitors = %d, want 20", got.Comparison.Visitors)
	}
}

func TestUmamiMetricsDecodesBothShapes(t *testing.T) {
	cases := map[string]string{
		"bare":    `[{"x":"/home","y":3},{"x":"/blog","y":2}]`,
		"wrapped": `{"data":[{"x":"/home","y":3},{"x":"/blog","y":2}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("type") != "path" {
					t.Errorf("type = %q", r.URL.Query().Get("type"))
				}
				if r.URL.Query().Get("limit") != "5000" {
					t.Errorf("limit = %q", r.URL.Query().Get("limit"))
				}
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()

			client := newUmamiClient(srv.URL, "site-id", "key")
			rows, err := client.metrics(context.Background(), "path", time.UnixMilli(0), time.UnixMilli(1), metricsLimit)
			if err != nil {
				t.Fatalf("metrics: %v", err)
			}
			if len(rows) != 2 || rows[0].X != "/home" || rows[0].Y != 3 {
				t.Fatalf("rows = %+v", rows)
			}
		})
	}
}

func TestUmamiWrappedStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"pageviews":7,"visitors":3,"visits":4,"bounces":1,"totaltime":60}}`)
	}))
	defer srv.Close()

	client := newUmamiClient(srv.URL, "site-id", "key")
	got, err := client.stats(context.Background(), time.UnixMilli(0), time.UnixMilli(1))
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Pageviews != 7 || got.Visitors != 3 {
		t.Fatalf("stats = %+v", got)
	}
}

func TestUmamiErrorIsReadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"Unauthorized","code":"unauthorized"}}`)
	}))
	defer srv.Close()

	client := newUmamiClient(srv.URL, "site-id", "key")
	_, err := client.stats(context.Background(), time.UnixMilli(0), time.UnixMilli(1))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("error = %v", err)
	}
}

func TestCollectReadsEveryWindow(t *testing.T) {
	var (
		mu          sync.Mutex
		statsStarts []string
		pathStarts  []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/stats"):
			mu.Lock()
			statsStarts = append(statsStarts, r.URL.Query().Get("startAt"))
			mu.Unlock()
			_, _ = io.WriteString(w, `{"pageviews":9,"visitors":4,"visits":5,"bounces":1,"totaltime":300}`)
		case strings.HasSuffix(r.URL.Path, "/metrics"):
			if dimension := r.URL.Query().Get("type"); dimension != "path" {
				t.Errorf("unexpected metrics type %q", dimension)
			}
			mu.Lock()
			pathStarts = append(pathStarts, r.URL.Query().Get("startAt"))
			mu.Unlock()
			_, _ = io.WriteString(w, `[{"x":"/zastraeachastity","y":60},{"x":"/home","y":20},{"x":"/zastraeachastity/archive","y":5}]`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	now := time.Date(2026, time.November, 22, 9, 0, 0, 0, time.UTC)
	f := &Feature{
		umami:   newUmamiClient(srv.URL, "site-id", "key"),
		loc:     time.UTC,
		siteURL: defaultSiteURL,
	}
	rep, err := f.collect(context.Background(), now)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if rep.today == nil || rep.today.Pageviews != 9 {
		t.Fatalf("today = %+v", rep.today)
	}
	if len(statsStarts) != 3 {
		t.Fatalf("stats calls = %d, want 3", len(statsStarts))
	}
	want := map[string]bool{
		strconv.FormatInt(time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC).UnixMilli(), 10): true,
		strconv.FormatInt(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC).UnixMilli(), 10): true,
		strconv.FormatInt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC).UnixMilli(), 10): true,
	}
	for _, start := range statsStarts {
		if !want[start] {
			t.Errorf("unexpected stats window start %s", start)
		}
	}
	if len(pathStarts) != 1 {
		t.Fatalf("path metric calls = %d, want 1", len(pathStarts))
	}
	wantPathStart := strconv.FormatInt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC).UnixMilli(), 10)
	if pathStarts[0] != wantPathStart {
		t.Errorf("path window start = %s, want %s", pathStarts[0], wantPathStart)
	}

	if len(rep.topBlogs) != 1 || rep.topBlogs[0].X != "zastraeachastity" || rep.topBlogs[0].Y != 65 {
		t.Fatalf("topBlogs = %+v", rep.topBlogs)
	}
}
