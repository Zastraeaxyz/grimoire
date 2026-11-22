package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatSendsSystemAndMessages(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Fatalf("authorization = %q", auth)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		_, _ = io.WriteString(w, `{
			"choices":[{"message":{"content":"  hello there  "}}],
			"usage":{"prompt_tokens":11,"completion_tokens":4,"total_tokens":15}
		}`)
	}))
	defer server.Close()

	client, err := New(Options{
		APIKey:    "test-key",
		BaseURL:   server.URL,
		Model:     "test/model",
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := client.Chat(context.Background(), "be brief", []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: ""},
		{Role: "", Content: "ignored"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.Text != "hello there" {
		t.Fatalf("text = %q", reply.Text)
	}
	if reply.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v", reply.Usage)
	}

	if got["model"] != "test/model" {
		t.Fatalf("model = %v", got["model"])
	}
	if got["max_tokens"] != float64(64) {
		t.Fatalf("max_tokens = %v", got["max_tokens"])
	}
	messages, ok := got["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", got["messages"])
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be brief" {
		t.Fatalf("first message = %#v", first)
	}
}

func TestChatErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"bad status": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"no key"}}`)
		},
		"empty choices": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[]}`)
		},
		"empty text": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"   "}}]}`)
		},
		"error member": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()

			client, err := New(Options{APIKey: "k", BaseURL: server.URL, Model: "m"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := client.Chat(context.Background(), "", []Message{{Role: "user", Content: "hi"}}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestNewRejectsMissingConfiguration(t *testing.T) {
	if _, err := New(Options{Model: "m"}); err == nil {
		t.Fatal("expected an error for a missing key")
	}
	if _, err := New(Options{APIKey: "k"}); err == nil {
		t.Fatal("expected an error for a missing model")
	}
}

func TestChatSkipsEmptyMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "system") {
			t.Fatalf("request should not include an empty system prompt: %s", body)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	client, err := New(Options{APIKey: "k", BaseURL: server.URL, Model: "m"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Chat(context.Background(), "   ", []Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, err := client.Chat(context.Background(), "", nil); err == nil {
		t.Fatal("expected an error with no messages")
	}
}
