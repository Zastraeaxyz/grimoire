package chastisafe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrRefEmpty means a lookup was attempted with a blank reference.
var ErrRefEmpty = errors.New("chastisafe: reference is empty")

// ErrRefNotFound means no template or lock matched a reference.
var ErrRefNotFound = errors.New("chastisafe: reference not found")

// ErrRefAmbiguous means more than one template or lock matched a reference.
var ErrRefAmbiguous = errors.New("chastisafe: reference is ambiguous")

// AmbiguousRefError carries the candidates for a short reference.
type AmbiguousRefError struct {
	Ref        string
	Candidates []string
}

func (e *AmbiguousRefError) Error() string {
	return fmt.Sprintf("chastisafe: %q matches %s", e.Ref, strings.Join(e.Candidates, ", "))
}

// Is maps the error onto ErrRefAmbiguous.
func (e *AmbiguousRefError) Is(target error) bool { return target == ErrRefAmbiguous }

// API is the primitive surface the Service uses. Client implements it, and
// tests can supply a smaller fake by embedding Client.
type API interface {
	Who(ctx context.Context) (*Identity, error)
	Profile(ctx context.Context) (*Profile, error)
	ProfileFor(ctx context.Context, user string) (*Profile, error)
	Templates(ctx context.Context) ([]Template, error)
	Template(ctx context.Context, id ID) (*Template, error)
	TemplateForEdit(ctx context.Context, id ID) (*Template, error)
	Loaded(ctx context.Context) ([]LoadedLock, error)
	Lockees(ctx context.Context) ([]LoadedLock, error)
	Load(ctx context.Context, req LoadRequest) ([]LoadedLock, error)
	AddTime(ctx context.Context, lockID ID, delta time.Duration) (*LoadedLock, error)
	SetRemainingTime(ctx context.Context, lockID ID, remaining time.Duration) (*LoadedLock, error)
	Freeze(ctx context.Context, lockID ID, frozen bool) (*LoadedLock, error)
	Hide(ctx context.Context, lockID ID, hidden bool) (*LoadedLock, error)
	SetPurpose(ctx context.Context, lockID ID, purpose Purpose) (*LoadedLock, error)
	UpdateSettings(ctx context.Context, lockID ID, settings KeyholderSettings) (*LoadedLock, error)
	UpdateCards(ctx context.Context, lockID ID, changes []CardChange, opts UpdateOptions) (*LoadedLock, error)
	Unlock(ctx context.Context, lockID ID) (*OpenResult, error)
	Reset(ctx context.Context, lockID ID) (*OpenResult, error)
	Abandon(ctx context.Context, lockID ID) error
	Split(ctx context.Context, lockID ID, count int) ([]LoadedLock, error)
	CreateTemplate(ctx context.Context, spec map[string]any) error
	UpdateTemplate(ctx context.Context, spec map[string]any) error
	DeleteTemplate(ctx context.Context, templateID ID) error
	UpdateCardRange(ctx context.Context, templateID ID, change CardRange) error
	SetBackground(ctx context.Context, templateID ID, background string) error
	Simulate(ctx context.Context, templateID ID) (*Simulation, error)
}

// Snapshot is a point-in-time view of one account.
type Snapshot struct {
	Identity  *Identity
	Templates []Template
	Loaded    []LoadedLock
	Lockees   []LoadedLock
}

// Service adds caching, reference resolution, and higher-level operations on
// top of the primitive Client.
type Service struct {
	client API
	ttl    time.Duration
	now    func() time.Time

	mu          sync.Mutex
	identity    *Identity
	identityAt  time.Time
	templates   []Template
	templatesAt time.Time
	loaded      []LoadedLock
	loadedAt    time.Time
	lockees     []LoadedLock
	lockeesAt   time.Time
}

// NewService wraps a primitive client. Callers normally create one per member.
func NewService(client API) *Service {
	return &Service{
		client: client,
		ttl:    20 * time.Second,
		now:    time.Now,
	}
}

// SetTTL changes the snapshot cache lifetime. Zero disables caching.
func (s *Service) SetTTL(ttl time.Duration) {
	if s == nil {
		return
	}
	s.ttl = ttl
}

// SetClock replaces the clock used by the cache. It is for tests.
func (s *Service) SetClock(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

// Client returns the primitive client behind the service.
func (s *Service) Client() API {
	if s == nil {
		return nil
	}
	return s.client
}

// Invalidate clears every cached snapshot.
func (s *Service) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identity = nil
	s.identityAt = time.Time{}
	s.templates = nil
	s.templatesAt = time.Time{}
	s.loaded = nil
	s.loadedAt = time.Time{}
	s.lockees = nil
	s.lockeesAt = time.Time{}
}

func (s *Service) invalidateLocks() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded = nil
	s.loadedAt = time.Time{}
	s.lockees = nil
	s.lockeesAt = time.Time{}
}

func (s *Service) invalidateTemplates() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.templates = nil
	s.templatesAt = time.Time{}
}

func (s *Service) fresh(at time.Time) bool {
	return s.ttl > 0 && !at.IsZero() && s.now().Sub(at) < s.ttl
}

// Identity returns the account attached to the client.
func (s *Service) Identity(ctx context.Context) (*Identity, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chastisafe: service has no client")
	}

	s.mu.Lock()
	if s.fresh(s.identityAt) && s.identity != nil {
		identity := *s.identity
		s.mu.Unlock()
		return &identity, nil
	}
	s.mu.Unlock()

	identity, err := s.client.Who(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.identity = identity
	s.identityAt = s.now()
	s.mu.Unlock()
	copy := *identity
	return &copy, nil
}

// Profile returns the caller's own profile.
func (s *Service) Profile(ctx context.Context) (*Profile, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chastisafe: service has no client")
	}
	return s.client.Profile(ctx)
}

// ProfileFor returns another account's public profile.
func (s *Service) ProfileFor(ctx context.Context, user string) (*Profile, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chastisafe: service has no client")
	}
	return s.client.ProfileFor(ctx, user)
}

// Templates returns the account's lock templates.
func (s *Service) Templates(ctx context.Context) ([]Template, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chastisafe: service has no client")
	}

	s.mu.Lock()
	if s.fresh(s.templatesAt) {
		out := append([]Template(nil), s.templates...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	templates, err := s.client.Templates(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.templates = append([]Template(nil), templates...)
	s.templatesAt = s.now()
	s.mu.Unlock()
	return templates, nil
}

// Loaded returns active locks where the account is the lockee or keyholder.
func (s *Service) Loaded(ctx context.Context) ([]LoadedLock, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chastisafe: service has no client")
	}

	s.mu.Lock()
	if s.fresh(s.loadedAt) {
		out := append([]LoadedLock(nil), s.loaded...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	locks, err := s.client.Loaded(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.loaded = append([]LoadedLock(nil), locks...)
	s.loadedAt = s.now()
	s.mu.Unlock()
	return locks, nil
}

// Lockees returns active locks where the account is the keyholder.
func (s *Service) Lockees(ctx context.Context) ([]LoadedLock, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chastisafe: service has no client")
	}

	s.mu.Lock()
	if s.fresh(s.lockeesAt) {
		out := append([]LoadedLock(nil), s.lockees...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	locks, err := s.client.Lockees(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.lockees = append([]LoadedLock(nil), locks...)
	s.lockeesAt = s.now()
	s.mu.Unlock()
	return locks, nil
}

// Snapshot loads identity, templates, loaded locks, and lockees together.
func (s *Service) Snapshot(ctx context.Context) (*Snapshot, error) {
	identity, err := s.Identity(ctx)
	if err != nil {
		return nil, err
	}
	templates, err := s.Templates(ctx)
	if err != nil {
		return nil, err
	}
	loaded, err := s.Loaded(ctx)
	if err != nil {
		return nil, err
	}
	lockees, err := s.Lockees(ctx)
	if err != nil {
		return nil, err
	}
	return &Snapshot{
		Identity:  identity,
		Templates: templates,
		Loaded:    loaded,
		Lockees:   lockees,
	}, nil
}

// ResolveTemplate finds one template by ID, exact name, unique name prefix, or
// unique ID prefix.
func (s *Service) ResolveTemplate(ctx context.Context, ref string) (*Template, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrRefEmpty
	}
	templates, err := s.Templates(ctx)
	if err != nil {
		return nil, err
	}

	for i := range templates {
		if templates[i].ID.String() == ref {
			copy := templates[i]
			return &copy, nil
		}
	}

	exact, prefix := matchTemplates(templates, ref)
	matches := exact
	if len(matches) == 0 {
		matches = prefix
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: template %q", ErrRefNotFound, ref)
	case 1:
		copy := matches[0]
		return &copy, nil
	default:
		return nil, &AmbiguousRefError{Ref: ref, Candidates: templateCandidates(matches)}
	}
}

func matchTemplates(templates []Template, ref string) (exact, prefix []Template) {
	want := strings.ToLower(ref)
	for _, template := range templates {
		name := strings.ToLower(strings.TrimSpace(template.LockName))
		id := strings.ToLower(template.ID.String())
		switch {
		case name == want || id == want:
			exact = append(exact, template)
		case strings.HasPrefix(name, want) || strings.HasPrefix(id, want):
			prefix = append(prefix, template)
		}
	}
	return exact, prefix
}

func templateCandidates(templates []Template) []string {
	out := make([]string, 0, len(templates))
	for _, template := range templates {
		name := strings.TrimSpace(template.LockName)
		if name == "" {
			name = "Untitled lock"
		}
		out = append(out, fmt.Sprintf("%s (%s)", name, template.ID))
	}
	return out
}

// mergedLocks combines the loaded and lockee views without duplicate IDs.
func (s *Service) mergedLocks(ctx context.Context) ([]LoadedLock, error) {
	type result struct {
		locks []LoadedLock
		err   error
	}

	loadedCh := make(chan result, 1)
	lockeesCh := make(chan result, 1)
	go func() {
		locks, err := s.Loaded(ctx)
		loadedCh <- result{locks: locks, err: err}
	}()
	go func() {
		locks, err := s.Lockees(ctx)
		lockeesCh <- result{locks: locks, err: err}
	}()

	loadedResult := <-loadedCh
	lockeesResult := <-lockeesCh
	if loadedResult.err != nil {
		return nil, loadedResult.err
	}
	loaded, lockees := loadedResult.locks, lockeesResult.locks
	// ChastiSafe can reject /lockees for an account that is only a lockee.
	// The combined running-lock list should still show that account's locks.
	if lockeesResult.err != nil && !errors.Is(lockeesResult.err, ErrNotKeyholder) && !errors.Is(lockeesResult.err, ErrNotAllowed) {
		return nil, lockeesResult.err
	}

	seen := make(map[string]struct{}, len(loaded)+len(lockees))
	out := make([]LoadedLock, 0, len(loaded)+len(lockees))
	for _, lock := range append(append([]LoadedLock(nil), loaded...), lockees...) {
		key := lock.ID.String()
		if key == "" {
			key = lock.LockName + "|" + lock.LockeeID.String() + "|" + lock.KeyholderID.String()
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, lock)
	}
	return out, nil
}
