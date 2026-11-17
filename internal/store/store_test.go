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
		GuildID:          "guild-1",
		WelcomeChannelID: "channel-1",
		BaseRoleID:       "role-1",
		RulesChannelID:   "rules-1",
		PanelBody:        "custom body",
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
		got.PanelBody != want.PanelBody {
		t.Fatalf("settings mismatch: got %+v want %+v", got, want)
	}
	if time.Since(got.UpdatedAt) > time.Minute {
		t.Fatalf("unexpected updated_at: %v", got.UpdatedAt)
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
