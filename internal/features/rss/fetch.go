package rss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	userAgent    = "grimoire/0.1 (+feed reader)"
	maxFeedBytes = 5 << 20
)

// fetchResult carries a feed body plus its validators. NotModified is set
// when the source answered 304.
type fetchResult struct {
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
}

// fetchFeed downloads a feed, using stored validators when available.
func (f *Feature) fetchFeed(ctx context.Context, feedURL, etag, lastModified string) (*fetchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.8")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &fetchResult{NotModified: true}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("feed returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading feed: %w", err)
	}
	if len(body) > maxFeedBytes {
		return nil, fmt.Errorf("feed is larger than %d bytes", maxFeedBytes)
	}

	return &fetchResult{
		Body:         body,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}
