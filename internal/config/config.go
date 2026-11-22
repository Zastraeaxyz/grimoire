package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
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

	// Reddit access. Reddit requires a logged-in session for its listing
	// pages, so the bot reads them with a reddit_session cookie.
	RedditSession     string
	RedditUserAgent   string
	RedditIncludeNSFW bool

	// BDSMLR account site access. Invitations use the WordPress cookies from
	// a signed-in account.bdsmlr.com session.
	BDSMLRSession string

	// Umami analytics for the stats reports. The feature is disabled unless
	// the URL, website ID, and API key are all set.
	UmamiURL       string
	UmamiWebsiteID string
	UmamiAPIKey    string

	// Stats report formatting and schedule.
	StatsSiteURL  string
	StatsTimezone string
	StatsTimes    []string

	// Optional seed values for the first boot. Once configured through
	// /welcome setup the database is the source of truth.
	WelcomeChannelID string
	BaseRoleID       string
	StatsChannelID   string

	// OpenRouter is shared by AI features. Only the roleplay bot needs it for
	// now.
	OpenRouterAPIKey  string
	OpenRouterModel   string
	OpenRouterBaseURL string

	// Roleplay is the optional second Discord bot. It stays offline when its
	// token is empty.
	Roleplay RoleplayConfig

	// Chastisafe configures the main bot's per-member ChastiSafe login.
	Chastisafe ChastisafeConfig
}

// ChastisafeConfig configures the main bot's member-scoped ChastiSafe access.
type ChastisafeConfig struct {
	BaseURL  string
	StoreKey string
}

// RoleplayConfig configures the persona bot that runs next to Grimoire.
type RoleplayConfig struct {
	Token            string
	GuildID          string
	BotName          string
	LockeeRoleID     string
	SystemPromptFile string
	SystemPrompt     string
	ContextMessages  int
	MaxChunks        int
	Timeout          time.Duration
	MaxConcurrent    int
	RateLimitCount   int
	RateLimitWindow  time.Duration

	// Punishment scales added lock time for a broken rule.
	PunishPercent int
	PunishMin     time.Duration
	PunishMax     time.Duration

	// Chastity enables the ChastiSafe tools.
	Chastity ChastityConfig
}

// ChastityConfig binds the persona to one ChastiSafe keyholder account.
type ChastityConfig struct {
	Enabled       bool
	KeyholderCode string
	BaseURL       string
}

// Enabled reports whether the roleplay bot has a token.
func (r RoleplayConfig) Enabled() bool { return r.Token != "" }

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
		RedditSession:    strings.TrimSpace(os.Getenv("REDDIT_SESSION")),
		RedditUserAgent:  strings.TrimSpace(os.Getenv("REDDIT_USER_AGENT")),
		BDSMLRSession:    strings.TrimSpace(os.Getenv("BDSMLR_SESSION")),
		UmamiURL:         strings.TrimSpace(os.Getenv("UMAMI_URL")),
		UmamiWebsiteID:   strings.TrimSpace(os.Getenv("UMAMI_WEBSITE_ID")),
		UmamiAPIKey:      strings.TrimSpace(os.Getenv("UMAMI_API_KEY")),
		StatsSiteURL:     envOr("STATS_SITE_URL", "https://bdsmlr.zastraea.xyz"),
		StatsTimezone:    envOr("STATS_TIMEZONE", "UTC"),
		StatsChannelID:   strings.TrimSpace(os.Getenv("STATS_CHANNEL_ID")),
		OpenRouterAPIKey: strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		OpenRouterModel:  envOr("OPENROUTER_MODEL", defaultOpenRouterModel),
		OpenRouterBaseURL: envOr("OPENROUTER_BASE_URL",
			"https://openrouter.ai/api/v1"),
		Chastisafe: ChastisafeConfig{
			BaseURL:  strings.TrimSpace(os.Getenv("CHASTISAFE_BASE_URL")),
			StoreKey: strings.TrimSpace(os.Getenv("CHASTISAFE_STORE_KEY")),
		},
	}

	roleplay, err := loadRoleplay()
	if err != nil {
		return nil, err
	}
	cfg.Roleplay = roleplay

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

	includeNSFW, err := envBool("REDDIT_INCLUDE_NSFW", true)
	if err != nil {
		return nil, err
	}
	cfg.RedditIncludeNSFW = includeNSFW

	times, err := parseStatsTimes(envOr("STATS_TIMES", defaultStatsTimes))
	if err != nil {
		return nil, err
	}
	cfg.StatsTimes = times

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

const defaultStatsTimes = "09:00,21:00"

const defaultOpenRouterModel = "deepseek/deepseek-v4.1-flash-1017"

// loadRoleplay reads the optional second bot configuration. Every value has a
// safe default except the token, the lockee role, and the OpenRouter key.
func loadRoleplay() (RoleplayConfig, error) {
	chastityEnabled, err := envBool("ROLEPLAY_CHASTITY_ENABLED", true)
	if err != nil {
		return RoleplayConfig{}, err
	}

	contextMessages, err := envInt("ROLEPLAY_CONTEXT_MESSAGES", 15)
	if err != nil {
		return RoleplayConfig{}, err
	}
	maxChunks, err := envInt("ROLEPLAY_MAX_CHUNKS", 3)
	if err != nil {
		return RoleplayConfig{}, err
	}
	timeout, err := envDuration("ROLEPLAY_TIMEOUT_SECONDS", 60*time.Second)
	if err != nil {
		return RoleplayConfig{}, err
	}
	maxConcurrent, err := envInt("ROLEPLAY_MAX_CONCURRENT", 2)
	if err != nil {
		return RoleplayConfig{}, err
	}
	rateLimitCount, err := envInt("ROLEPLAY_RATE_LIMIT_CAPACITY", 2)
	if err != nil {
		return RoleplayConfig{}, err
	}
	rateLimitWindow, err := envDuration("ROLEPLAY_RATE_LIMIT_SECONDS", 10*time.Second)
	if err != nil {
		return RoleplayConfig{}, err
	}
	punishPercent, err := envInt("ROLEPLAY_PUNISH_PERCENT", 5)
	if err != nil {
		return RoleplayConfig{}, err
	}
	punishMinMinutes, err := envInt("ROLEPLAY_PUNISH_MIN_MINUTES", 30)
	if err != nil {
		return RoleplayConfig{}, err
	}
	punishMaxMinutes, err := envInt("ROLEPLAY_PUNISH_MAX_MINUTES", 240)
	if err != nil {
		return RoleplayConfig{}, err
	}

	return RoleplayConfig{
		Token:            strings.TrimSpace(os.Getenv("DISCORD_ROLEPLAY_TOKEN")),
		GuildID:          roleplayGuildID(),
		BotName:          envOr("ROLEPLAY_BOT_NAME", "Keyholder"),
		LockeeRoleID:     strings.TrimSpace(os.Getenv("ROLEPLAY_LOCKEE_ROLE_ID")),
		SystemPromptFile: envOr("ROLEPLAY_SYSTEM_PROMPT_FILE", "./prompts/keyholder.md"),
		SystemPrompt:     strings.TrimSpace(os.Getenv("ROLEPLAY_SYSTEM_PROMPT")),
		ContextMessages:  contextMessages,
		MaxChunks:        maxChunks,
		Timeout:          timeout,
		MaxConcurrent:    maxConcurrent,
		RateLimitCount:   rateLimitCount,
		RateLimitWindow:  rateLimitWindow,
		PunishPercent:    punishPercent,
		PunishMin:        time.Duration(punishMinMinutes) * time.Minute,
		PunishMax:        time.Duration(punishMaxMinutes) * time.Minute,
		Chastity: ChastityConfig{
			Enabled:       chastityEnabled,
			KeyholderCode: strings.TrimSpace(os.Getenv("CHASTISAFE_KEYHOLDER_CODE")),
			BaseURL:       strings.TrimSpace(os.Getenv("CHASTISAFE_BASE_URL")),
		},
	}, nil
}

// roleplayGuildID returns the optional server lock for the persona. An empty
// value lets it serve every guild it is invited to.
func roleplayGuildID() string {
	return strings.TrimSpace(os.Getenv("ROLEPLAY_GUILD_ID"))
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
	if err := c.validateUmami(); err != nil {
		return err
	}
	if _, err := time.LoadLocation(c.StatsTimezone); err != nil {
		return fmt.Errorf("STATS_TIMEZONE %q is not a time zone: %w", c.StatsTimezone, err)
	}
	return nil
}

// validateUmami keeps a half-configured analytics client from starting, since
// every request would fail with a 401 at report time.
func (c *Config) validateUmami() error {
	set := 0
	for _, value := range []string{c.UmamiURL, c.UmamiWebsiteID, c.UmamiAPIKey} {
		if value != "" {
			set++
		}
	}
	if set == 0 || set == 3 {
		return nil
	}
	return errors.New("UMAMI_URL, UMAMI_WEBSITE_ID and UMAMI_API_KEY must be set together, or all left empty")
}

// envInt reads a positive or zero integer environment variable.
func envInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not an integer", key, raw)
	}
	return value, nil
}

// envDuration reads a duration from a plain number of seconds.
func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number of seconds", key, raw)
	}
	return time.Duration(seconds) * time.Second, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// envBool reads a boolean environment variable, falling back when unset.
func envBool(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s %q is not a boolean", key, raw)
	}
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

// parseStatsTimes accepts "09:00,21:00" and returns the de-duplicated times in
// chronological order. "off" turns scheduled reports off, leaving /stats post.
func parseStatsTimes(raw string) ([]string, error) {
	value := strings.TrimSpace(raw)
	if strings.EqualFold(value, "off") {
		return nil, nil
	}
	if value == "" {
		return nil, errors.New("STATS_TIMES must be a comma-separated list of HH:MM times, or \"off\"")
	}

	seen := make(map[string]struct{})
	times := make([]string, 0, 2)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := time.Parse("15:04", part)
		if err != nil {
			return nil, fmt.Errorf("STATS_TIMES value %q is not HH:MM", part)
		}
		normalized := parsed.Format("15:04")
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		times = append(times, normalized)
	}
	if len(times) == 0 {
		return nil, errors.New("STATS_TIMES has no times")
	}
	sort.Strings(times)
	return times, nil
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
