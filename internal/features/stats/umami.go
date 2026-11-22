package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	requestTimeout = 15 * time.Second
	maxResponse    = 8 << 20
	userAgent      = "grimoire stats (+https://github.com/Zastraeaxyz/bdsmlr)"
)

// Metric is one ranked row from Umami: a value and its count.
type Metric struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

// WebsiteStats is Umami's summary for one date range. Comparison holds the
// equal-length period that precedes it.
type WebsiteStats struct {
	Pageviews int64 `json:"pageviews"`
	Visitors  int64 `json:"visitors"`
	Visits    int64 `json:"visits"`
	Bounces   int64 `json:"bounces"`
	TotalTime int64 `json:"totaltime"`

	Comparison struct {
		Pageviews int64 `json:"pageviews"`
		Visitors  int64 `json:"visitors"`
		Visits    int64 `json:"visits"`
		Bounces   int64 `json:"bounces"`
		TotalTime int64 `json:"totaltime"`
	} `json:"comparison"`
}

// umamiClient reads one website through the Umami REST API. Only read
// endpoints are used.
type umamiClient struct {
	baseURL   string
	websiteID string
	apiKey    string
	http      *http.Client
}

func newUmamiClient(baseURL, websiteID, apiKey string) *umamiClient {
	return &umamiClient{
		baseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		websiteID: strings.TrimSpace(websiteID),
		apiKey:    strings.TrimSpace(apiKey),
		http:      &http.Client{Timeout: requestTimeout},
	}
}

func (c *umamiClient) stats(ctx context.Context, start, end time.Time) (*WebsiteStats, error) {
	var out WebsiteStats
	if err := c.get(ctx, "stats", rangeParams(start, end), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *umamiClient) metrics(ctx context.Context, dimension string, start, end time.Time, limit int) ([]Metric, error) {
	q := rangeParams(start, end)
	q.Set("type", dimension)
	q.Set("limit", strconv.Itoa(limit))

	var out []Metric
	if err := c.get(ctx, "metrics", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func rangeParams(start, end time.Time) url.Values {
	q := url.Values{}
	q.Set("startAt", strconv.FormatInt(start.UnixMilli(), 10))
	q.Set("endAt", strconv.FormatInt(end.UnixMilli(), 10))
	return q
}

func (c *umamiClient) get(ctx context.Context, action string, q url.Values, out any) error {
	endpoint := fmt.Sprintf("%s/api/websites/%s/%s?%s",
		c.baseURL, url.PathEscape(c.websiteID), action, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("building the umami %s request: %w", action, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("reading umami %s: %w", action, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("reading the umami %s response: %w", action, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("umami %s failed: %s", action, apiError(resp.StatusCode, body))
	}
	if err := decodePayload(body, out); err != nil {
		return fmt.Errorf("decoding umami %s: %w", action, err)
	}
	return nil
}

// decodePayload accepts both a bare JSON value and Umami's newer {data: ...}
// envelope, so a server upgrade cannot silently zero the report.
func decodePayload(body []byte, out any) error {
	var probe struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &probe); err == nil && len(probe.Data) > 0 && bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		return json.Unmarshal(probe.Data, out)
	}
	return json.Unmarshal(body, out)
}

// apiError turns Umami's error body into a short, log-safe message.
func apiError(status int, body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", status, payload.Error.Message)
	}

	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	if text == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, text)
}
