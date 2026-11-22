package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestHandleMessageCreateFansOut(t *testing.T) {
	b := newTestBot()
	seen := make(chan string, 2)
	b.AddMessageHandler(func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) error {
		if err := ctx.Err(); err != nil {
			t.Errorf("handler got a cancelled context: %v", err)
		}
		seen <- "first:" + m.ID
		return nil
	})
	b.AddMessageHandler(func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) error {
		seen <- "second:" + m.ID
		return errors.New("boom")
	})

	b.handleMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "m1"}})

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case value := <-seen:
			got[value] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for message handlers")
		}
	}
	if !got["first:m1"] || !got["second:m1"] {
		t.Fatalf("handlers saw %v", got)
	}
}

func TestHandleMessageCreateIgnoresNil(t *testing.T) {
	b := newTestBot()
	called := make(chan struct{}, 1)
	b.AddMessageHandler(func(context.Context, *discordgo.Session, *discordgo.MessageCreate) error {
		called <- struct{}{}
		return nil
	})

	b.handleMessageCreate(nil, nil)
	b.handleMessageCreate(nil, &discordgo.MessageCreate{})
	select {
	case <-called:
		t.Fatal("handler ran for an empty message")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestAddMessageHandlerIgnoresNil(t *testing.T) {
	b := newTestBot()
	b.AddMessageHandler(nil)
	if len(b.messageHandlers) != 0 {
		t.Fatalf("handlers = %d", len(b.messageHandlers))
	}
}

func TestHandleMessageCreateRecoversPanics(t *testing.T) {
	var buf strings.Builder
	b := &Bot{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	b.AddMessageHandler(func(context.Context, *discordgo.Session, *discordgo.MessageCreate) error {
		panic("broken handler")
	})

	b.handleMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "m1"}})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "panicked") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("log = %q, want a panic entry", buf.String())
}

func newTestBot() *Bot {
	return &Bot{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestOptionsIntents(t *testing.T) {
	b, err := New(Options{Token: "token", Intents: discordgo.IntentsGuildMessages | discordgo.IntentMessageContent})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := discordgo.IntentsGuildMessages | discordgo.IntentMessageContent
	if b.Session.Identify.Intents != want {
		t.Fatalf("intents = %v, want %v", b.Session.Identify.Intents, want)
	}

	defaultBot, err := New(Options{Token: "token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if defaultBot.Session.Identify.Intents != discordgo.IntentsGuilds {
		t.Fatalf("default intents = %v", defaultBot.Session.Identify.Intents)
	}
}
