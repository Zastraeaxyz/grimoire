package config

import (
	"testing"
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
