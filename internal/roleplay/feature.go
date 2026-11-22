// Package roleplay runs the optional persona bot that shares a process with
// Grimoire. It answers mentions and replies from members, optionally limited to
// one Discord role, and can check their ChastiSafe status with a tool.
package roleplay

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"grimoire/internal/ai"
	"grimoire/internal/bot"
	"grimoire/internal/chastisafe"
)

// ChatClient is the model surface the feature needs.
type ChatClient interface {
	Chat(ctx context.Context, system string, messages []ai.Message) (ai.Reply, error)
	ChatWithTools(ctx context.Context, system string, messages []ai.Message, tools []ai.Tool) (ai.Reply, error)
}

// LockSource is the read-only ChastiSafe surface used for status. A nil source
// disables the ChastiSafe block entirely.
type LockSource interface {
	Lockees(ctx context.Context) ([]chastisafe.LoadedLock, error)
	LoadableByKeyholder(ctx context.Context, keyholderID string) ([]chastisafe.Template, error)
}

// Options configures the roleplay feature.
type Options struct {
	GuildID      string
	BotName      string
	LockeeRoleID string
	SystemPrompt string

	ContextMessages int
	MaxChunks       int
	Timeout         time.Duration
	MaxConcurrent   int

	RateLimitCount  int
	RateLimitWindow time.Duration

	Chat      ChatClient
	Locks     LockSource
	Keyholder *chastisafe.Identity
	Log       *slog.Logger
}

// Feature implements the persona bot.
type Feature struct {
	guildID      string
	botName      string
	lockeeRoleID string
	basePrompt   string
	tools        []ai.Tool

	contextMessages int
	maxChunks       int
	timeout         time.Duration

	chat      ChatClient
	locks     LockSource
	keyholder *chastisafe.Identity
	log       *slog.Logger

	sem       chan struct{}
	limiter   *limiter
	lockCache *lockeesCache
	templates *templateCache
	sent      *sentCache
	now       func() time.Time
}

// New builds the roleplay feature. Missing limits fall back to safe defaults.
func New(opts Options) *Feature {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.BotName == "" {
		opts.BotName = "Keyholder"
	}
	if opts.ContextMessages < 1 {
		opts.ContextMessages = 15
	}
	if opts.MaxChunks < 1 {
		opts.MaxChunks = 3
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.MaxConcurrent < 1 {
		opts.MaxConcurrent = 2
	}
	if opts.RateLimitCount < 1 {
		opts.RateLimitCount = 2
	}
	if opts.RateLimitWindow <= 0 {
		opts.RateLimitWindow = 10 * time.Second
	}

	now := time.Now
	prompt := strings.ReplaceAll(opts.SystemPrompt, "{{bot}}", opts.BotName)
	basePrompt := prompt
	var tools []ai.Tool
	if opts.Locks != nil {
		basePrompt += "\n\n" + chastityGuidance
		tools = chastityTools()
	}

	f := &Feature{
		guildID:         opts.GuildID,
		botName:         opts.BotName,
		lockeeRoleID:    opts.LockeeRoleID,
		basePrompt:      basePrompt,
		tools:           tools,
		contextMessages: opts.ContextMessages,
		maxChunks:       opts.MaxChunks,
		timeout:         opts.Timeout,
		chat:            opts.Chat,
		locks:           opts.Locks,
		keyholder:       opts.Keyholder,
		log:             opts.Log,
		sem:             make(chan struct{}, opts.MaxConcurrent),
		limiter:         newLimiter(opts.RateLimitCount, opts.RateLimitWindow, now),
		lockCache:       newLockeesCache(30*time.Second, now),
		templates:       newTemplateCache(5*time.Minute, now),
		sent:            newSentCache(time.Hour, now),
		now:             now,
	}
	return f
}

// Register wires the feature into a bot.
func (f *Feature) Register(b *bot.Bot) {
	if b == nil {
		return
	}
	if f.lockeeRoleID != "" {
		b.AddUserCommand(statusCommand(), f.handleStatusCommand)
	} else {
		b.AddPublicCommand(statusCommand(), f.handleStatusCommand)
	}
	b.AddCommand(promptCommand(), f.handlePromptCommand)
	b.AddMessageHandler(f.handleMessage)
}

// String describes the feature without exposing any configuration secrets.
func (f *Feature) String() string {
	return fmt.Sprintf("roleplay(bot=%q, chastity=%t)", f.botName, f.locks != nil)
}

func (f *Feature) acquire(ctx context.Context) (func(), error) {
	select {
	case f.sem <- struct{}{}:
		return func() { <-f.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("roleplay is busy")
	}
}
