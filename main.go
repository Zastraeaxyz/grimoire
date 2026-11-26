package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"grimoire/internal/bot"
	"grimoire/internal/config"
	"grimoire/internal/cryptobox"
	"grimoire/internal/features/bdsmlr"
	memberlocks "grimoire/internal/features/chastisafe"
	"grimoire/internal/features/rss"
	"grimoire/internal/features/settings"
	"grimoire/internal/features/stats"
	"grimoire/internal/features/welcome"
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

	// The main bot stores one encrypted ChastiSafe code per member.
	box, boxErr := cryptobox.LoadOrCreate(
		cfg.Chastisafe.StoreKey,
		filepath.Join(filepath.Dir(cfg.DatabasePath), "chastisafe.key"),
	)
	if boxErr != nil {
		log.Error("chastisafe credential store disabled", "err", boxErr)
	}
	memberlocks.New(memberlocks.Options{
		Store:     st,
		Box:       box,
		BaseURL:   cfg.Chastisafe.BaseURL,
		UserAgent: "grimoire-locks/1",
		Logger:    log,
	}).Register(b)

	health := newHealthServer(cfg.Port, log)
	health.start()
	defer health.shutdown()

	if err := b.Run(ctx); err != nil {
		return err
	}
	return nil
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
