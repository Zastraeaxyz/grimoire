package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/ai"
	"grimoire/internal/bot"
	"grimoire/internal/chastisafe"
	"grimoire/internal/config"
	"grimoire/internal/features/bdsmlr"
	"grimoire/internal/features/rss"
	"grimoire/internal/features/settings"
	"grimoire/internal/features/stats"
	"grimoire/internal/features/welcome"
	"grimoire/internal/roleplay"
	"grimoire/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(log)

	log.Info("starting grimoire",
		"database", cfg.DatabasePath,
		"guild_id", cfg.GuildID,
	)

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	seedCtx, cancelSeed := context.WithTimeout(ctx, 10*time.Second)
	if err := seedSettings(seedCtx, cfg, st, log); err != nil {
		log.Warn("seeding welcome settings", "err", err)
	}
	cancelSeed()

	b, err := bot.New(bot.Options{
		Token:   cfg.Token,
		GuildID: cfg.GuildID,
		Log:     log,
	})
	if err != nil {
		return err
	}

	b.SetUserRoleLookup(func(ctx context.Context) (string, error) {
		guildSettings, err := st.GetGuildSettings(ctx, cfg.GuildID)
		if err != nil {
			return "", err
		}
		if guildSettings == nil {
			return "", nil
		}
		return guildSettings.UserCommandRoleID, nil
	})

	welcome.New(cfg.GuildID, st, log).Register(b)
	settings.New(cfg.GuildID, st, log).Register(b)
	rss.New(ctx, rss.Options{
		GuildID:            cfg.GuildID,
		Store:              st,
		Log:                log,
		PollInterval:       cfg.RSSPollInterval,
		MaxAttachmentBytes: cfg.RSSMaxAttachmentBytes,
		RedditSession:      cfg.RedditSession,
		RedditUserAgent:    cfg.RedditUserAgent,
		RedditIncludeNSFW:  cfg.RedditIncludeNSFW,
	}).Register(b)

	bdsmlr.New(bdsmlr.Options{
		GuildID: cfg.GuildID,
		Session: cfg.BDSMLRSession,
		Store:   st,
		Log:     log,
	}).Register(b)

	stats.New(ctx, stats.Options{
		GuildID:        cfg.GuildID,
		Store:          st,
		Log:            log,
		SiteURL:        cfg.StatsSiteURL,
		Timezone:       cfg.StatsTimezone,
		Times:          cfg.StatsTimes,
		UmamiURL:       cfg.UmamiURL,
		UmamiWebsiteID: cfg.UmamiWebsiteID,
		UmamiAPIKey:    cfg.UmamiAPIKey,
	}).Register(b)

	if cfg.Roleplay.Enabled() {
		if err := startRoleplay(ctx, cfg, log); err != nil {
			// The persona is optional. A failure keeps Grimoire running.
			log.Error("roleplay bot not started", "err", err)
		}
	}

	health := newHealthServer(cfg.Port, log)
	health.start()
	defer health.shutdown()

	if err := b.Run(ctx); err != nil {
		return err
	}
	return nil
}

// startRoleplay connects the optional persona bot. A missing prompt or an
// invalid ChastiSafe code disables only the persona.
func startRoleplay(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	rp := cfg.Roleplay

	if rp.LockeeRoleID != "" {
		if _, err := strconv.ParseUint(rp.LockeeRoleID, 10, 64); err != nil {
			return fmt.Errorf("ROLEPLAY_LOCKEE_ROLE_ID %q is not a valid Discord snowflake", rp.LockeeRoleID)
		}
	}
	if rp.GuildID != "" {
		if _, err := strconv.ParseUint(rp.GuildID, 10, 64); err != nil {
			return fmt.Errorf("ROLEPLAY_GUILD_ID %q is not a valid Discord snowflake", rp.GuildID)
		}
	}
	if cfg.OpenRouterAPIKey == "" {
		return errors.New("OPENROUTER_API_KEY is required when DISCORD_ROLEPLAY_TOKEN is set")
	}

	prompt, err := loadRoleplayPrompt(rp)
	if err != nil {
		return err
	}

	chat, err := ai.New(ai.Options{
		APIKey:    cfg.OpenRouterAPIKey,
		BaseURL:   cfg.OpenRouterBaseURL,
		Model:     cfg.OpenRouterModel,
		AppName:   "Grimoire",
		MaxTokens: 1024,
		Timeout:   rp.Timeout,
	})
	if err != nil {
		return fmt.Errorf("building the roleplay chat client: %w", err)
	}

	var (
		locks     roleplay.LockSource
		keyholder *chastisafe.Identity
	)
	switch {
	case !rp.Chastity.Enabled:
		log.Info("chastisafe status disabled by configuration")
	case rp.Chastity.KeyholderCode == "":
		log.Warn("chastisafe status disabled: CHASTISAFE_KEYHOLDER_CODE is empty")
	default:
		client, err := chastisafe.New(chastisafe.Options{
			Token:     rp.Chastity.KeyholderCode,
			BaseURL:   rp.Chastity.BaseURL,
			UserAgent: "grimoire-roleplay/1",
			Logger:    log,
		})
		if err != nil {
			log.Warn("chastisafe status disabled", "err", err)
			break
		}

		whoCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		identity, whoErr := client.Who(whoCtx)
		cancel()
		if whoErr != nil {
			log.Warn("chastisafe status disabled: cannot load the keyholder account", "err", whoErr)
			break
		}

		locks = client
		keyholder = identity
		log.Info("chastisafe keyholder ready",
			"keyholder", identity.User,
			"keyholder_id", identity.ID.String(),
		)
	}

	b, err := bot.New(bot.Options{
		Token:   rp.Token,
		GuildID: rp.GuildID,
		Log:     log.With("bot", "roleplay"),
		Intents: discordgo.IntentsGuilds |
			discordgo.IntentsGuildMessages |
			discordgo.IntentMessageContent,
	})
	if err != nil {
		return fmt.Errorf("creating the roleplay bot: %w", err)
	}

	roleplay.New(roleplay.Options{
		GuildID:         rp.GuildID,
		BotName:         rp.BotName,
		LockeeRoleID:    rp.LockeeRoleID,
		SystemPrompt:    prompt,
		ContextMessages: rp.ContextMessages,
		MaxChunks:       rp.MaxChunks,
		Timeout:         rp.Timeout,
		MaxConcurrent:   rp.MaxConcurrent,
		RateLimitCount:  rp.RateLimitCount,
		RateLimitWindow: rp.RateLimitWindow,
		Chat:            chat,
		Locks:           locks,
		Keyholder:       keyholder,
		Log:             log.With("bot", "roleplay"),
	}).Register(b)

	b.SetUserRoleLookup(func(context.Context) (string, error) {
		return rp.LockeeRoleID, nil
	})

	log.Info("roleplay bot starting",
		"name", rp.BotName,
		"model", cfg.OpenRouterModel,
		"chastity", locks != nil,
	)
	go func() {
		if err := b.Run(ctx); err != nil {
			log.Error("roleplay bot stopped", "err", err)
		}
	}()
	return nil
}

// loadRoleplayPrompt prefers the environment override and otherwise reads the
// persona file from disk.
func loadRoleplayPrompt(rp config.RoleplayConfig) (string, error) {
	if prompt := strings.TrimSpace(rp.SystemPrompt); prompt != "" {
		return prompt, nil
	}

	data, err := os.ReadFile(rp.SystemPromptFile)
	if err != nil {
		return "", fmt.Errorf("reading the roleplay system prompt %q: %w", rp.SystemPromptFile, err)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", fmt.Errorf("the roleplay system prompt %q is empty", rp.SystemPromptFile)
	}
	return prompt, nil
}

// seedSettings lets a fresh install boot with env configuration before an
// admin runs /welcome setup. Existing database configuration always wins.
func seedSettings(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) error {
	welcomeSeed := cfg.WelcomeChannelID != "" && cfg.BaseRoleID != ""
	if !welcomeSeed && cfg.StatsChannelID == "" {
		return nil
	}

	existing, err := st.GetGuildSettings(ctx, cfg.GuildID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	settings := &store.GuildSettings{
		GuildID:        cfg.GuildID,
		StatsChannelID: cfg.StatsChannelID,
	}
	if welcomeSeed {
		settings.WelcomeChannelID = cfg.WelcomeChannelID
		settings.BaseRoleID = cfg.BaseRoleID
	}
	if err := st.SaveGuildSettings(ctx, settings); err != nil {
		return err
	}
	log.Info("seeded settings from environment",
		"welcome_channel_id", settings.WelcomeChannelID,
		"base_role_id", settings.BaseRoleID,
		"stats_channel_id", settings.StatsChannelID,
	)
	return nil
}

type healthServer struct {
	srv *http.Server
	log *slog.Logger
}

func newHealthServer(port string, log *slog.Logger) *healthServer {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "grimoire is running\n")
	})

	return &healthServer{
		srv: &http.Server{
			Addr:              ":" + port,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		log: log,
	}
}

func (h *healthServer) start() {
	go func() {
		if err := h.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.log.Warn("health server stopped", "err", err)
		}
	}()
}

func (h *healthServer) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != nil {
		h.log.Warn("shutting down health server", "err", err)
	}
}
