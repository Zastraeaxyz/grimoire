package rss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	redgifsWebBase     = "https://www.redgifs.com"
	redgifsUserAgent   = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"
	redgifsMaxAPIBytes = 1 << 20
	redgifsTokenTTL    = 20 * time.Hour
)

// redgifsAPIBase is the API origin. It is a var so tests can point requests at
// a local server.
var redgifsAPIBase = "https://api.redgifs.com"

var errRedgifsAuth = errors.New("redgifs rejected the API token")

// redgifsURL normalizes a RedGifs watch link. Other destinations return "".
func redgifsURL(raw string) string {
	id := redgifsID(raw)
	if id == "" {
		return ""
	}
	return redgifsWebBase + "/watch/" + url.PathEscape(id)
}

// redgifsID returns the id in a RedGifs watch link.
func redgifsID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "redgifs.com", "www.redgifs.com", "v3.redgifs.com":
	default:
		return ""
	}
	parts := pathSegments(u.Path)
	if len(parts) < 2 {
		return ""
	}
	switch strings.ToLower(parts[0]) {
	case "watch", "ifr":
	default:
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// redgifsGif is the subset of a gif's API representation the bot needs.
type redgifsGif struct {
	URLs struct {
		HD string `json:"hd"`
		SD string `json:"sd"`
	} `json:"urls"`
}

// redgifsMediaFor downloads a RedGifs post's MP4. Discord only gives
// user-posted RedGifs links its click-to-play frame; bot posts get the static
// OpenGraph card, so the video is attached instead. A failure returns nil,
// which keeps the listing's still frame.
func (f *Feature) redgifsMediaFor(ctx context.Context, item FeedItem) []downloadedMedia {
	if item.RedgifsURL == "" {
		return nil
	}
	id := redgifsID(item.RedgifsURL)
	if id == "" {
		return nil
	}

	gif, err := f.redgifsGif(ctx, id, false)
	if err != nil {
		f.log.Warn("resolving redgifs video", "id", id, "err", err)
		return nil
	}

	// Prefer HD and fall back to SD, which usually fits the attachment cap.
	for _, raw := range []string{gif.URLs.HD, gif.URLs.SD} {
		raw = strings.TrimSpace(html.UnescapeString(raw))
		if raw == "" {
			continue
		}
		data, contentType, err := f.downloadOneAs(ctx, raw, f.maxAttachmentBytes, redgifsUserAgent)
		if err != nil {
			f.log.Debug("skipping redgifs video", "id", id, "err", err)
			continue
		}
		return []downloadedMedia{{
			FileName:    "media-1" + extensionFor(contentType, raw),
			ContentType: contentType,
			Data:        data,
		}}
	}
	return nil
}

// redgifsGif fetches one post's metadata. An expired token is refreshed once.
func (f *Feature) redgifsGif(ctx context.Context, id string, refresh bool) (redgifsGif, error) {
	token, err := f.redgifsAccessToken(ctx, refresh)
	if err != nil {
		return redgifsGif{}, err
	}

	endpoint := redgifsAPIBase + "/v2/gifs/" + url.PathEscape(id)
	body, err := f.redgifsAPIRequest(ctx, endpoint, token)
	if errors.Is(err, errRedgifsAuth) && !refresh {
		return f.redgifsGif(ctx, id, true)
	}
	if err != nil {
		return redgifsGif{}, err
	}

	var gif redgifsGif
	if err := json.Unmarshal(body, &gif); err != nil {
		return redgifsGif{}, fmt.Errorf("reading redgifs post: %w", err)
	}
	return gif, nil
}

// redgifsAccessToken returns a cached temporary token, fetching a new one when
// it is missing, expired, or rejected.
func (f *Feature) redgifsAccessToken(ctx context.Context, refresh bool) (string, error) {
	f.redgifsMu.Lock()
	defer f.redgifsMu.Unlock()

	if !refresh && f.redgifsToken != "" && time.Now().Before(f.redgifsTokenExpiry) {
		return f.redgifsToken, nil
	}

	body, err := f.redgifsAPIRequest(ctx, redgifsAPIBase+"/v2/auth/temporary", "")
	if err != nil {
		return "", err
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("reading redgifs token: %w", err)
	}
	token := strings.TrimSpace(payload.Token)
	if token == "" {
		return "", errors.New("redgifs returned no token")
	}

	f.redgifsToken = token
	f.redgifsTokenExpiry = time.Now().Add(redgifsTokenTTL)
	return token, nil
}

// redgifsAPIRequest performs one JSON request against the RedGifs API.
func (f *Feature) redgifsAPIRequest(ctx context.Context, endpoint, token string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building redgifs request: %w", err)
	}
	req.Header.Set("User-Agent", redgifsUserAgent)
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching redgifs: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, errRedgifsAuth
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("redgifs returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, redgifsMaxAPIBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading redgifs response: %w", err)
	}
	if int64(len(body)) > redgifsMaxAPIBytes {
		return nil, fmt.Errorf("redgifs response is larger than %d bytes", redgifsMaxAPIBytes)
	}
	return body, nil
}
