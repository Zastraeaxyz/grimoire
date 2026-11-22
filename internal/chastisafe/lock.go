package chastisafe

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LockScope selects which running locks a list should include.
type LockScope string

const (
	ScopeMine    LockScope = "mine"
	ScopeLockees LockScope = "lockees"
	ScopeAll     LockScope = "all"
)

// LockState filters a running lock list.
type LockState string

const (
	StateActive LockState = "active"
	StateFrozen LockState = "frozen"
	StateReady  LockState = "ready"
)

// LockRole describes the caller's relationship to a lock.
type LockRole string

const (
	RoleUnknown   LockRole = ""
	RoleLockee    LockRole = "lockee"
	RoleKeyholder LockRole = "keyholder"
	RoleBoth      LockRole = "both"
)

// LockView attaches the caller's role to one running lock.
type LockView struct {
	Lock LoadedLock
	Role LockRole
}

// IsLockee reports whether the caller is the lockee.
func (v LockView) IsLockee() bool {
	return v.Role == RoleLockee || v.Role == RoleBoth
}

// IsKeyholder reports whether the caller is the keyholder.
func (v LockView) IsKeyholder() bool {
	return v.Role == RoleKeyholder || v.Role == RoleBoth
}

// LockFilter selects running locks for a list.
type LockFilter struct {
	Scope LockScope
	State LockState
}

// Locks returns running locks with the caller's role attached.
func (s *Service) Locks(ctx context.Context, filter LockFilter) ([]LockView, error) {
	locks, err := s.mergedLocks(ctx)
	if err != nil {
		return nil, err
	}
	identity, err := s.Identity(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]LockView, 0, len(locks))
	for i := range locks {
		view := LockView{Lock: locks[i], Role: roleFor(locks[i], identity)}
		if !filter.Scope.includes(view) {
			continue
		}
		if !filter.State.matches(&view.Lock) {
			continue
		}
		views = append(views, view)
	}

	sort.SliceStable(views, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(views[i].Lock.Lockee))
		right := strings.ToLower(strings.TrimSpace(views[j].Lock.Lockee))
		if left != right {
			return left < right
		}
		return strings.ToLower(views[i].Lock.LockName) < strings.ToLower(views[j].Lock.LockName)
	})
	return views, nil
}

func (scope LockScope) includes(view LockView) bool {
	switch scope {
	case ScopeMine:
		return view.IsLockee()
	case ScopeLockees:
		return view.IsKeyholder()
	case ScopeAll, "":
		return true
	default:
		return true
	}
}

func (state LockState) matches(lock *LoadedLock) bool {
	switch state {
	case StateFrozen:
		return lock.IsFrozen()
	case StateReady:
		return lock.IsReady()
	case StateActive:
		return !lock.IsFrozen() && !lock.IsReady()
	case "":
		return true
	default:
		return true
	}
}

// Lock resolves one running lock by ID, name, lockee name, or a unique prefix.
func (s *Service) Lock(ctx context.Context, ref string) (*LockView, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrRefEmpty
	}
	locks, err := s.mergedLocks(ctx)
	if err != nil {
		return nil, err
	}
	identity, _ := s.Identity(ctx)

	for i := range locks {
		if locks[i].ID.String() == ref {
			view := LockView{Lock: locks[i], Role: roleFor(locks[i], identity)}
			return &view, nil
		}
	}

	exact, prefix := matchLocks(locks, ref)
	matches := exact
	if len(matches) == 0 {
		matches = prefix
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: lock %q", ErrRefNotFound, ref)
	case 1:
		view := LockView{Lock: matches[0], Role: roleFor(matches[0], identity)}
		return &view, nil
	default:
		return nil, &AmbiguousRefError{Ref: ref, Candidates: lockCandidates(matches)}
	}
}

func matchLocks(locks []LoadedLock, ref string) (exact, prefix []LoadedLock) {
	want := strings.ToLower(ref)
	for _, lock := range locks {
		name := strings.ToLower(strings.TrimSpace(lock.LockName))
		lockee := strings.ToLower(strings.TrimSpace(lock.Lockee))
		keyholder := strings.ToLower(strings.TrimSpace(lock.Keyholder))
		id := strings.ToLower(lock.ID.String())
		switch {
		case id == want || name == want || lockee == want || keyholder == want:
			exact = append(exact, lock)
		case strings.HasPrefix(id, want) || strings.HasPrefix(name, want) ||
			strings.HasPrefix(lockee, want) || strings.HasPrefix(keyholder, want):
			prefix = append(prefix, lock)
		}
	}
	return exact, prefix
}

func lockCandidates(locks []LoadedLock) []string {
	out := make([]string, 0, len(locks))
	for _, lock := range locks {
		name := strings.TrimSpace(lock.LockName)
		if name == "" {
			name = "lock"
		}
		lockee := strings.TrimSpace(lock.Lockee)
		if lockee == "" {
			lockee = "unknown lockee"
		}
		out = append(out, fmt.Sprintf("%s for %s (%s)", name, lockee, lock.ID))
	}
	return out
}

func roleFor(lock LoadedLock, identity *Identity) LockRole {
	if identity == nil || identity.ID == "" {
		return RoleUnknown
	}
	me := identity.ID.String()
	isLockee := lock.LockeeID.String() == me
	isKeyholder := lock.KeyholderID.String() == me
	switch {
	case isLockee && isKeyholder:
		return RoleBoth
	case isLockee:
		return RoleLockee
	case isKeyholder:
		return RoleKeyholder
	default:
		return RoleUnknown
	}
}

// LoadLock starts a lock from a template reference.
func (s *Service) LoadLock(ctx context.Context, templateRef, code string, purpose Purpose) ([]LoadedLock, error) {
	template, err := s.ResolveTemplate(ctx, templateRef)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("chastisafe: a lock code is required")
	}
	locks, err := s.client.Load(ctx, LoadRequest{
		TemplateID: template.ID,
		Code:       strings.TrimSpace(code),
		Purpose:    purpose,
	})
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return locks, nil
}

// AddLockTime adds or removes time from a running timer lock.
func (s *Service) AddLockTime(ctx context.Context, ref string, delta time.Duration) (*LoadedLock, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	updated, err := s.client.AddTime(ctx, view.Lock.ID, delta)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return s.refreshLock(ctx, view.Lock.ID, updated)
}

// SetLockTime replaces the remaining time on a running timer lock.
func (s *Service) SetLockTime(ctx context.Context, ref string, remaining time.Duration) (*LoadedLock, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	updated, err := s.client.SetRemainingTime(ctx, view.Lock.ID, remaining)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return s.refreshLock(ctx, view.Lock.ID, updated)
}

// FreezeLock freezes or thaws a running lock.
func (s *Service) FreezeLock(ctx context.Context, ref string, frozen bool) (*LoadedLock, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	updated, err := s.client.Freeze(ctx, view.Lock.ID, frozen)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return s.refreshLock(ctx, view.Lock.ID, updated)
}

// HideLock hides or reveals a lock's timer or cards.
func (s *Service) HideLock(ctx context.Context, ref string, hidden bool) (*LoadedLock, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	updated, err := s.client.Hide(ctx, view.Lock.ID, hidden)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return s.refreshLock(ctx, view.Lock.ID, updated)
}

// SetLockPurpose changes a running lock's purpose.
func (s *Service) SetLockPurpose(ctx context.Context, ref string, purpose Purpose) (*LoadedLock, error) {
	if !purpose.Valid() {
		return nil, fmt.Errorf("chastisafe: unknown purpose %q", purpose)
	}
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	updated, err := s.client.SetPurpose(ctx, view.Lock.ID, purpose)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return s.refreshLock(ctx, view.Lock.ID, updated)
}

// UpdateLockCards adds or removes cards.
func (s *Service) UpdateLockCards(ctx context.Context, ref string, changes []CardChange, opts UpdateOptions) (*LoadedLock, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	updated, err := s.client.UpdateCards(ctx, view.Lock.ID, changes, opts)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return s.refreshLock(ctx, view.Lock.ID, updated)
}

// UpdateLockSettings applies a keyholder settings patch.
func (s *Service) UpdateLockSettings(ctx context.Context, ref string, patch LockSettingsPatch) (*LoadedLock, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	settings, err := patch.keyholderSettings()
	if err != nil {
		return nil, err
	}
	updated, err := s.client.UpdateSettings(ctx, view.Lock.ID, settings)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return s.refreshLock(ctx, view.Lock.ID, updated)
}

// UnlockLock asks ChastiSafe to unlock a ready lock as its keyholder. The
// returned code is intentionally left to the caller to ignore.
func (s *Service) UnlockLock(ctx context.Context, ref string) (*OpenResult, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	result, err := s.client.Unlock(ctx, view.Lock.ID)
	if err != nil {
		return result, err
	}
	s.invalidateLocks()
	return result, nil
}

// ResetLock restarts a running lock.
func (s *Service) ResetLock(ctx context.Context, ref string) (*OpenResult, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	result, err := s.client.Reset(ctx, view.Lock.ID)
	if err != nil {
		return result, err
	}
	s.invalidateLocks()
	return result, nil
}

// AbandonLock ends a running lock without revealing a code.
func (s *Service) AbandonLock(ctx context.Context, ref string) error {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return err
	}
	if err := s.client.Abandon(ctx, view.Lock.ID); err != nil {
		return err
	}
	s.invalidateLocks()
	return nil
}

// SplitLock creates fake locks from a running card lock.
func (s *Service) SplitLock(ctx context.Context, ref string, count int) ([]LoadedLock, error) {
	view, err := s.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	locks, err := s.client.Split(ctx, view.Lock.ID, count)
	if err != nil {
		return nil, err
	}
	s.invalidateLocks()
	return locks, nil
}

// refreshLock returns the updated lock, falling back to a fresh lookup when
// ChastiSafe only reported success.
func (s *Service) refreshLock(ctx context.Context, id ID, updated *LoadedLock) (*LoadedLock, error) {
	if updated != nil {
		return updated, nil
	}
	locks, err := s.mergedLocks(ctx)
	if err != nil {
		return nil, err
	}
	for i := range locks {
		if locks[i].ID == id {
			copy := locks[i]
			return &copy, nil
		}
	}
	return nil, nil
}

// LockSettingsPatch is a typed keyholder settings update. Durations use the
// same units as KeyholderSettings.
type LockSettingsPatch struct {
	MaxPicks              *int
	CurrentPicks          *int
	Regularity            *time.Duration
	CheckInPeriod         *time.Duration
	CheckInTolerance      *time.Duration
	TimeToNextCheckIn     *time.Duration
	CheckInMissPenalty    *string
	HygieneUnlockPeriod   *time.Duration
	HygieneUnlockDuration *time.Duration
	TimeToHygieneUnlock   *time.Duration
	HygieneRelockPenalty  *string
	AutoResetPeriod       *time.Duration
	AutoResetCount        *int
	BotID                 *ID
	TaskID                *ID
	MissedTaskPenalty     *string
	RemainingTime         *time.Duration
	AddTime               *time.Duration
	LockPurpose           *Purpose
}

func (p LockSettingsPatch) keyholderSettings() (KeyholderSettings, error) {
	var out KeyholderSettings
	empty := true

	out.MaxPicks = p.MaxPicks
	out.CurrentPicks = p.CurrentPicks
	out.CheckInMissPenalty = p.CheckInMissPenalty
	out.HygieneRelockPenalty = p.HygieneRelockPenalty
	out.AutoResetCount = p.AutoResetCount
	out.BotID = stringPtr(p.BotID)
	out.TaskID = stringPtr(p.TaskID)
	out.MissedTaskPenalty = p.MissedTaskPenalty
	if p.BotID != nil || p.TaskID != nil || p.MaxPicks != nil || p.CurrentPicks != nil ||
		p.CheckInMissPenalty != nil || p.HygieneRelockPenalty != nil ||
		p.AutoResetCount != nil || p.MissedTaskPenalty != nil {
		empty = false
	}

	for key, value := range map[string]*time.Duration{
		"regularity":            p.Regularity,
		"checkinperiod":         p.CheckInPeriod,
		"checkintolerance":      p.CheckInTolerance,
		"hygieneunlockperiod":   p.HygieneUnlockPeriod,
		"hygieneunlockduration": p.HygieneUnlockDuration,
		"autoresetperiod":       p.AutoResetPeriod,
	} {
		if value == nil {
			continue
		}
		minutes, err := wholeMinutes(*value)
		if err != nil {
			return out, fmt.Errorf("chastisafe: %s: %w", key, err)
		}
		switch key {
		case "regularity":
			out.Regularity = intPtr(minutes)
		case "checkinperiod":
			out.CheckInPeriod = intPtr(minutes)
		case "checkintolerance":
			out.CheckInTolerance = intPtr(minutes)
		case "hygieneunlockperiod":
			out.HygieneUnlockPeriod = intPtr(minutes)
		case "hygieneunlockduration":
			out.HygieneUnlockDuration = intPtr(minutes)
		case "autoresetperiod":
			out.AutoResetPeriod = intPtr(minutes)
		}
		empty = false
	}

	for key, value := range map[string]*time.Duration{
		"timetonextcheckin":   p.TimeToNextCheckIn,
		"timetohygieneunlock": p.TimeToHygieneUnlock,
		"remainingtime":       p.RemainingTime,
		"addtime":             p.AddTime,
	} {
		if value == nil {
			continue
		}
		seconds, err := wholeSeconds(*value)
		if err != nil {
			return out, fmt.Errorf("chastisafe: %s: %w", key, err)
		}
		switch key {
		case "timetonextcheckin":
			out.TimeToNextCheckIn = intPtr(seconds)
		case "timetohygieneunlock":
			out.TimeToHygieneUnlock = intPtr(seconds)
		case "remainingtime":
			out.RemainingTime = intPtr(seconds)
		case "addtime":
			out.AddTime = intPtr(seconds)
		}
		empty = false
	}

	if p.LockPurpose != nil {
		if !p.LockPurpose.Valid() {
			return out, fmt.Errorf("chastisafe: unknown purpose %q", *p.LockPurpose)
		}
		out.LockPurpose = p.LockPurpose
		empty = false
	}
	if empty {
		return out, errors.New("chastisafe: no lock settings were supplied")
	}
	return out, nil
}

func intPtr(value int) *int { return &value }

func stringPtr(value *ID) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}

// Validate checks a settings patch without sending it.
func (p LockSettingsPatch) Validate() error {
	_, err := p.keyholderSettings()
	return err
}
