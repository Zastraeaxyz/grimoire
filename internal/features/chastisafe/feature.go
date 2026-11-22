// Package chastisafe adds member-scoped ChastiSafe commands to Grimoire.
//
// Every member stores their own developer code after /login. Commands resolve
// that code for the caller only. The roleplay bot has its own separate
// keyholder client and never uses this package.
package chastisafe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"grimoire/internal/bot"
	cs "grimoire/internal/chastisafe"
	"grimoire/internal/cryptobox"
	"grimoire/internal/store"
)

var (
	errNotConfigured        = errors.New("chastisafe: credential store is not configured")
	errNotLoggedIn          = errors.New("chastisafe: member is not logged in")
	errCredentialUnreadable = errors.New("chastisafe: stored credential cannot be read")
	errNotOwner             = errors.New("chastisafe: code belongs to another Discord account")
)

// lockClient is the ChastiSafe surface the commands need.
type lockClient interface {
	Who(ctx context.Context) (*cs.Identity, error)
	ProfileFor(ctx context.Context, user string) (*cs.Profile, error)
	Loaded(ctx context.Context) ([]cs.LoadedLock, error)
	Lockees(ctx context.Context) ([]cs.LoadedLock, error)
	Templates(ctx context.Context) ([]cs.Template, error)
}

// Options configures the feature.
type Options struct {
	Store      *store.Store
	Box        *cryptobox.Box
	BaseURL    string
	UserAgent  string
	HTTPClient *http.Client
	Logger     *slog.Logger
	Now        func() time.Time
}

// Feature implements the member-scoped ChastiSafe commands.
type Feature struct {
	store     *store.Store
	box       *cryptobox.Box
	baseURL   string
	userAgent string
	http      *http.Client
	log       *slog.Logger

	newClient func(token string) (lockClient, error)
	limiter   *loginLimiter
}

// New builds the feature. A nil store or box leaves the commands registered
// but makes /login explain that the feature is not configured.
func New(opts Options) *Feature {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "grimoire-locks/1"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}

	f := &Feature{
		store:     opts.Store,
		box:       opts.Box,
		baseURL:   strings.TrimSpace(opts.BaseURL),
		userAgent: opts.UserAgent,
		http:      opts.HTTPClient,
		log:       opts.Logger,
		limiter:   newLoginLimiter(5, 10*time.Minute, opts.Now),
	}
	f.newClient = func(token string) (lockClient, error) {
		return cs.New(cs.Options{
			Token:      token,
			BaseURL:    f.baseURL,
			HTTPClient: f.http,
			UserAgent:  f.userAgent,
			Logger:     f.log,
		})
	}
	return f
}

// Register wires the feature into the main Grimoire bot.
func (f *Feature) Register(b *bot.Bot) {
	if b == nil {
		return
	}
	b.AddPublicCommand(loginCommand(), f.handleLoginCommand)
	b.AddPublicCommand(logoutCommand(), f.handleLogoutCommand)
	b.AddPublicCommand(lockeesCommand(), f.handleLockeesCommand)
	b.AddPublicCommand(locksCommand(), f.handleLocksCommand)
	b.AddPublicCommand(lookupCommand(), f.handleLookupCommand)
	b.AddPublicCommand(chastityCommand(), f.handleChastityCommand)
	b.AddModal(modalNamespace, f.handleLoginModal)
}

// String describes the feature without exposing configuration secrets.
func (f *Feature) String() string {
	return fmt.Sprintf("chastisafe(store=%t, box=%t)", f.store != nil, f.box != nil)
}

// credentialFor returns the caller's stored account and decrypted code.
func (f *Feature) credentialFor(ctx context.Context, userID string) (*store.ChastiSafeCredential, string, error) {
	if f.store == nil || f.box == nil {
		return nil, "", errNotConfigured
	}
	if strings.TrimSpace(userID) == "" {
		return nil, "", errNotLoggedIn
	}

	cred, err := f.store.GetChastiSafeCredential(ctx, userID)
	if err != nil {
		return nil, "", fmt.Errorf("loading a chastisafe credential: %w", err)
	}
	if cred == nil {
		return nil, "", errNotLoggedIn
	}

	secret, err := f.box.Open(cred.Secret)
	if err != nil {
		f.log.Warn("cannot decrypt a chastisafe credential", "user_id", userID, "err", err)
		return nil, "", errCredentialUnreadable
	}
	code := strings.TrimSpace(string(secret))
	if code == "" {
		return nil, "", errCredentialUnreadable
	}
	return cred, code, nil
}

// clientFor returns a ChastiSafe client bound to the caller's own code.
func (f *Feature) clientFor(ctx context.Context, userID string) (lockClient, *store.ChastiSafeCredential, error) {
	cred, code, err := f.credentialFor(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	client, err := f.newClient(code)
	if err != nil {
		return nil, nil, err
	}
	return client, cred, nil
}

// authenticate validates a submitted code and stores it for the member.
func (f *Feature) authenticate(ctx context.Context, userID, code string) (*cs.Identity, error) {
	if f.store == nil || f.box == nil {
		return nil, errNotConfigured
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, cs.ErrUnauthorized
	}
	if strings.TrimSpace(userID) == "" {
		return nil, errNotOwner
	}

	client, err := f.newClient(code)
	if err != nil {
		return nil, err
	}
	identity, err := client.Who(ctx)
	if err != nil {
		return nil, err
	}
	if !identity.OwnedBy(userID) {
		return nil, errNotOwner
	}

	sealed, err := f.box.Seal([]byte(code))
	if err != nil {
		return nil, fmt.Errorf("encrypting a chastisafe code: %w", err)
	}
	if err := f.store.SaveChastiSafeCredential(ctx, &store.ChastiSafeCredential{
		DiscordUserID:  userID,
		ChastiSafeID:   identity.ID.String(),
		ChastiSafeUser: identity.User,
		Secret:         sealed,
	}); err != nil {
		return nil, fmt.Errorf("saving a chastisafe credential: %w", err)
	}
	return identity, nil
}

// forget deletes the caller's stored code.
func (f *Feature) forget(ctx context.Context, userID string) error {
	if f.store == nil {
		return errNotConfigured
	}
	if err := f.store.DeleteChastiSafeCredential(ctx, userID); err != nil {
		return fmt.Errorf("deleting a chastisafe credential: %w", err)
	}
	return nil
}

// apiFailure turns a ChastiSafe error into a private reply. An unauthorized
// response also drops the saved code so the member cannot loop on it.
func (f *Feature) apiFailure(ctx context.Context, userID string, err error) string {
	if errors.Is(err, cs.ErrUnauthorized) {
		if f.store != nil {
			if delErr := f.store.DeleteChastiSafeCredential(ctx, userID); delErr != nil {
				f.log.Warn("deleting a rejected chastisafe credential", "user_id", userID, "err", delErr)
			}
		}
		f.log.Info("chastisafe rejected a saved code", "user_id", userID)
		return "ChastiSafe rejected your saved code. Run `/login` again."
	}
	if errors.Is(err, errNotLoggedIn) {
		return "Run `/login` first with your ChastiSafe developer code."
	}
	if errors.Is(err, errCredentialUnreadable) {
		return "I cannot read your saved ChastiSafe code. Run `/login` again."
	}
	if errors.Is(err, errNotConfigured) {
		return "ChastiSafe login is not configured on this bot."
	}
	if errors.Is(err, cs.ErrNotKeyholder) {
		return "This ChastiSafe account is not a keyholder, so it has no lockees to show."
	}
	f.log.Warn("chastisafe request failed", "user_id", userID, "err", err)
	return "I could not reach ChastiSafe right now. Try again in a moment."
}

// loginAttemptAllowed rate-limits failed login attempts per member.
func (f *Feature) loginAttemptAllowed(userID string) bool {
	return f.limiter.allow(userID)
}

func (f *Feature) loginSucceeded(userID string) {
	f.limiter.reset(userID)
}

// loginLimiter keeps a short failed-attempt window per member.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	limit    int
	window   time.Duration
	now      func() time.Time
}

func newLoginLimiter(limit int, window time.Duration, now func() time.Time) *loginLimiter {
	if now == nil {
		now = time.Now
	}
	if limit < 1 {
		limit = 1
	}
	return &loginLimiter{
		attempts: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
		now:      now,
	}
}

func (l *loginLimiter) allow(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	kept := l.attempts[userID][:0]
	for _, attempt := range l.attempts[userID] {
		if attempt.After(cutoff) {
			kept = append(kept, attempt)
		}
	}
	if len(kept) >= l.limit {
		l.attempts[userID] = kept
		return false
	}
	l.attempts[userID] = append(kept, now)
	return true
}

func (l *loginLimiter) reset(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, userID)
}
