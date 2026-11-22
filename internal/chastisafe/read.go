package chastisafe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Who returns the account attached to the client token.
func (c *Client) Who(ctx context.Context) (*Identity, error) {
	const op = "GET /who"
	body, err := c.get(ctx, "/who")
	if err != nil {
		return nil, err
	}
	var out Identity
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		apiErr := newAPIError(op, 200, nil, body)
		if apiErr.Reason == "" {
			apiErr.Reason = "unauthorized"
		}
		return nil, apiErr
	}
	return &out, nil
}

// Profile returns the account's profile.
func (c *Client) Profile(ctx context.Context) (*Profile, error) {
	return c.profile(ctx, "/profile")
}

// ProfileFor returns another account's public profile. ChastiSafe returns the
// caller's own profile when the requested account has hidden its profile, so
// ProfileFor verifies the returned account and returns ErrProfileNotPublic in
// that case.
func (c *Client) ProfileFor(ctx context.Context, user string) (*Profile, error) {
	key := userKey(user)
	if key == "" {
		return nil, fmt.Errorf("chastisafe: profile user is empty")
	}
	profile, err := c.profile(ctx, "/profile/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	if !profileMatchesKey(profile, key) {
		return nil, fmt.Errorf("%w: requested %s, got %s", ErrProfileNotPublic, key, profile.ID)
	}
	return profile, nil
}

func profileMatchesKey(profile *Profile, key string) bool {
	if profile == nil {
		return false
	}
	if _, err := strconv.ParseUint(key, 10, 64); err == nil {
		return profile.ID.String() == key
	}
	got := strings.TrimSpace(profile.User)
	if index := strings.IndexByte(got, '#'); index > 0 {
		got = got[:index]
	}
	return strings.EqualFold(got, key)
}

func (c *Client) profile(ctx context.Context, path string) (*Profile, error) {
	const op = "GET /profile"
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var out Profile
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &out, nil
}

// Templates returns the lock templates the account can manage.
func (c *Client) Templates(ctx context.Context) ([]Template, error) {
	return c.templateList(ctx, "/locks")
}

// LoadableByKeyholder returns the templates a keyholder offers for loading.
func (c *Client) LoadableByKeyholder(ctx context.Context, keyholderID string) ([]Template, error) {
	return c.templateList(ctx, "/loadable/"+url.PathEscape(strings.TrimSpace(keyholderID)))
}

func (c *Client) templateList(ctx context.Context, path string) ([]Template, error) {
	const op = "GET /locks"
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var out []Template
	if err := json.Unmarshal(body, &out); err != nil {
		if present, ok, _ := statusOf(body); present && !ok {
			return nil, newAPIError(op, 200, nil, body)
		}
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidResponse)
	}
	return out, nil
}

// Template returns one template.
func (c *Client) Template(ctx context.Context, id ID) (*Template, error) {
	return c.template(ctx, "/getlock/"+url.PathEscape(id.String()))
}

// TemplateForEdit returns one template including private edit fields.
func (c *Client) TemplateForEdit(ctx context.Context, id ID) (*Template, error) {
	return c.template(ctx, "/getlockforedit/"+url.PathEscape(id.String()))
}

func (c *Client) template(ctx context.Context, path string) (*Template, error) {
	const op = "GET /getlock"
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var out Template
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.ID == "" {
		return nil, newAPIError(op, 200, nil, body)
	}
	return &out, nil
}

// Loaded returns every active lock where the account is the lockee or the
// keyholder.
func (c *Client) Loaded(ctx context.Context) ([]LoadedLock, error) {
	const op = "GET /loaded"
	body, err := c.get(ctx, "/loaded")
	if err != nil {
		return nil, err
	}
	var out []LoadedLock
	if err := json.Unmarshal(body, &out); err != nil {
		if present, ok, _ := statusOf(body); present && !ok {
			return nil, newAPIError(op, 200, nil, body)
		}
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidResponse)
	}
	return out, nil
}

// LoadedLock returns one active lock and whether the caller is its lockee.
func (c *Client) LoadedLock(ctx context.Context, id ID) (*LoadedLock, bool, error) {
	const op = "GET /loaded/{id}"
	body, err := c.get(ctx, "/loaded/"+url.PathEscape(id.String()))
	if err != nil {
		return nil, false, err
	}
	var out struct {
		Status   Status      `json:"status"`
		Reason   string      `json:"reason"`
		Lock     *LoadedLock `json:"lock"`
		IsLockee bool        `json:"isLockee"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, false, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK || out.Lock == nil {
		return nil, out.IsLockee, newAPIError(op, 200, nil, body)
	}
	return out.Lock, out.IsLockee, nil
}

// Lockees returns the active locks of the account's lockees.
func (c *Client) Lockees(ctx context.Context) ([]LoadedLock, error) {
	const op = "GET /lockees"
	body, err := c.get(ctx, "/lockees")
	if err != nil {
		return nil, err
	}
	var out []LoadedLock
	if err := json.Unmarshal(body, &out); err != nil {
		if present, ok, _ := statusOf(body); present && !ok {
			return nil, newAPIError(op, 200, nil, body)
		}
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidResponse)
	}
	return out, nil
}

// Lockee returns one active lock from the keyholder's lockee list.
func (c *Client) Lockee(ctx context.Context, lockID ID) (*LoadedLock, error) {
	const op = "GET /lockee/{id}"
	body, err := c.get(ctx, "/lockee/"+url.PathEscape(lockID.String()))
	if err != nil {
		return nil, err
	}
	var out []LoadedLock
	if err := json.Unmarshal(body, &out); err != nil {
		if present, ok, _ := statusOf(body); present && !ok {
			return nil, newAPIError(op, 200, nil, body)
		}
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidResponse)
	}
	if len(out) == 0 {
		return nil, ErrLockNotFound
	}
	return &out[0], nil
}

// Recent returns the account's completed lock history.
func (c *Client) Recent(ctx context.Context) ([]RecentLock, error) {
	return c.recent(ctx, "/recent")
}

// RecentFor returns another account's completed lock history.
func (c *Client) RecentFor(ctx context.Context, user string) ([]RecentLock, error) {
	return c.recent(ctx, "/recent/"+url.PathEscape(userKey(user)))
}

func (c *Client) recent(ctx context.Context, path string) ([]RecentLock, error) {
	const op = "GET /recent"
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var out []RecentLock
	if err := json.Unmarshal(body, &out); err != nil {
		if present, ok, _ := statusOf(body); present && !ok {
			return nil, newAPIError(op, 200, nil, body)
		}
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidResponse)
	}
	return out, nil
}

// userKey strips the #discriminator that ChastiSafe appends to usernames.
func userKey(user string) string {
	user = strings.TrimSpace(user)
	if index := strings.IndexByte(user, '#'); index > 0 {
		user = user[:index]
	}
	return user
}

// Log returns lock history entries.
func (c *Client) Log(ctx context.Context, lockID ID, from, count int) ([]LogEntry, error) {
	const op = "POST /log"
	if count <= 0 {
		count = 100
	}
	body, err := c.post(ctx, "/log", map[string]any{
		"lockId": lockID,
		"from":   from,
		"count":  count,
	})
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	if err := json.Unmarshal(body, &out); err != nil {
		if present, ok, _ := statusOf(body); present && !ok {
			return nil, newAPIError(op, 200, nil, body)
		}
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidResponse)
	}
	return out, nil
}

// Tasks returns the account's task rollers.
func (c *Client) Tasks(ctx context.Context) ([]Task, error) {
	const op = "GET /tasks"
	body, err := c.get(ctx, "/tasks")
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Status Status `json:"status"`
		Tasks  []Task `json:"tasks"`
	}
	if err := decodeJSON(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if envelope.Status != StatusOK {
		return nil, newAPIError(op, 200, nil, body)
	}
	return envelope.Tasks, nil
}

// Task returns one task roller.
func (c *Client) Task(ctx context.Context, id ID) (*Task, error) {
	const op = "GET /task/{id}"
	body, err := c.get(ctx, "/task/"+url.PathEscape(id.String()))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Status Status `json:"status"`
		Task   *Task  `json:"task"`
	}
	if err := decodeJSON(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if envelope.Task == nil {
		return nil, newAPIError(op, 200, nil, body)
	}
	return envelope.Task, nil
}

// TaskStatus returns the task state for a loaded lock.
func (c *Client) TaskStatus(ctx context.Context, lockID ID) (*TaskStatus, error) {
	const op = "GET /taskstatus/{id}"
	body, err := c.get(ctx, "/taskstatus/"+url.PathEscape(lockID.String()))
	if err != nil {
		return nil, err
	}
	var out TaskStatus
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &out, nil
}

// Pillories returns the account's active pillory sessions.
func (c *Client) Pillories(ctx context.Context) ([]Pillory, error) {
	const op = "GET /pillory"
	body, err := c.get(ctx, "/pillory")
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Pillories []Pillory `json:"pillories"`
	}
	if err := decodeJSON(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return envelope.Pillories, nil
}

// Pillory returns one lock's pillory session.
func (c *Client) Pillory(ctx context.Context, lockID ID) (*Pillory, error) {
	const op = "GET /pillory/{id}"
	body, err := c.get(ctx, "/pillory/"+url.PathEscape(lockID.String()))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Status  Status        `json:"status"`
		Actions []PilloryItem `json:"actions"`
	}
	if err := decodeJSON(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &Pillory{Items: envelope.Actions, LockID: lockID}, nil
}

// Scripts returns the scripts saved on a template.
func (c *Client) Scripts(ctx context.Context, templateID ID) ([]Script, error) {
	const op = "GET /scripts/{id}"
	body, err := c.get(ctx, "/scripts/"+url.PathEscape(templateID.String()))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Scripts []Script `json:"scripts"`
	}
	if err := decodeJSON(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if present, _, reason := statusOf(body); present && reason != "" {
		return nil, newAPIError(op, 200, nil, body)
	}
	return envelope.Scripts, nil
}

// Bots returns the keyholder bots for "card" or "timer" locks.
func (c *Client) Bots(ctx context.Context, kind string) ([]Bot, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "card" && kind != "timer" {
		return nil, fmt.Errorf("chastisafe: bot kind must be card or timer")
	}
	const op = "GET /bots"
	body, err := c.get(ctx, "/bots/"+kind)
	if err != nil {
		return nil, err
	}
	var out []Bot
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidResponse)
	}
	return out, nil
}
