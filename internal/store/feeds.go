package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Feed is an RSS or Atom source that posts new items into a channel.
type Feed struct {
	ID            int64
	GuildID       string
	URL           string
	ChannelID     string
	Title         string
	ETag          string
	LastModified  string
	LastCheckedAt time.Time
	LastError     string
	CreatedAt     time.Time
}

// CreateFeed inserts a feed and returns its id.
func (s *Store) CreateFeed(ctx context.Context, f *Feed) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO feeds (guild_id, url, channel_id, title)
		VALUES (?, ?, ?, ?)`,
		f.GuildID, f.URL, f.ChannelID, f.Title)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetFeedByURL returns the feed for a URL in a guild, or (nil, nil) when the
// URL is not followed yet. Used to keep a URL from being added twice.
func (s *Store) GetFeedByURL(ctx context.Context, guildID, url string) (*Feed, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, guild_id, url, channel_id, title, etag, last_modified,
		       last_checked_at, last_error, created_at
		FROM feeds
		WHERE guild_id = ? AND url = ?`, guildID, url)
	return scanFeed(row)
}

// ListFeeds returns every feed for a guild, oldest first.
func (s *Store) ListFeeds(ctx context.Context, guildID string) ([]Feed, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, guild_id, url, channel_id, title, etag, last_modified,
		       last_checked_at, last_error, created_at
		FROM feeds
		WHERE guild_id = ?
		ORDER BY id`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, *f)
	}
	return feeds, rows.Err()
}

// DeleteFeed removes a feed and, through the cascade, its item markers.
// Messages already posted to Discord are left alone.
func (s *Store) DeleteFeed(ctx context.Context, guildID string, id int64) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM feeds WHERE guild_id = ? AND id = ?`, guildID, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("no feed %d for guild %s", id, guildID)
	}
	return nil
}

// UpdateFeedFetch records a completed poll. Empty values leave the stored
// validators and title untouched, so a failed parse cannot erase them.
func (s *Store) UpdateFeedFetch(ctx context.Context, id int64, title, etag, lastModified, fetchErr string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE feeds
		SET title = CASE WHEN ? <> '' THEN ? ELSE title END,
		    etag = CASE WHEN ? <> '' THEN ? ELSE etag END,
		    last_modified = CASE WHEN ? <> '' THEN ? ELSE last_modified END,
		    last_checked_at = unixepoch(),
		    last_error = ?
		WHERE id = ?`,
		title, title,
		etag, etag,
		lastModified, lastModified,
		fetchErr, id)
	return err
}

// SeenItemIDs returns the set of feed item keys already handled.
func (s *Store) SeenItemIDs(ctx context.Context, feedID int64) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT guid FROM feed_items WHERE feed_id = ?`, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return nil, err
		}
		seen[guid] = struct{}{}
	}
	return seen, rows.Err()
}

// MarkFeedItem records an item as handled. An empty messageID means the item
// was deliberately skipped, for example beyond the backfill window.
func (s *Store) MarkFeedItem(ctx context.Context, feedID int64, guid, messageID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feed_items (feed_id, guid, message_id)
		VALUES (?, ?, ?)
		ON CONFLICT(feed_id, guid) DO UPDATE SET
			message_id = CASE WHEN excluded.message_id <> ''
			                  THEN excluded.message_id
			                  ELSE feed_items.message_id END`,
		feedID, guid, messageID)
	return err
}

// PruneFeedItems keeps only the most recent keep markers for a feed.
func (s *Store) PruneFeedItems(ctx context.Context, feedID int64, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM feed_items
		WHERE feed_id = ?
		  AND guid NOT IN (
		      SELECT guid FROM feed_items
		      WHERE feed_id = ?
		      ORDER BY seen_at DESC, rowid DESC
		      LIMIT ?
		  )`, feedID, feedID, keep)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFeed(row rowScanner) (*Feed, error) {
	var (
		f         Feed
		checkedAt sql.NullInt64
		createdAt int64
	)
	err := row.Scan(
		&f.ID, &f.GuildID, &f.URL, &f.ChannelID, &f.Title,
		&f.ETag, &f.LastModified, &checkedAt, &f.LastError, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if checkedAt.Valid {
		f.LastCheckedAt = time.Unix(checkedAt.Int64, 0).UTC()
	}
	f.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &f, nil
}
