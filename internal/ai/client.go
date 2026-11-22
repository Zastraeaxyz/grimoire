// Package ai provides the OpenRouter chat client used by the roleplay bot.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is OpenRouter's OpenAI-compatible API root.
	DefaultBaseURL = "https://openrouter.ai/api/v1"

	defaultTimeout     = 60 * time.Second
	defaultMaxResponse = 1 << 20
	maxErrorBodyChars  = 400
)

// Message is one chat turn in an OpenAI-compatible request.
type Message struct {
	Role    string
	Content string
}

// Usage reports token usage for a request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Reply is a completed chat request.
type Reply struct {
	Text  string
	Usage Usage
}

// Options configures a Client.
type Options struct {
	// APIKey is the OpenRouter key. It is required.
	APIKey string

	// BaseURL overrides the API root. It is primarily for tests.
	BaseURL string

	// Model is the OpenRouter model slug. It is required.
	Model string

	// AppName and AppURL are optional OpenRouter attribution headers.
	AppName string
	AppURL  string

	// MaxTokens caps the completion. Zero leaves the choice to the model.
	MaxTokens int

	// Timeout caps a single request. Zero uses 60 seconds.
	Timeout time.Duration

	// HTTPClient overrides the HTTP client.
	HTTPClient *http.Client
}

// Client calls the OpenRouter chat completions endpoint.
type Client struct {
	apiKey    string
	baseURL   string
	model     string
	appName   string
	appURL    string
	maxTokens int
	http      *http.Client
}

// New builds a chat client.
func New(opts Options) (*Client, error) {
	apiKey := strings.TrimSpace(opts.APIKey)
	if apiKey == "" {
		return nil, errors.New("ai: API key is empty")
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return nil, errors.New("ai: model is empty")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return nil, fmt.Errorf("ai: base URL %q must be absolute", baseURL)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}

	return &Client{
		apiKey:    apiKey,
		baseURL:   baseURL,
		model:     model,
		appName:   strings.TrimSpace(opts.AppName),
		appURL:    strings.TrimSpace(opts.AppURL),
		maxTokens: opts.MaxTokens,
		http:      httpClient,
	}, nil
}

// Model returns the configured model slug.
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

type requestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string           `json:"model"`
	Messages  []requestMessage `json:"messages"`
	MaxTokens int              `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends a system prompt and conversation to the model. An empty system
// prompt is allowed.
func (c *Client) Chat(ctx context.Context, system string, messages []Message) (Reply, error) {
	if c == nil || c.http == nil {
		return Reply{}, errors.New("ai: client is disabled")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	wire := make([]requestMessage, 0, len(messages)+1)
	if text := strings.TrimSpace(system); text != "" {
		wire = append(wire, requestMessage{Role: "system", Content: text})
	}
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		content := strings.TrimSpace(message.Content)
		if role == "" || content == "" {
			continue
		}
		wire = append(wire, requestMessage{Role: role, Content: content})
	}
	if len(wire) == 0 {
		return Reply{}, errors.New("ai: no messages to send")
	}

	body, err := json.Marshal(chatRequest{
		Model:     c.model,
		Messages:  wire,
		MaxTokens: c.maxTokens,
	})
	if err != nil {
		return Reply{}, fmt.Errorf("ai: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Reply{}, fmt.Errorf("ai: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.appURL != "" {
		req.Header.Set("HTTP-Referer", c.appURL)
	}
	if c.appName != "" {
		req.Header.Set("X-Title", c.appName)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Reply{}, fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, defaultMaxResponse))
	if err != nil {
		return Reply{}, fmt.Errorf("ai: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Reply{}, fmt.Errorf("ai: status %d: %s", resp.StatusCode, summarizeError(data))
	}

	var decoded chatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return Reply{}, fmt.Errorf("ai: decoding response: %w", err)
	}
	if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
		return Reply{}, fmt.Errorf("ai: %s", strings.TrimSpace(decoded.Error.Message))
	}
	if len(decoded.Choices) == 0 {
		return Reply{}, errors.New("ai: model returned no choices")
	}

	text := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if text == "" {
		return Reply{}, errors.New("ai: model returned an empty reply")
	}
	return Reply{Text: text, Usage: decoded.Usage}, nil
}

func summarizeError(data []byte) string {
	text := strings.Join(strings.Fields(string(data)), " ")
	if text == "" {
		return "empty error body"
	}
	runes := []rune(text)
	if len(runes) > maxErrorBodyChars {
		text = string(runes[:maxErrorBodyChars]) + "..."
	}
	return text
}
