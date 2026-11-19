package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BDSMLRConfirmation is a pending invitation that a member has not confirmed
// yet. The email lives only until the confirmation is used, canceled, or
// expires.
type BDSMLRConfirmation struct {
	Token     string
	GuildID   string
	ChannelID string
	UserID    string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateBDSMLRConfirmation stores a pending invitation confirmation.
func (s *Store) CreateBDSMLRConfirmation(ctx context.Context, c *BDSMLRConfirmation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO bdsmlr_invite_confirmations (
			token, guild_id, channel_id, user_id, email, expires_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		c.Token, c.GuildID, c.ChannelID, c.UserID, c.Email, c.ExpiresAt.Unix())
	return err
}

// GetBDSMLRConfirmation returns a confirmation by token, or (nil, nil) when
// the token is unknown.
func (s *Store) GetBDSMLRConfirmation(ctx context.Context, token string) (*BDSMLRConfirmation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT token, guild_id, channel_id, user_id, email, created_at, expires_at
		FROM bdsmlr_invite_confirmations
		WHERE token = ?`, token)

	var (
		c         BDSMLRConfirmation
		createdAt int64
		expiresAt int64
	)
	err := row.Scan(&c.Token, &c.GuildID, &c.ChannelID, &c.UserID, &c.Email, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt = time.Unix(createdAt, 0).UTC()
	c.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return &c, nil
}

// DeleteBDSMLRConfirmation removes a pending confirmation. It is safe to call
// for a token that is already gone.
func (s *Store) DeleteBDSMLRConfirmation(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM bdsmlr_invite_confirmations WHERE token = ?`, token)
	return err
}

// DeleteBDSMLRConfirmationsForUser clears a member's pending confirmation so
// only the newest one can be used.
func (s *Store) DeleteBDSMLRConfirmationsForUser(ctx context.Context, guildID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM bdsmlr_invite_confirmations
		WHERE guild_id = ? AND user_id = ?`, guildID, userID)
	return err
}

// PruneBDSMLRConfirmations drops confirmations whose window has passed.
func (s *Store) PruneBDSMLRConfirmations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM bdsmlr_invite_confirmations WHERE expires_at <= unixepoch()`)
	return err
}

// RecordBDSMLRInvite records a successful invitation for the rate limit. The
// invited email is deliberately not kept here.
func (s *Store) RecordBDSMLRInvite(ctx context.Context, guildID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO bdsmlr_invites (guild_id, user_id) VALUES (?, ?)`,
		guildID, userID)
	return err
}

// LastBDSMLRInvite returns when this member last sent an invitation, or the
// zero time when they never have.
func (s *Store) LastBDSMLRInvite(ctx context.Context, guildID, userID string) (time.Time, error) {
	var sentAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT sent_at FROM bdsmlr_invites
		WHERE guild_id = ? AND user_id = ?
		ORDER BY sent_at DESC
		LIMIT 1`, guildID, userID).Scan(&sentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sentAt, 0).UTC(), nil
}
