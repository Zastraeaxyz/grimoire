package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ChastiSafeCredential is one member's encrypted ChastiSafe developer code.
// Secret is ciphertext; the store never receives or returns a plaintext code.
type ChastiSafeCredential struct {
	DiscordUserID  string
	ChastiSafeID   string
	ChastiSafeUser string
	Secret         []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SaveChastiSafeCredential inserts or replaces a member's encrypted code.
func (s *Store) SaveChastiSafeCredential(ctx context.Context, cred *ChastiSafeCredential) error {
	if cred == nil {
		return errors.New("store: credential is nil")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chastisafe_credentials (
			discord_user_id, chastisafe_id, chastisafe_user, secret, updated_at
		) VALUES (?, ?, ?, ?, unixepoch())
		ON CONFLICT(discord_user_id) DO UPDATE SET
			chastisafe_id = excluded.chastisafe_id,
			chastisafe_user = excluded.chastisafe_user,
			secret = excluded.secret,
			updated_at = unixepoch()`,
		cred.DiscordUserID,
		cred.ChastiSafeID,
		cred.ChastiSafeUser,
		cred.Secret,
	)
	return err
}

// GetChastiSafeCredential returns a member's credential, or (nil, nil) when
// they have not logged in.
func (s *Store) GetChastiSafeCredential(ctx context.Context, discordUserID string) (*ChastiSafeCredential, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT discord_user_id, chastisafe_id, chastisafe_user, secret,
		       created_at, updated_at
		FROM chastisafe_credentials
		WHERE discord_user_id = ?`, discordUserID)

	var (
		cred                 ChastiSafeCredential
		createdAt, updatedAt int64
	)
	err := row.Scan(
		&cred.DiscordUserID,
		&cred.ChastiSafeID,
		&cred.ChastiSafeUser,
		&cred.Secret,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cred.CreatedAt = time.Unix(createdAt, 0).UTC()
	cred.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &cred, nil
}

// DeleteChastiSafeCredential removes a member's stored code. Deleting a
// missing row is not an error.
func (s *Store) DeleteChastiSafeCredential(ctx context.Context, discordUserID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM chastisafe_credentials WHERE discord_user_id = ?`,
		discordUserID,
	)
	return err
}
