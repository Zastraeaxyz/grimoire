package roleplay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"grimoire/internal/ai"
	"grimoire/internal/chastisafe"
)

// chastityGuidance is appended to the persona prompt only when ChastiSafe is
// configured. It keeps the lock feature in the model's toolbelt instead of in
// every reply.
const chastityGuidance = `## Tools (added by the bot)
You have a chastity_status tool. It reads the real ChastiSafe lock of the person you are talking to. Use it only when chastity, locks, keys, or their account are already part of the conversation. Do not announce the check, and do not bring up cages or keys unprompted. You can read status only. You cannot create, freeze, unlock, or change a lock.`

// chastityTools is the tool schema the model sees.
func chastityTools() []ai.Tool {
	return []ai.Tool{{
		Type: "function",
		Function: ai.ToolFunction{
			Name:        "chastity_status",
			Description: "Read the caller's ChastiSafe lock status. Use it only when the conversation already involves chastity, locks, keys, or their account.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
	}}
}

// lockeesCache holds the keyholder's lockee list for a short time so a burst
// of messages does not hit ChastiSafe once per message.
type lockeesCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	expires time.Time
	locks   []chastisafe.LoadedLock
	valid   bool
}

func newLockeesCache(ttl time.Duration, now func() time.Time) *lockeesCache {
	return &lockeesCache{ttl: ttl, now: now}
}

func (c *lockeesCache) all(ctx context.Context, fetch func(context.Context) ([]chastisafe.LoadedLock, error)) ([]chastisafe.LoadedLock, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid && c.now().Before(c.expires) {
		return c.locks, nil
	}

	locks, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	c.locks = locks
	c.expires = c.now().Add(c.ttl)
	c.valid = true
	return locks, nil
}

// templateCache keeps the keyholder's offered template names around. Template
// edits are rare, so a longer cache is fine.
type templateCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	expires time.Time
	names   []string
	valid   bool
}

func newTemplateCache(ttl time.Duration, now func() time.Time) *templateCache {
	return &templateCache{ttl: ttl, now: now}
}

func (c *templateCache) load(ctx context.Context, source LockSource, keyholderID string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid && c.now().Before(c.expires) {
		return c.names
	}

	templates, err := source.LoadableByKeyholder(ctx, keyholderID)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(templates))
	for _, template := range templates {
		name := strings.TrimSpace(template.LockName)
		if name == "" || len(names) >= 10 {
			continue
		}
		names = append(names, name)
	}
	c.names = names
	c.expires = c.now().Add(c.ttl)
	c.valid = true
	return names
}

// locksFor returns only the locks where the Discord member is the lockee. The
// keyholder list can hold every sub, so filtering happens here, not at the
// API.
func (f *Feature) locksFor(ctx context.Context, userID string) ([]chastisafe.LoadedLock, error) {
	if f.locks == nil {
		return nil, fmt.Errorf("chastity is disabled")
	}
	all, err := f.lockCache.all(ctx, f.locks.Lockees)
	if err != nil {
		return nil, err
	}

	locks := make([]chastisafe.LoadedLock, 0, len(all))
	for _, lock := range all {
		if lock.LockeeID.String() != userID {
			continue
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

// statusText renders lock summaries for a member. It never includes a
// combination, and LockSummary does not read one.
func statusText(locks []chastisafe.LoadedLock) string {
	if len(locks) == 0 {
		return ""
	}

	var b strings.Builder
	if len(locks) == 1 {
		b.WriteString("**Your lock**\n")
	} else {
		b.WriteString("**Your locks**\n")
	}
	for i := range locks {
		b.WriteString("- ")
		b.WriteString(chastisafe.LockSummary(&locks[i]))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// connectText explains how a lockee adds the persona as their keyholder in the
// ChastiSafe app. There is no API for a keyholder to attach a lockee; the
// lockee starts that relationship from their side.
func (f *Feature) connectText(ctx context.Context) string {
	if f.keyholder == nil || f.keyholder.ID == "" {
		return "ChastiSafe is not set up yet."
	}

	var b strings.Builder
	b.WriteString("You have no active lock where I am the keyholder.\n\n")
	b.WriteString("Add me as your keyholder in ChastiSafe:\n")
	name := strings.TrimSpace(f.keyholder.User)
	if name != "" {
		fmt.Fprintf(&b, "**%s**", name)
	}
	fmt.Fprintf(&b, " (ID `%s`)\n", f.keyholder.ID)

	if f.locks != nil {
		if names := f.templates.load(ctx, f.locks, f.keyholder.ID.String()); len(names) > 0 {
			b.WriteString("\nTemplates I offer: ")
			b.WriteString(strings.Join(names, ", "))
			b.WriteString("\n")
		}
	}

	b.WriteString("\nThen load a lock. I will see it here.")
	return b.String()
}

// runTool executes a model tool call and returns a short result for the
// model. Results never include lock combinations.
func (f *Feature) runTool(ctx context.Context, userID, name string) string {
	switch name {
	case "chastity_status":
		return f.toolStatus(ctx, userID)
	default:
		return "That tool is not available."
	}
}

func (f *Feature) toolStatus(ctx context.Context, userID string) string {
	if f.locks == nil {
		return "ChastiSafe is not connected."
	}

	locks, err := f.locksFor(ctx, userID)
	if err != nil {
		f.log.Warn("loading chastisafe status for a tool call", "err", err, "user_id", userID)
		return "Lock status is unavailable right now."
	}
	if len(locks) == 0 {
		return "No active lock where I am the keyholder."
	}
	return "Current lock status:\n" + statusText(locks)
}
