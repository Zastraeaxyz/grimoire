package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config is the runtime configuration loaded from the environment.
type Config struct {
	Token        string
	GuildID      string
	DatabasePath string
	Port         string
	LogLevel     slog.Level

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
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
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
