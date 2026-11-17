package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the runtime configuration loaded from the environment.
type Config struct {
	Token        string
	GuildID      string
	DatabasePath string
	Port         string
	LogLevel     slog.Level

	// RSS polling knobs.
	RSSPollInterval       time.Duration
	RSSMaxAttachmentBytes int64

	// Optional seed values for the first boot. Once configured through
	// /welcome setup the database is the source of truth.
	WelcomeChannelID string
	BaseRoleID       string
}

// Load reads configuration from .env (when present) and the environment.
func Load() (*Config, error) {
	// Real environment variables win over .env values.
	_ = godotenv.Load()

	cfg := &Config{
		Token:            strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
		GuildID:          strings.TrimSpace(os.Getenv("GUILD_ID")),
		DatabasePath:     envOr("DATABASE_PATH", "./data/grimoire.db"),
		Port:             envOr("PORT", "8080"),
		LogLevel:         parseLevel(os.Getenv("LOG_LEVEL")),
		WelcomeChannelID: strings.TrimSpace(os.Getenv("WELCOME_CHANNEL_ID")),
		BaseRoleID:       strings.TrimSpace(os.Getenv("BASE_ROLE_ID")),
	}

	interval, err := time.ParseDuration(envOr("RSS_POLL_INTERVAL", "1h"))
	if err != nil {
		return nil, fmt.Errorf("RSS_POLL_INTERVAL %q is not a duration: %w", os.Getenv("RSS_POLL_INTERVAL"), err)
	}
	cfg.RSSPollInterval = interval

	maxBytes, err := parseBytes(envOr("RSS_MAX_ATTACHMENT_BYTES", "10485760"))
	if err != nil {
		return nil, fmt.Errorf("RSS_MAX_ATTACHMENT_BYTES: %w", err)
	}
	cfg.RSSMaxAttachmentBytes = maxBytes

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Token == "" {
		return errors.New("DISCORD_TOKEN is required")
	}
	if c.GuildID == "" {
		return errors.New("GUILD_ID is required")
	}
	if _, err := strconv.ParseUint(c.GuildID, 10, 64); err != nil {
		return fmt.Errorf("GUILD_ID %q is not a valid Discord snowflake", c.GuildID)
	}
	if c.DatabasePath == "" {
		return errors.New("DATABASE_PATH must not be empty")
	}
	if c.Port == "" {
		return errors.New("PORT must not be empty")
	}
	if c.RSSPollInterval < time.Minute {
		return errors.New("RSS_POLL_INTERVAL must be at least 1m")
	}
	if c.RSSMaxAttachmentBytes < 1<<20 {
		return errors.New("RSS_MAX_ATTACHMENT_BYTES must be at least 1048576")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// parseBytes accepts a plain byte count or a KiB/MiB suffix such as "10MiB".
func parseBytes(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	multiplier := int64(1)

	lower := strings.ToLower(value)
	switch {
	case strings.HasSuffix(lower, "mib"):
		multiplier = 1 << 20
		value = value[:len(value)-3]
	case strings.HasSuffix(lower, "kib"):
		multiplier = 1 << 10
		value = value[:len(value)-3]
	case strings.HasSuffix(lower, "mb"):
		multiplier = 1 << 20
		value = value[:len(value)-2]
	case strings.HasSuffix(lower, "kb"):
		multiplier = 1 << 10
		value = value[:len(value)-2]
	}

	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q is not a positive byte count", raw)
	}
	return n * multiplier, nil
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
