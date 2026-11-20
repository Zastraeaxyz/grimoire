package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestFeedLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	id, err := st.CreateFeed(ctx, &Feed{
		GuildID:   "guild-1",
		URL:       "https://example.com/feed.xml",
		ChannelID: "channel-1",
		Title:     "Example",
		Domain:    "example.com",
		AddedBy:   "user-1",
	})
	if err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}

	got, err := st.GetFeedByURL(ctx, "guild-1", "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("GetFeedByURL: %v", err)
	}
	if got == nil {
		t.Fatal("expected a feed")
	}
	if got.ID != id || got.ChannelID != "channel-1" || got.Title != "Example" {
		t.Fatalf("unexpected feed: %+v", got)
	}
	if got.Domain != "example.com" || got.AddedBy != "user-1" {
		t.Fatalf("identity not saved: %+v", got)
	}
	if time.Since(got.CreatedAt) > time.Minute {
		t.Fatalf("unexpected created_at: %v", got.CreatedAt)
	}

	if err := st.UpdateFeedFetch(ctx, id, "Renamed", "example.org", `"etag-1"`, "yesterday", ""); err != nil {
		t.Fatalf("UpdateFeedFetch: %v", err)
	}
	got, err = st.GetFeedByURL(ctx, "guild-1", "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("GetFeedByURL: %v", err)
	}
	if got.Title != "Renamed" || got.Domain != "example.org" ||
		got.ETag != `"etag-1"` || got.LastModified != "yesterday" {
		t.Fatalf("fetch state not saved: %+v", got)
	}
	if got.LastCheckedAt.IsZero() {
		t.Fatal("expected last_checked_at")
	}

	// Empty values must not erase the stored identity.
	if err := st.UpdateFeedFetch(ctx, id, "", "", "", "", "boom"); err != nil {
		t.Fatalf("UpdateFeedFetch: %v", err)
	}
	got, _ = st.GetFeedByURL(ctx, "guild-1", "https://example.com/feed.xml")
	if got.Title != "Renamed" || got.Domain != "example.org" || got.LastError != "boom" {
		t.Fatalf("empty values overwrote state: %+v", got)
	}

	feeds, err := st.ListFeeds(ctx, "guild-1")
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("feeds = %d", len(feeds))
	}

	if err := st.DeleteFeed(ctx, "guild-1", id); err != nil {
		t.Fatalf("DeleteFeed: %v", err)
	}
	got, err = st.GetFeedByURL(ctx, "guild-1", "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("GetFeedByURL after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("feed still present: %+v", got)
	}
}

func TestCreateFeedRejectsDuplicateURL(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	feed := &Feed{GuildID: "guild-1", URL: "https://example.com/feed", ChannelID: "channel-1"}
	if _, err := st.CreateFeed(ctx, feed); err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}
	if _, err := st.CreateFeed(ctx, feed); err == nil {
		t.Fatal("expected a UNIQUE violation for a duplicate URL")
	}
}

func TestMarkFeedItemKeepsMessageID(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	id, err := st.CreateFeed(ctx, &Feed{GuildID: "guild-1", URL: "https://example.com/feed", ChannelID: "channel-1"})
	if err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}

	if err := st.MarkFeedItem(ctx, id, "guid-1", "message-1"); err != nil {
		t.Fatalf("MarkFeedItem: %v", err)
	}
	// Seeding marks the same item again without a message ID; the posted ID
	// must survive.
	if err := st.MarkFeedItem(ctx, id, "guid-1", ""); err != nil {
		t.Fatalf("MarkFeedItem: %v", err)
	}

	var messageID string
	if err := st.db.QueryRowContext(ctx,
		`SELECT message_id FROM feed_items WHERE feed_id = ? AND guid = ?`, id, "guid-1",
	).Scan(&messageID); err != nil {
		t.Fatalf("reading marker: %v", err)
	}
	if messageID != "message-1" {
		t.Fatalf("message_id = %q, want message-1", messageID)
	}

	seen, err := st.SeenItemIDs(ctx, id)
	if err != nil {
		t.Fatalf("SeenItemIDs: %v", err)
	}
	if _, ok := seen["guid-1"]; !ok {
		t.Fatal("guid-1 not marked as seen")
	}
}

func TestDeleteFeedCascadesItems(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	id, err := st.CreateFeed(ctx, &Feed{GuildID: "guild-1", URL: "https://example.com/feed", ChannelID: "channel-1"})
	if err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}
	for _, guid := range []string{"a", "b", "c"} {
		if err := st.MarkFeedItem(ctx, id, guid, "message-"+guid); err != nil {
			t.Fatalf("MarkFeedItem: %v", err)
		}
	}

	if err := st.DeleteFeed(ctx, "guild-1", id); err != nil {
		t.Fatalf("DeleteFeed: %v", err)
	}

	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM feed_items`).Scan(&count); err != nil {
		t.Fatalf("counting items: %v", err)
	}
	if count != 0 {
		t.Fatalf("feed_items = %d after cascade, want 0", count)
	}
}

func TestPruneFeedItems(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	id, err := st.CreateFeed(ctx, &Feed{GuildID: "guild-1", URL: "https://example.com/feed", ChannelID: "channel-1"})
	if err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}
	for _, guid := range []string{"a", "b", "c", "d", "e"} {
		if err := st.MarkFeedItem(ctx, id, guid, ""); err != nil {
			t.Fatalf("MarkFeedItem: %v", err)
		}
		// Distinct timestamps make the retained rows deterministic.
		if _, err := st.db.ExecContext(ctx,
			`UPDATE feed_items SET seen_at = unixepoch() - ? WHERE feed_id = ? AND guid = ?`,
			guidOffset(guid), id, guid,
		); err != nil {
			t.Fatalf("backdating: %v", err)
		}
	}

	if err := st.PruneFeedItems(ctx, id, 3); err != nil {
		t.Fatalf("PruneFeedItems: %v", err)
	}

	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM feed_items WHERE feed_id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 3 {
		t.Fatalf("feed_items = %d, want 3", count)
	}

	seen, err := st.SeenItemIDs(ctx, id)
	if err != nil {
		t.Fatalf("SeenItemIDs: %v", err)
	}
	for _, guid := range []string{"c", "d", "e"} {
		if _, ok := seen[guid]; !ok {
			t.Fatalf("expected %q to survive pruning", guid)
		}
	}
}

func guidOffset(guid string) int64 {
	switch guid {
	case "a":
		return 50
	case "b":
		return 40
	case "c":
		return 30
	case "d":
		return 20
	case "e":
		return 10
	}
	return 0
}

// TestOpenMigratesVersionOneDatabase proves an existing install upgrades in
// place rather than needing a fresh file.
func TestOpenMigratesVersionOneDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grimoire.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	stmts := []string{
		`CREATE TABLE guild_settings (
			guild_id TEXT PRIMARY KEY,
			welcome_channel_id TEXT NOT NULL DEFAULT '',
			base_role_id TEXT NOT NULL DEFAULT '',
			rules_channel_id TEXT NOT NULL DEFAULT '',
			panel_message_id TEXT NOT NULL DEFAULT '',
			panel_body TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		)`,
		`PRAGMA user_version = 1`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("preparing v1 database: %v", err)
		}
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	feed := &Feed{
		GuildID:   "guild-1",
		URL:       "https://example.com/feed",
		ChannelID: "channel-1",
		Domain:    "example.com",
		AddedBy:   "user-1",
	}
	if _, err := st.CreateFeed(ctx, feed); err != nil {
		t.Fatalf("feeds table missing after migration: %v", err)
	}
}

// TestOpenMigratesVersionTwoDatabase covers the deployed v2 schema, whose
// feeds table predates the domain and added_by columns.
func TestOpenMigratesVersionTwoDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grimoire.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	stmts := []string{
		`CREATE TABLE guild_settings (
			guild_id TEXT PRIMARY KEY,
			welcome_channel_id TEXT NOT NULL DEFAULT '',
			base_role_id TEXT NOT NULL DEFAULT '',
			rules_channel_id TEXT NOT NULL DEFAULT '',
			panel_message_id TEXT NOT NULL DEFAULT '',
			panel_body TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		)`,
		`CREATE TABLE feeds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guild_id TEXT NOT NULL,
			url TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			etag TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			last_checked_at INTEGER,
			last_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			UNIQUE (guild_id, url)
		)`,
		`CREATE TABLE feed_items (
			feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
			guid TEXT NOT NULL,
			message_id TEXT NOT NULL DEFAULT '',
			seen_at INTEGER NOT NULL DEFAULT (unixepoch()),
			PRIMARY KEY (feed_id, guid)
		)`,
		`INSERT INTO guild_settings (guild_id, base_role_id) VALUES ('guild-1', 'role-old')`,
		`INSERT INTO feeds (guild_id, url, channel_id, title)
			VALUES ('guild-1', 'https://example.com/old', 'channel-1', 'Old')`,
		`PRAGMA user_version = 2`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("preparing v2 database: %v", err)
		}
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	old, err := st.GetFeedByURL(ctx, "guild-1", "https://example.com/old")
	if err != nil {
		t.Fatalf("GetFeedByURL: %v", err)
	}
	if old == nil || old.Title != "Old" || old.Domain != "" || old.AddedBy != "" {
		t.Fatalf("existing feed changed unexpectedly: %+v", old)
	}

	feed := &Feed{
		GuildID:   "guild-1",
		URL:       "https://example.com/new",
		ChannelID: "channel-1",
		Title:     "New",
		Domain:    "example.com",
		AddedBy:   "user-1",
	}
	if _, err := st.CreateFeed(ctx, feed); err != nil {
		t.Fatalf("CreateFeed after migration: %v", err)
	}

	settings, err := st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings after migration: %v", err)
	}
	if settings == nil || settings.BaseRoleID != "role-old" || settings.UserCommandRoleID != "" {
		t.Fatalf("existing settings changed unexpectedly: %+v", settings)
	}
	if err := st.SetUserCommandRole(ctx, "guild-1", "role-command"); err != nil {
		t.Fatalf("SetUserCommandRole after migration: %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings after setting the role: %v", err)
	}
	if settings.UserCommandRoleID != "role-command" {
		t.Fatalf("UserCommandRoleID = %q, want role-command", settings.UserCommandRoleID)
	}
}

// TestOpenMigratesVersionThreeDatabase covers the deployed v3 schema, which
// predates the user command role. A v3 database already has the welcome
// settings and feed columns, so only the guild settings column is added.
func TestOpenMigratesVersionThreeDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grimoire.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	stmts := []string{
		`CREATE TABLE guild_settings (
			guild_id TEXT PRIMARY KEY,
			welcome_channel_id TEXT NOT NULL DEFAULT '',
			base_role_id TEXT NOT NULL DEFAULT '',
			rules_channel_id TEXT NOT NULL DEFAULT '',
			panel_message_id TEXT NOT NULL DEFAULT '',
			panel_body TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		)`,
		`CREATE TABLE feeds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guild_id TEXT NOT NULL,
			url TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			etag TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			last_checked_at INTEGER,
			last_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			domain TEXT NOT NULL DEFAULT '',
			added_by TEXT NOT NULL DEFAULT '',
			UNIQUE (guild_id, url)
		)`,
		`CREATE TABLE feed_items (
			feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
			guid TEXT NOT NULL,
			message_id TEXT NOT NULL DEFAULT '',
			seen_at INTEGER NOT NULL DEFAULT (unixepoch()),
			PRIMARY KEY (feed_id, guid)
		)`,
		`INSERT INTO guild_settings (guild_id, welcome_channel_id, base_role_id)
			VALUES ('guild-1', 'channel-1', 'role-old')`,
		`PRAGMA user_version = 3`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("preparing v3 database: %v", err)
		}
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	settings, err := st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings after migration: %v", err)
	}
	if settings == nil ||
		settings.WelcomeChannelID != "channel-1" ||
		settings.BaseRoleID != "role-old" ||
		settings.UserCommandRoleID != "" {
		t.Fatalf("existing settings changed unexpectedly: %+v", settings)
	}

	if err := st.SetUserCommandRole(ctx, "guild-1", "role-command"); err != nil {
		t.Fatalf("SetUserCommandRole after migration: %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings after setting the role: %v", err)
	}
	if settings.UserCommandRoleID != "role-command" {
		t.Fatalf("UserCommandRoleID = %q, want role-command", settings.UserCommandRoleID)
	}

	if err := st.CreateBDSMLRConfirmation(ctx, &BDSMLRConfirmation{
		Token:     "token-1",
		GuildID:   "guild-1",
		UserID:    "user-1",
		Email:     "friend@example.com",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateBDSMLRConfirmation after migration: %v", err)
	}
	if err := st.RecordBDSMLRInvite(ctx, "guild-1", "user-1"); err != nil {
		t.Fatalf("RecordBDSMLRInvite after migration: %v", err)
	}
}

// TestOpenMigratesVersionFiveDatabase covers the deployed v5 schema, which
// predates the bot metadata channel. The upgrade must add the column without
// touching existing settings.
func TestOpenMigratesVersionFiveDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grimoire.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	stmts := []string{
		`CREATE TABLE guild_settings (
			guild_id TEXT PRIMARY KEY,
			welcome_channel_id TEXT NOT NULL DEFAULT '',
			base_role_id TEXT NOT NULL DEFAULT '',
			rules_channel_id TEXT NOT NULL DEFAULT '',
			panel_message_id TEXT NOT NULL DEFAULT '',
			panel_body TEXT NOT NULL DEFAULT '',
			user_command_role_id TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		)`,
		`INSERT INTO guild_settings (guild_id, welcome_channel_id, base_role_id, user_command_role_id)
			VALUES ('guild-1', 'channel-1', 'role-old', 'role-command')`,
		`PRAGMA user_version = 5`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("preparing v5 database: %v", err)
		}
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	settings, err := st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings after migration: %v", err)
	}
	if settings == nil ||
		settings.WelcomeChannelID != "channel-1" ||
		settings.BaseRoleID != "role-old" ||
		settings.UserCommandRoleID != "role-command" ||
		settings.BotMetaChannelID != "" {
		t.Fatalf("existing settings changed unexpectedly: %+v", settings)
	}

	if err := st.SetBotMetaChannel(ctx, "guild-1", "meta-1"); err != nil {
		t.Fatalf("SetBotMetaChannel after migration: %v", err)
	}
	settings, err = st.GetGuildSettings(ctx, "guild-1")
	if err != nil {
		t.Fatalf("GetGuildSettings after setting the channel: %v", err)
	}
	if settings.BotMetaChannelID != "meta-1" {
		t.Fatalf("BotMetaChannelID = %q, want meta-1", settings.BotMetaChannelID)
	}
}
