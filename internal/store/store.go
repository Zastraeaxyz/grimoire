package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 6

// Store is the SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
}

// GuildSettings holds the per-guild configuration. The welcome fields belong
// to the welcome panel; UserCommandRoleID gates commands that are open to
// members beyond server admins.
type GuildSettings struct {
	GuildID           string
	WelcomeChannelID  string
	BaseRoleID        string
	RulesChannelID    string
	PanelMessageID    string
	PanelBody         string
	UserCommandRoleID string
	BotMetaChannelID  string
	UpdatedAt         time.Time
}

// Open opens the SQLite database, creating its directory if needed, and
// applies any pending migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating database directory: %w", err)
		}
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	// One writer keeps SQLite happy without busy loops. The bot's write
	// volume is tiny (panel state and acknowledgement audits).
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}
	return &Store{db: db}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= schemaVersion {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Each step applies only once. Columns added later use ALTER TABLE, so
	// existing installs upgrade in place without a fresh database file.
	steps := []struct {
		version    int
		statements []string
	}{
		{
			version: 1,
			statements: []string{
				`CREATE TABLE IF NOT EXISTS guild_settings (
					guild_id TEXT PRIMARY KEY,
					welcome_channel_id TEXT NOT NULL DEFAULT '',
					base_role_id TEXT NOT NULL DEFAULT '',
					rules_channel_id TEXT NOT NULL DEFAULT '',
					panel_message_id TEXT NOT NULL DEFAULT '',
					panel_body TEXT NOT NULL DEFAULT '',
					updated_at INTEGER NOT NULL DEFAULT (unixepoch())
				)`,
				`CREATE TABLE IF NOT EXISTS acknowledgements (
					guild_id TEXT NOT NULL,
					user_id TEXT NOT NULL,
					acknowledged_at INTEGER NOT NULL DEFAULT (unixepoch()),
					PRIMARY KEY (guild_id, user_id)
				)`,
			},
		},
		{
			version: 2,
			statements: []string{
				`CREATE TABLE IF NOT EXISTS feeds (
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
				`CREATE TABLE IF NOT EXISTS feed_items (
					feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
					guid TEXT NOT NULL,
					message_id TEXT NOT NULL DEFAULT '',
					seen_at INTEGER NOT NULL DEFAULT (unixepoch()),
					PRIMARY KEY (feed_id, guid)
				)`,
				`CREATE INDEX IF NOT EXISTS feed_items_seen_at
					ON feed_items (feed_id, seen_at)`,
			},
		},
		{
			version: 3,
			statements: []string{
				`ALTER TABLE feeds ADD COLUMN domain TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE feeds ADD COLUMN added_by TEXT NOT NULL DEFAULT ''`,
			},
		},
		{
			version: 4,
			statements: []string{
				`ALTER TABLE guild_settings ADD COLUMN user_command_role_id TEXT NOT NULL DEFAULT ''`,
			},
		},
		{
			version: 5,
			statements: []string{
				`CREATE TABLE IF NOT EXISTS bdsmlr_invite_confirmations (
					token TEXT PRIMARY KEY,
					guild_id TEXT NOT NULL,
					channel_id TEXT NOT NULL DEFAULT '',
					user_id TEXT NOT NULL,
					email TEXT NOT NULL,
					created_at INTEGER NOT NULL DEFAULT (unixepoch()),
					expires_at INTEGER NOT NULL
				)`,
				`CREATE INDEX IF NOT EXISTS bdsmlr_invite_confirmations_expires
					ON bdsmlr_invite_confirmations (expires_at)`,
				`CREATE TABLE IF NOT EXISTS bdsmlr_invites (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					guild_id TEXT NOT NULL,
					user_id TEXT NOT NULL,
					sent_at INTEGER NOT NULL DEFAULT (unixepoch())
				)`,
				`CREATE INDEX IF NOT EXISTS bdsmlr_invites_user
					ON bdsmlr_invites (guild_id, user_id, sent_at)`,
			},
		},
		{
			version: 6,
			statements: []string{
				`ALTER TABLE guild_settings ADD COLUMN bot_meta_channel_id TEXT NOT NULL DEFAULT ''`,
			},
		},
	}

	for _, step := range steps {
		if version >= step.version {
			continue
		}
		for _, stmt := range step.statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration %d: %w", step.version, err)
			}
		}
	}

	// user_version cannot be parameterized, and schemaVersion is a constant.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// GetGuildSettings returns the settings for a guild, or (nil, nil) when the
// guild has not been configured yet.
func (s *Store) GetGuildSettings(ctx context.Context, guildID string) (*GuildSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT guild_id, welcome_channel_id, base_role_id, rules_channel_id,
		       panel_message_id, panel_body, user_command_role_id,
		       bot_meta_channel_id, updated_at
		FROM guild_settings
		WHERE guild_id = ?`, guildID)

	var (
		st        GuildSettings
		updatedAt int64
	)
	err := row.Scan(
		&st.GuildID,
		&st.WelcomeChannelID,
		&st.BaseRoleID,
		&st.RulesChannelID,
		&st.PanelMessageID,
		&st.PanelBody,
		&st.UserCommandRoleID,
		&st.BotMetaChannelID,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &st, nil
}

// SaveGuildSettings inserts or replaces the settings for a guild.
func (s *Store) SaveGuildSettings(ctx context.Context, st *GuildSettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO guild_settings (
			guild_id, welcome_channel_id, base_role_id, rules_channel_id,
			panel_message_id, panel_body, user_command_role_id,
			bot_meta_channel_id, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(guild_id) DO UPDATE SET
			welcome_channel_id = excluded.welcome_channel_id,
			base_role_id = excluded.base_role_id,
			rules_channel_id = excluded.rules_channel_id,
			panel_message_id = excluded.panel_message_id,
			panel_body = excluded.panel_body,
			user_command_role_id = excluded.user_command_role_id,
			bot_meta_channel_id = excluded.bot_meta_channel_id,
			updated_at = unixepoch()`,
		st.GuildID,
		st.WelcomeChannelID,
		st.BaseRoleID,
		st.RulesChannelID,
		st.PanelMessageID,
		st.PanelBody,
		st.UserCommandRoleID,
		st.BotMetaChannelID,
	)
	return err
}

// SetUserCommandRole records which role may run commands that are open to
// members. An empty role removes the grant. The upsert lets an admin configure
// it before the welcome panel exists.
func (s *Store) SetUserCommandRole(ctx context.Context, guildID, roleID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO guild_settings (guild_id, user_command_role_id, updated_at)
		VALUES (?, ?, unixepoch())
		ON CONFLICT(guild_id) DO UPDATE SET
			user_command_role_id = excluded.user_command_role_id,
			updated_at = unixepoch()`,
		guildID, roleID)
	return err
}

// SetBotMetaChannel records where the bot posts its build metadata. An empty
// channel turns the startup post off. The upsert lets an admin configure it
// before the welcome panel exists.
func (s *Store) SetBotMetaChannel(ctx context.Context, guildID, channelID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO guild_settings (guild_id, bot_meta_channel_id, updated_at)
		VALUES (?, ?, unixepoch())
		ON CONFLICT(guild_id) DO UPDATE SET
			bot_meta_channel_id = excluded.bot_meta_channel_id,
			updated_at = unixepoch()`,
		guildID, channelID)
	return err
}

// SetPanelMessage records the message ID of the live welcome panel.
func (s *Store) SetPanelMessage(ctx context.Context, guildID, messageID string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE guild_settings
		SET panel_message_id = ?, updated_at = unixepoch()
		WHERE guild_id = ?`, messageID, guildID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("no settings row for guild %s", guildID)
	}
	return nil
}

// RecordAcknowledgement audits a rule confirmation. It never gates access:
// the role is always granted from Discord's live member state.
func (s *Store) RecordAcknowledgement(ctx context.Context, guildID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO acknowledgements (guild_id, user_id, acknowledged_at)
		VALUES (?, ?, unixepoch())
		ON CONFLICT(guild_id, user_id) DO UPDATE SET
			acknowledged_at = excluded.acknowledged_at`,
		guildID, userID)
	return err
}

// CountAcknowledgements returns how many members have confirmed the rules.
func (s *Store) CountAcknowledgements(ctx context.Context, guildID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM acknowledgements WHERE guild_id = ?`, guildID).Scan(&count)
	return count, err
}
