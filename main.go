package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grimoire/internal/bot"
	"grimoire/internal/config"
	"grimoire/internal/features/rss"
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

	welcome.New(cfg.GuildID, st, log).Register(b)
	rss.New(ctx, rss.Options{
		GuildID:            cfg.GuildID,
		Store:              st,
		Log:                log,
		PollInterval:       cfg.RSSPollInterval,
		MaxAttachmentBytes: cfg.RSSMaxAttachmentBytes,
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
	if cfg.WelcomeChannelID == "" || cfg.BaseRoleID == "" {
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
		GuildID:          cfg.GuildID,
		WelcomeChannelID: cfg.WelcomeChannelID,
		BaseRoleID:       cfg.BaseRoleID,
	}
	if err := st.SaveGuildSettings(ctx, settings); err != nil {
		return err
	}
	log.Info("seeded welcome settings from environment",
		"channel_id", cfg.WelcomeChannelID,
		"role_id", cfg.BaseRoleID,
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
