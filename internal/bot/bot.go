package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Options configures a Bot.
type Options struct {
	Token   string
	GuildID string
	Log     *slog.Logger
}

// CommandHandler runs a slash command. Handlers are responsible for
// acknowledging the interaction.
type CommandHandler func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error

// ComponentHandler runs a message component interaction (buttons, selects).
type ComponentHandler func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error

// AutocompleteHandler answers suggestions for a slash command option.
type AutocompleteHandler func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error

// ReadyHook runs after every gateway READY event.
type ReadyHook func(ctx context.Context, s *discordgo.Session) error

// Bot owns the Discord session and dispatches interactions to features.
type Bot struct {
	Session *discordgo.Session
	Log     *slog.Logger

	guildID string

	mu                sync.RWMutex
	commands          map[string]*discordgo.ApplicationCommand
	commandHandlers   map[string]CommandHandler
	autocompletes     map[string]AutocompleteHandler
	componentHandlers map[string]ComponentHandler
	readyHooks        []ReadyHook
	commandsSynced    bool
}

// New creates a bot session. The token is validated against the REST API in
// Run so an invalid token fails fast instead of reconnect-looping.
func New(opts Options) (*Bot, error) {
	if opts.Token == "" {
		return nil, errors.New("discord token is empty")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}

	s, err := discordgo.New("Bot " + opts.Token)
	if err != nil {
		return nil, fmt.Errorf("creating discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuilds
	s.LogLevel = discordgo.LogWarning
	s.ShouldReconnectOnError = true

	b := &Bot{
		Session:           s,
		Log:               opts.Log,
		guildID:           opts.GuildID,
		commands:          make(map[string]*discordgo.ApplicationCommand),
		commandHandlers:   make(map[string]CommandHandler),
		autocompletes:     make(map[string]AutocompleteHandler),
		componentHandlers: make(map[string]ComponentHandler),
	}
	s.AddHandler(b.handleReady)
	s.AddHandler(b.handleInteraction)
	return b, nil
}

// AddCommand registers a slash command and its handler.
func (b *Bot) AddCommand(cmd *discordgo.ApplicationCommand, handler CommandHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commands[cmd.Name] = cmd
	b.commandHandlers[cmd.Name] = handler
}

// AddComponent registers a component handler for a custom ID namespace, for
// example "welcome" handles "welcome:ack". Namespacing keeps buttons working
// across restarts without any in-memory routing state.
func (b *Bot) AddComponent(namespace string, handler ComponentHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.componentHandlers[namespace] = handler
}

// AddAutocomplete registers suggestions for a command's options.
func (b *Bot) AddAutocomplete(commandName string, handler AutocompleteHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.autocompletes[commandName] = handler
}

// AddReadyHook registers work that must be re-validated on every reconnect.
func (b *Bot) AddReadyHook(hook ReadyHook) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.readyHooks = append(b.readyHooks, hook)
}

// Run validates the token, connects the gateway, and blocks until ctx is done.
func (b *Bot) Run(ctx context.Context) error {
	me, err := b.Session.User("@me")
	if err != nil {
		return fmt.Errorf("validating bot token: %w", err)
	}
	b.Log.Info("authenticated with discord",
		"user", me.Username,
		"user_id", me.ID,
		"guild_id", b.guildID,
	)

	if err := b.Session.Open(); err != nil {
		return fmt.Errorf("opening gateway: %w", err)
	}
	defer b.Session.Close()
	b.Log.Info("gateway connected")

	<-ctx.Done()
	b.Log.Info("gateway shutting down")
	return nil
}

func (b *Bot) handleReady(s *discordgo.Session, r *discordgo.Ready) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b.Log.Info("discord ready",
		"user", r.User.Username,
		"guilds", len(r.Guilds),
	)

	if b.needsCommandSync() {
		if err := b.syncCommands(s); err != nil {
			// Leaving the flag unset retries on the next READY event.
			b.Log.Error("syncing application commands", "err", err)
		} else {
			b.mu.Lock()
			b.commandsSynced = true
			b.mu.Unlock()
		}
	}

	b.mu.RLock()
	hooks := append([]ReadyHook(nil), b.readyHooks...)
	b.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(ctx, s); err != nil {
			b.Log.Error("ready hook failed", "err", err)
		}
	}
}

func (b *Bot) needsCommandSync() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return !b.commandsSynced
}

func (b *Bot) syncCommands(s *discordgo.Session) error {
	b.mu.RLock()
	cmds := make([]*discordgo.ApplicationCommand, 0, len(b.commands))
	for _, cmd := range b.commands {
		cmds = append(cmds, cmd)
	}
	b.mu.RUnlock()

	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })

	appID := s.State.User.ID
	if b.guildID != "" {
		_, err := s.ApplicationCommandBulkOverwrite(appID, b.guildID, cmds)
		if err != nil {
			return fmt.Errorf("overwriting guild commands: %w", err)
		}
		b.Log.Info("registered guild commands", "count", len(cmds), "guild_id", b.guildID)
		return nil
	}

	_, err := s.ApplicationCommandBulkOverwrite(appID, "", cmds)
	if err != nil {
		return fmt.Errorf("overwriting global commands: %w", err)
	}
	b.Log.Info("registered global commands", "count", len(cmds))
	return nil
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionPing &&
		i.Type != discordgo.InteractionApplicationCommand &&
		i.Type != discordgo.InteractionApplicationCommandAutocomplete &&
		i.Type != discordgo.InteractionMessageComponent {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch i.Type {
	case discordgo.InteractionPing:
		respondErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponsePong,
		})
		if respondErr != nil {
			b.Log.Warn("replying to ping", "err", respondErr)
		}

	case discordgo.InteractionApplicationCommand:
		name := i.ApplicationCommandData().Name
		b.mu.RLock()
		handler := b.commandHandlers[name]
		b.mu.RUnlock()
		if handler == nil {
			b.Log.Warn("unhandled command", "command", name)
			return
		}
		if err := handler(ctx, s, i); err != nil {
			b.handleError(s, i, err)
		}

	case discordgo.InteractionApplicationCommandAutocomplete:
		name := i.ApplicationCommandData().Name
		b.mu.RLock()
		handler := b.autocompletes[name]
		b.mu.RUnlock()
		if handler == nil {
			b.Log.Warn("unhandled autocomplete", "command", name)
			return
		}
		if err := handler(ctx, s, i); err != nil {
			b.Log.Error("autocomplete failed", "command", name, "err", err)
		}

	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		b.mu.RLock()
		handler := b.componentHandlers[customID]
		if handler == nil {
			if idx := strings.Index(customID, ":"); idx > 0 {
				handler = b.componentHandlers[customID[:idx]]
			}
		}
		b.mu.RUnlock()
		if handler == nil {
			b.Log.Warn("unhandled component", "custom_id", customID)
			return
		}
		if err := handler(ctx, s, i); err != nil {
			b.handleError(s, i, err)
		}
	}
}

func (b *Bot) handleError(s *discordgo.Session, i *discordgo.InteractionCreate, err error) {
	b.Log.Error("interaction failed",
		"type", i.Type.String(),
		"err", err,
	)

	msg := "Something went wrong on my side. Try again in a moment."
	if editErr := EditEphemeral(s, i, msg); editErr == nil {
		return
	}
	if _, followErr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: msg,
		Flags:   discordgo.MessageFlagsEphemeral,
	}); followErr == nil {
		return
	}
	if replyErr := ReplyEphemeral(s, i, msg); replyErr != nil {
		b.Log.Warn("could not report interaction error to user", "err", replyErr)
	}
}
