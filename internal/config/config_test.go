package config

import (
	"testing"
	"time"
)

func TestParseBytes(t *testing.T) {
	cases := map[string]int64{
		"10485760": 10485760,
		"10MiB":    10485760,
		"10MB":     10485760,
		"512KiB":   524288,
		"512KB":    524288,
		"1MiB":     1048576,
	}
	for input, want := range cases {
		got, err := parseBytes(input)
		if err != nil {
			t.Fatalf("parseBytes(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("parseBytes(%q) = %d, want %d", input, got, want)
		}
	}

	for _, input := range []string{"", "0", "-5", "abc", "10GB"} {
		if _, err := parseBytes(input); err == nil {
			t.Errorf("parseBytes(%q) unexpectedly succeeded", input)
		}
	}
}

func TestParseStatsTimes(t *testing.T) {
	times, err := parseStatsTimes("21:00, 09:00,09:00")
	if err != nil {
		t.Fatalf("parseStatsTimes: %v", err)
	}
	want := []string{"09:00", "21:00"}
	if len(times) != len(want) || times[0] != want[0] || times[1] != want[1] {
		t.Fatalf("times = %v, want %v", times, want)
	}

	if times, err := parseStatsTimes("off"); err != nil || times != nil {
		t.Fatalf("off = (%v, %v), want (nil, nil)", times, err)
	}
	for _, raw := range []string{"", "9am", "25:00", "09:00,,nope"} {
		if _, err := parseStatsTimes(raw); err == nil {
			t.Errorf("parseStatsTimes(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestLoadStatsDefaults(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	for _, key := range []string{
		"STATS_SITE_URL", "STATS_TIMEZONE", "STATS_TIMES", "STATS_CHANNEL_ID",
		"UMAMI_URL", "UMAMI_WEBSITE_ID", "UMAMI_API_KEY",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StatsSiteURL != "https://bdsmlr.zastraea.xyz" {
		t.Fatalf("StatsSiteURL = %q", cfg.StatsSiteURL)
	}
	if cfg.StatsTimezone != "UTC" {
		t.Fatalf("StatsTimezone = %q", cfg.StatsTimezone)
	}
	if len(cfg.StatsTimes) != 2 || cfg.StatsTimes[0] != "09:00" || cfg.StatsTimes[1] != "21:00" {
		t.Fatalf("StatsTimes = %v", cfg.StatsTimes)
	}
	if cfg.UmamiURL != "" || cfg.UmamiWebsiteID != "" || cfg.UmamiAPIKey != "" {
		t.Fatalf("umami settings should be empty: %+v", cfg)
	}
}

func TestLoadRejectsPartialUmamiConfig(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("UMAMI_URL", "https://analytics.example.com")
	t.Setenv("UMAMI_WEBSITE_ID", "")
	t.Setenv("UMAMI_API_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a partial Umami configuration")
	}
}

func TestLoadRejectsInvalidStatsTimezone(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("STATS_TIMEZONE", "Mars/Olympus_Mons")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an unknown time zone")
	}
}

func TestLoadBdsmlrSession(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("BDSMLR_SESSION", "  wordpress_logged_in_abc=value  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BDSMLRSession != "wordpress_logged_in_abc=value" {
		t.Fatalf("BDSMLRSession = %q", cfg.BDSMLRSession)
	}
}

func TestLoadRoleplayDefaults(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	for _, key := range []string{
		"DISCORD_ROLEPLAY_TOKEN", "ROLEPLAY_LOCKEE_ROLE_ID", "OPENROUTER_API_KEY",
		"ROLEPLAY_CHASTITY_ENABLED", "CHASTISAFE_KEYHOLDER_CODE", "CHASTISAFE_BASE_URL",
		"ROLEPLAY_GUILD_ID", "ROLEPLAY_PUNISH_PERCENT", "ROLEPLAY_PUNISH_MIN_MINUTES",
		"ROLEPLAY_PUNISH_MAX_MINUTES", "ROLEPLAY_VISION_ENABLED",
		"ROLEPLAY_VISION_MAX_IMAGES", "ROLEPLAY_VISION_MAX_IMAGE_MB",
		"ROLEPLAY_VISION_DETAIL", "ROLEPLAY_ABOUT_FILE",
		"ROLEPLAY_MEMORY_ENABLED", "ROLEPLAY_MEMORY_DIR", "ROLEPLAY_MEMORY_MAX_CHARS",
		"ROLEPLAY_MEMORY_CONTEXT_MAX_CHARS", "ROLEPLAY_MEMORY_TIMEOUT_SECONDS",
		"ROLEPLAY_SOUL_MAX_CHARS", "ROLEPLAY_LEARNING_SESSION_MINUTES",
		"ROLEPLAY_LEARNING_TIMEOUT_SECONDS", "ROLEPLAY_AUX_MODEL",
		"ROLEPLAY_AUX_MAX_TOKENS", "DATABASE_PATH",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Roleplay.Enabled() {
		t.Fatal("roleplay should be off without a token")
	}
	if cfg.Roleplay.BotName != "Keyholder" {
		t.Fatalf("BotName = %q", cfg.Roleplay.BotName)
	}
	if cfg.Roleplay.ContextMessages != 15 || cfg.Roleplay.MaxChunks != 3 {
		t.Fatalf("roleplay limits = %+v", cfg.Roleplay)
	}
	if cfg.OpenRouterModel != defaultOpenRouterModel {
		t.Fatalf("OpenRouterModel = %q", cfg.OpenRouterModel)
	}
	if cfg.Roleplay.PunishPercent != 5 ||
		cfg.Roleplay.PunishMin != 30*time.Minute ||
		cfg.Roleplay.PunishMax != 4*time.Hour {
		t.Fatalf("punishment defaults = %+v", cfg.Roleplay)
	}
	if !cfg.Roleplay.VisionEnabled ||
		cfg.Roleplay.VisionMaxImages != 4 ||
		cfg.Roleplay.VisionMaxImageMB != 8 ||
		cfg.Roleplay.VisionDetail != "auto" {
		t.Fatalf("vision defaults = %+v", cfg.Roleplay)
	}
	if cfg.Roleplay.AboutFile != "./prompts/about.md" {
		t.Fatalf("AboutFile = %q", cfg.Roleplay.AboutFile)
	}
	if !cfg.Roleplay.MemoryEnabled ||
		cfg.Roleplay.MemoryDir != "data/roleplay" ||
		cfg.Roleplay.MemoryMaxChars != 3000 ||
		cfg.Roleplay.MemoryContextChars != 2500 ||
		cfg.Roleplay.MemoryTimeout != 30*time.Second ||
		cfg.Roleplay.SoulMaxChars != 8000 ||
		cfg.Roleplay.LearningTTL != time.Hour ||
		cfg.Roleplay.LearningTimeout != 60*time.Second ||
		cfg.Roleplay.AuxMaxTokens != 4096 {
		t.Fatalf("memory defaults = %+v", cfg.Roleplay)
	}
}

func TestLoadRoleplayMemoryDirFromDatabase(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("DATABASE_PATH", "/data/grimoire.db")
	t.Setenv("ROLEPLAY_MEMORY_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Roleplay.MemoryDir != "/data/roleplay" {
		t.Fatalf("MemoryDir = %q", cfg.Roleplay.MemoryDir)
	}
}

func TestLoadRoleplayMemoryOverrides(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("ROLEPLAY_MEMORY_ENABLED", "false")
	t.Setenv("ROLEPLAY_MEMORY_DIR", "/tmp/raven-memory")
	t.Setenv("ROLEPLAY_MEMORY_MAX_CHARS", "1200")
	t.Setenv("ROLEPLAY_MEMORY_CONTEXT_MAX_CHARS", "900")
	t.Setenv("ROLEPLAY_SOUL_MAX_CHARS", "4000")
	t.Setenv("ROLEPLAY_LEARNING_SESSION_MINUTES", "30")
	t.Setenv("ROLEPLAY_AUX_MODEL", "test/model")
	t.Setenv("ROLEPLAY_AUX_MAX_TOKENS", "2048")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Roleplay.MemoryEnabled ||
		cfg.Roleplay.MemoryDir != "/tmp/raven-memory" ||
		cfg.Roleplay.MemoryMaxChars != 1200 ||
		cfg.Roleplay.MemoryContextChars != 900 ||
		cfg.Roleplay.SoulMaxChars != 4000 ||
		cfg.Roleplay.LearningTTL != 30*time.Minute ||
		cfg.Roleplay.AuxModel != "test/model" ||
		cfg.Roleplay.AuxMaxTokens != 2048 {
		t.Fatalf("memory overrides = %+v", cfg.Roleplay)
	}
}

func TestLoadRoleplayAboutFileOverride(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("ROLEPLAY_ABOUT_FILE", "./prompts/custom-about.md")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Roleplay.AboutFile != "./prompts/custom-about.md" {
		t.Fatalf("AboutFile = %q", cfg.Roleplay.AboutFile)
	}
}

func TestLoadRoleplayVisionOverrides(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("ROLEPLAY_VISION_ENABLED", "false")
	t.Setenv("ROLEPLAY_VISION_MAX_IMAGES", "2")
	t.Setenv("ROLEPLAY_VISION_MAX_IMAGE_MB", "3")
	t.Setenv("ROLEPLAY_VISION_DETAIL", "high")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Roleplay.VisionEnabled ||
		cfg.Roleplay.VisionMaxImages != 2 ||
		cfg.Roleplay.VisionMaxImageMB != 3 ||
		cfg.Roleplay.VisionDetail != "high" {
		t.Fatalf("vision = %+v", cfg.Roleplay)
	}
}

func TestLoadRoleplayDoesNotRequireCompanionsAtLoad(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("DISCORD_ROLEPLAY_TOKEN", "roleplay-token")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("ROLEPLAY_LOCKEE_ROLE_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load should keep Grimoire running when roleplay is incomplete: %v", err)
	}
	if !cfg.Roleplay.Enabled() {
		t.Fatal("roleplay should be enabled")
	}
}

func TestLoadRoleplayEnabled(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GUILD_ID", "123")
	t.Setenv("DISCORD_ROLEPLAY_TOKEN", "roleplay-token")
	t.Setenv("OPENROUTER_API_KEY", "key")
	t.Setenv("ROLEPLAY_LOCKEE_ROLE_ID", "456")
	t.Setenv("ROLEPLAY_GUILD_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Roleplay.Enabled() {
		t.Fatal("roleplay should be enabled")
	}
	if cfg.Roleplay.LockeeRoleID != "456" {
		t.Fatalf("LockeeRoleID = %q", cfg.Roleplay.LockeeRoleID)
	}
	if cfg.Roleplay.GuildID != "" {
		t.Fatalf("GuildID = %q, want the persona to serve every guild by default", cfg.Roleplay.GuildID)
	}
	if !cfg.Roleplay.Chastity.Enabled {
		t.Fatal("chastity should default to on")
	}
}

func TestRoleplayGuildID(t *testing.T) {
	t.Setenv("GUILD_ID", "123")
	t.Setenv("ROLEPLAY_GUILD_ID", "")
	if got := roleplayGuildID(); got != "" {
		t.Fatalf("roleplayGuildID = %q, want an empty optional gate", got)
	}

	t.Setenv("ROLEPLAY_GUILD_ID", "456")
	if got := roleplayGuildID(); got != "456" {
		t.Fatalf("roleplayGuildID = %q", got)
	}
}
