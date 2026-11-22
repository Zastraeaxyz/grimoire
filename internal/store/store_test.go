package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(filepath.Join(t.TempDir(), "grimoire.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return st
}

func TestSetBotMetaChannel(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// The upsert must work before any other settings row exists.
	if err := st.SetBotMetaChannel(ctx, "guild-1", "meta-1"); err != nil {
		t.Fatalf("SetBotMetaChannel: %v", err)
	}
	settings, err := st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if settings == nil || settings.BotMetaChannelID != "meta-1" {
		t.Fatalf("BotMetaChannelID = %+v, want meta-1", settings)
	}

	if err := st.SetBotMetaChannel(ctx, "guild-1", ""); err != nil {
		t.Fatalf("clearing the bot metadata channel: %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings after clearing: %v", err)
	}
	if settings.BotMetaChannelID != "" {
		t.Fatalf("BotMetaChannelID = %q, want empty", settings.BotMetaChannelID)
	}
}

func TestSetStatsChannel(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// The upsert must work before any other settings row exists.
	if err := st.SetStatsChannel(ctx, "guild-1", "stats-1"); err != nil {
		t.Fatalf("SetStatsChannel: %v", err)
	}
	settings, err := st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if settings == nil || settings.StatsChannelID != "stats-1" {
		t.Fatalf("StatsChannelID = %+v", settings)
	}

	if err := st.SetStatsMessage(ctx, "guild-1", "stats-1", "message-1"); err != nil {
		t.Fatalf("SetStatsMessage: %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if settings.StatsMessageID != "message-1" {
		t.Fatalf("StatsMessageID = %q, want message-1", settings.StatsMessageID)
	}

	// Saving the same channel keeps the dashboard message.
	if err := st.SetStatsChannel(ctx, "guild-1", "stats-1"); err != nil {
		t.Fatalf("SetStatsChannel(same): %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if settings.StatsMessageID != "message-1" {
		t.Fatalf("StatsMessageID = %q, want it kept", settings.StatsMessageID)
	}

	// Moving the dashboard drops a message ID that belongs to the old channel.
	if err := st.SetStatsChannel(ctx, "guild-1", "stats-2"); err != nil {
		t.Fatalf("SetStatsChannel(move): %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if settings.StatsChannelID != "stats-2" || settings.StatsMessageID != "" {
		t.Fatalf("settings after move = %+v", settings)
	}

	// A message saved for a stale channel is ignored.
	if err := st.SetStatsMessage(ctx, "guild-1", "stats-1", "stale"); err != nil {
		t.Fatalf("SetStatsMessage(stale): %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if settings.StatsMessageID != "" {
		t.Fatalf("StatsMessageID = %q, want empty", settings.StatsMessageID)
	}
}

func TestGetGuildSettingsMissing(t *testing.T) {
	st := newTestStore(t)
	settings, err := st.GetGuildSettings(context.Background(), "123")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil settings, got %+v", settings)
	}
}

func TestSaveAndLoadSettings(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	want := &GuildSettings{
		GuildID:           "guild-1",
		WelcomeChannelID:  "channel-1",
		BaseRoleID:        "role-1",
		RulesChannelID:    "rules-1",
		PanelBody:         "custom body",
		UserCommandRoleID: "command-role-1",
		BotMetaChannelID:  "meta-1",
		StatsChannelID:    "stats-1",
		StatsMessageID:    "stats-message-1",
	}
	if err := st.SaveGuildSettings(ctx, want); err != nil {
		t.Fatalf("SaveGuildSettings: %v", err)
	}

	got, err := st.GetGuildSettings(ctx, want.GuildID)
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if got == nil {
		t.Fatal("expected settings, got nil")
	}
	if got.WelcomeChannelID != want.WelcomeChannelID ||
		got.BaseRoleID != want.BaseRoleID ||
		got.RulesChannelID != want.RulesChannelID ||
		got.PanelBody != want.PanelBody ||
		got.UserCommandRoleID != want.UserCommandRoleID ||
		got.BotMetaChannelID != want.BotMetaChannelID ||
		got.StatsChannelID != want.StatsChannelID ||
		got.StatsMessageID != want.StatsMessageID {
		t.Fatalf("settings mismatch: got %+v want %+v", got, want)
	}
	if time.Since(got.UpdatedAt) > time.Minute {
		t.Fatalf("unexpected updated_at: %v", got.UpdatedAt)
	}
}

func TestSetUserCommandRoleWithoutSettings(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if err := st.SetUserCommandRole(ctx, "guild-1", "role-9"); err != nil {
		t.Fatalf("SetUserCommandRole: %v", err)
	}
	got, err := st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if got == nil || got.UserCommandRoleID != "role-9" {
		t.Fatalf("settings = %+v, want the user command role", got)
	}

	if err := st.SetUserCommandRole(ctx, "guild-1", ""); err != nil {
		t.Fatalf("SetUserCommandRole(clear): %v", err)
	}
	got, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if got.UserCommandRoleID != "" {
		t.Fatalf("UserCommandRoleID = %q, want empty", got.UserCommandRoleID)
	}
}

func TestSetPanelMessage(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if err := st.SaveGuildSettings(ctx, &GuildSettings{GuildID: "guild-1"}); err != nil {
		t.Fatalf("SaveGuildSettings: %v", err)
	}
	if err := st.SetPanelMessage(ctx, "guild-1", "message-1"); err != nil {
		t.Fatalf("SetPanelMessage: %v", err)
	}

	got, err := st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings: %v", err)
	}
	if got.PanelMessageID != "message-1" {
		t.Fatalf("PanelMessageID = %q, want message-1", got.PanelMessageID)
	}
}

func TestSetPanelMessageWithoutSettings(t *testing.T) {
	st := newTestStore(t)
	if err := st.SetPanelMessage(context.Background(), "missing", "message-1"); err == nil {
		t.Fatal("expected an error for a guild without settings")
	}
}

func TestAcknowledgementsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	for i := 0; i < 3; i++ {
		if err := st.RecordAcknowledgement(ctx, "guild-1", "user-1"); err != nil {
			t.Fatalf("RecordAcknowledgement: %v", err)
		}
	}
	if err := st.RecordAcknowledgement(ctx, "guild-1", "user-2"); err != nil {
		t.Fatalf("RecordAcknowledgement: %v", err)
	}

	count, err := st.CountAcknowledgements(ctx, "guild-1")
	if err != nil {
		t.Fatalf("CountAcknowledgements: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}
