package chastisafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the API root used by the ChastiSafe web app.
	DefaultBaseURL = "https://chastisafe.com/api-v1"

	defaultTimeout     = 20 * time.Second
	defaultMaxResponse = 4 << 20
	defaultUserAgent   = "grimoire-chastisafe/1"
	authHeader         = "chastisafedev"
	maxErrorBodyChars  = 400
)

// Options configures a Client.
type Options struct {
	// Token is the account's ChastiSafe developer code. It is required.
	Token string

	// BaseURL overrides the API root. It is primarily for tests.
	BaseURL string

	// HTTPClient overrides the HTTP client. The default has a 20s timeout.
	HTTPClient *http.Client

	// UserAgent identifies this bot to ChastiSafe.
	UserAgent string

	// Logger receives debug request lines. Bodies and tokens are never logged.
	Logger *slog.Logger

	// MaxResponseBytes caps a single response. Zero uses 4 MiB.
	MaxResponseBytes int64
}

// LogValue keeps the token out of structured logs.
func (o Options) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("base_url", o.BaseURL),
		slog.String("user_agent", o.UserAgent),
		slog.Int64("max_response_bytes", o.MaxResponseBytes),
	)
}

// Client talks to one ChastiSafe account.
//
// Clients are immutable after construction and safe for concurrent use. The
// HTTP client is shared by copies made with WithToken.
type Client struct {
	baseURL   string
	token     string
	http      *http.Client
	userAgent string
	log       *slog.Logger
	maxBody   int64
}

// New builds a client for one ChastiSafe account.
func New(opts Options) (*Client, error) {
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return nil, errors.New("chastisafe: token is empty")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("chastisafe: invalid base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("chastisafe: base URL %q must be an absolute http or https URL", baseURL)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	maxBody := opts.MaxResponseBytes
	if maxBody <= 0 {
		maxBody = defaultMaxResponse
	}

	return &Client{
		baseURL:   baseURL,
		token:     token,
		http:      httpClient,
		userAgent: userAgent,
		log:       log,
		maxBody:   maxBody,
	}, nil
}

// WithToken returns a copy of the client bound to another account. The HTTP
// client, logger and limits are shared.
func (c *Client) WithToken(token string) *Client {
	if c == nil {
		return nil
	}
	clone := *c
	clone.token = strings.TrimSpace(token)
	return &clone
}

// BaseURL is the API root this client uses.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// String redacts the account token.
func (c *Client) String() string {
	if c == nil {
		return "chastisafe.Client(nil)"
	}
	return fmt.Sprintf("chastisafe.Client{baseURL:%q, token:[redacted]}", c.baseURL)
}

// LogValue keeps the token out of structured logs.
func (c *Client) LogValue() slog.Value { return slog.StringValue(c.String()) }

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

func (c *Client) post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, body)
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	if c == nil {
		return nil, errors.New("chastisafe: nil client")
	}
	if c.token == "" {
		return nil, ErrUnauthorized
	}
	if c.http == nil {
		return nil, errors.New("chastisafe: client has no HTTP client")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("chastisafe: encoding %s %s: %w", method, path, err)
		}
		payload = bytes.NewReader(encoded)
	}

	endpoint := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("chastisafe: building %s %s: %w", method, path, err)
	}
	req.Header.Set(authHeader, c.token)
	// The live service accepts the code in the chastisafe cookie even when a
	// header-only request fails. Send both so either path authenticates.
	req.Header.Set("Cookie", "chastisafe="+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chastisafe: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	limit := c.maxBody
	if limit <= 0 {
		limit = defaultMaxResponse
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("chastisafe: reading %s %s: %w", method, path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("chastisafe: %s %s returned more than %d bytes", method, path, limit)
	}

	if c.log != nil {
		c.log.Debug("chastisafe request",
			"method", method,
			"path", path,
			"status", resp.StatusCode,
			"duration", time.Since(start),
		)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newAPIError(method+" "+path, resp.StatusCode, resp.Header, data)
	}
	return data, nil
}

func decodeJSON(data []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}
