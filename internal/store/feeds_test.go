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
	if time.Since(got.CreatedAt) > time.Minute {
		t.Fatalf("unexpected created_at: %v", got.CreatedAt)
	}

	if err := st.UpdateFeedFetch(ctx, id, "Renamed", `"etag-1"`, "yesterday", ""); err != nil {
		t.Fatalf("UpdateFeedFetch: %v", err)
	}
	got, err = st.GetFeedByURL(ctx, "guild-1", "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("GetFeedByURL: %v", err)
	}
	if got.Title != "Renamed" || got.ETag != `"etag-1"` || got.LastModified != "yesterday" {
		t.Fatalf("fetch state not saved: %+v", got)
	}
	if got.LastCheckedAt.IsZero() {
		t.Fatal("expected last_checked_at")
	}

	// An empty title must not erase the stored one.
	if err := st.UpdateFeedFetch(ctx, id, "", "", "", "boom"); err != nil {
		t.Fatalf("UpdateFeedFetch: %v", err)
	}
	got, _ = st.GetFeedByURL(ctx, "guild-1", "https://example.com/feed.xml")
	if got.Title != "Renamed" || got.LastError != "boom" {
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
	if _, err := st.CreateFeed(ctx, &Feed{GuildID: "guild-1", URL: "https://example.com/feed", ChannelID: "channel-1"}); err != nil {
		t.Fatalf("feeds table missing after migration: %v", err)
	}
}
