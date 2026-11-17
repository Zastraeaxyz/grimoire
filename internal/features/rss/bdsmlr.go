package rss

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// bdsmlrAPIEndpoint returns a post's real media. The public RSS feed only
// carries the original upload URL, which can point at a converted or deleted
// file (for example a GIF that now exists only as an MP4 alternate).
var bdsmlrAPIEndpoint = "https://api-prod.bdsmlr.com/v2/api/get-post-detail"

type bdsmlrDetail struct {
	Post struct {
		ID                  int64 `json:"id"`
		MediaRepresentation struct {
			Items []bdsmlrMediaItem `json:"items"`
		} `json:"mediaRepresentation"`
	} `json:"post"`
}

type bdsmlrMediaItem struct {
	Original   *bdsmlrMedia  `json:"original"`
	Alternates []bdsmlrMedia `json:"alternates"`
	Poster     *bdsmlrMedia  `json:"poster"`
	Preview    *bdsmlrMedia  `json:"preview"`
}

type bdsmlrMedia struct {
	URL      string `json:"url"`
	MimeType string `json:"mimeType"`
}

// resolveBdsmlrMedia asks bdsmlr for the post's current media. A nil result
// means the post is not from bdsmlr or the API did not answer, in which case
// the caller keeps the feed's own URLs.
func (f *Feature) resolveBdsmlrMedia(ctx context.Context, link string) []Media {
	postID, ok := bdsmlrPostID(link)
	if !ok {
		return nil
	}

	payload, err := json.Marshal(map[string]int64{"post_id": postID})
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bdsmlrAPIEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("ngrok-skip-browser-warning", "true")
	req.Header.Set("User-Agent", userAgent)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		f.log.Debug("bdsmlr media lookup failed", "post_id", postID, "err", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		f.log.Debug("bdsmlr media lookup returned a non-200",
			"post_id", postID,
			"status", resp.StatusCode,
		)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		f.log.Debug("reading bdsmlr media lookup", "post_id", postID, "err", err)
		return nil
	}

	media := parseBdsmlrMedia(body)
	if len(media) == 0 {
		f.log.Debug("bdsmlr post has no usable media", "post_id", postID)
	}
	return media
}

// parseBdsmlrMedia extracts one URL per media item, preferring transcoded
// alternates because the original upload is what tends to disappear.
func parseBdsmlrMedia(body []byte) []Media {
	var detail bdsmlrDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil
	}

	var media []Media
	for _, item := range detail.Post.MediaRepresentation.Items {
		candidate, ok := pickBdsmlrMedia(item)
		if ok {
			media = append(media, candidate)
		}
	}
	if len(media) == 0 {
		return nil
	}
	return dedupeMedia("", media)
}

func pickBdsmlrMedia(item bdsmlrMediaItem) (Media, bool) {
	// Alternates first: for a converted GIF these are the working MP4s.
	for _, alt := range item.Alternates {
		if m, ok := bdsmlrCandidate(alt); ok {
			return m, true
		}
	}
	if item.Original != nil {
		if m, ok := bdsmlrCandidate(*item.Original); ok {
			return m, true
		}
	}
	// A poster or preview is better than posting a text-only message.
	for _, fallback := range []*bdsmlrMedia{item.Poster, item.Preview} {
		if fallback == nil {
			continue
		}
		if m, ok := bdsmlrCandidate(*fallback); ok {
			return m, true
		}
	}
	return Media{}, false
}

func bdsmlrCandidate(m bdsmlrMedia) (Media, bool) {
	raw := strings.TrimSpace(m.URL)
	if raw == "" {
		return Media{}, false
	}
	switch {
	case strings.HasPrefix(m.MimeType, "image/"), strings.HasPrefix(m.MimeType, "video/"):
	case mediaExtension(raw) != "":
	default:
		return Media{}, false
	}
	return Media{URL: raw, Type: m.MimeType}, true
}

// bdsmlrPostID recognizes links such as https://bdsmlr.com/post/877982914.
func bdsmlrPostID(link string) (int64, bool) {
	u, err := url.Parse(link)
	if err != nil {
		return 0, false
	}

	host := strings.ToLower(u.Host)
	if host != "bdsmlr.com" && !strings.HasSuffix(host, ".bdsmlr.com") {
		return 0, false
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "post" {
		return 0, false
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (f *Feature) bdsmlrMediaFor(ctx context.Context, item FeedItem) []Media {
	media := f.resolveBdsmlrMedia(ctx, item.Link)
	if len(media) == 0 {
		return nil
	}
	f.log.Debug("using bdsmlr API media",
		"link", item.Link,
		"count", len(media),
	)
	return media
}
