package bdsmlr

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "grimoire.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return st
}

func TestInviteAllowed(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	f := &Feature{guildID: "guild-1", store: st}

	allowed, next, err := f.inviteAllowed(ctx, "user-1")
	if err != nil {
		t.Fatalf("inviteAllowed: %v", err)
	}
	if !allowed || !next.IsZero() {
		t.Fatalf("fresh user: allowed=%v next=%v", allowed, next)
	}

	if err := st.RecordBDSMLRInvite(ctx, "guild-1", "user-1"); err != nil {
		t.Fatalf("RecordBDSMLRInvite: %v", err)
	}
	allowed, next, err = f.inviteAllowed(ctx, "user-1")
	if err != nil {
		t.Fatalf("inviteAllowed after send: %v", err)
	}
	if allowed {
		t.Fatal("user should be rate limited")
	}
	if window := time.Until(next); window < 23*time.Hour || window > 25*time.Hour {
		t.Fatalf("next window = %v, want about 24 hours", window)
	}

	allowed, _, err = f.inviteAllowed(ctx, "user-2")
	if err != nil {
		t.Fatalf("inviteAllowed(other): %v", err)
	}
	if !allowed {
		t.Fatal("another user should not be rate limited")
	}
}

func TestConfirmationMessage(t *testing.T) {
	member := confirmationMessage("friend@example.com", false)
	if !strings.Contains(member, "friend@example.com") {
		t.Fatal("member message does not show the email")
	}
	if !strings.Contains(member, "24 hours") {
		t.Fatal("member message does not mention the daily limit")
	}
	if !strings.Contains(member, "invitation link") {
		t.Fatal("member message does not explain what happens next")
	}

	admin := confirmationMessage("friend@example.com", true)
	if strings.Contains(admin, "24 hours") {
		t.Fatal("admin message should not mention the member limit")
	}
}

func TestConfirmationComponents(t *testing.T) {
	rows := confirmationComponents("abc123")
	if len(rows) != 1 {
		t.Fatalf("components = %d, want one row", len(rows))
	}
	row, ok := rows[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("row type = %T", rows[0])
	}
	if len(row.Components) != 2 {
		t.Fatalf("buttons = %d, want two", len(row.Components))
	}
}

func TestParseComponentID(t *testing.T) {
	action, token, ok := parseComponentID("bdsmlr:send:abc123")
	if !ok || action != "send" || token != "abc123" {
		t.Fatalf("parseComponentID = %q %q %v", action, token, ok)
	}
	action, token, ok = parseComponentID("bdsmlr:cancel:abc123")
	if !ok || action != "cancel" || token != "abc123" {
		t.Fatalf("parseComponentID = %q %q %v", action, token, ok)
	}
	for _, id := range []string{"", "bdsmlr", "bdsmlr:send", "other:send:abc", "bdsmlr:send:abc:extra"} {
		if _, _, ok := parseComponentID(id); ok {
			t.Errorf("parseComponentID(%q) unexpectedly succeeded", id)
		}
	}
}

func TestNewConfirmationToken(t *testing.T) {
	token, err := newConfirmationToken()
	if err != nil {
		t.Fatalf("newConfirmationToken: %v", err)
	}
	if len(token) != 32 {
		t.Fatalf("token length = %d, want 32", len(token))
	}
	other, err := newConfirmationToken()
	if err != nil {
		t.Fatalf("newConfirmationToken: %v", err)
	}
	if token == other {
		t.Fatal("tokens should be unique")
	}
}

func TestPublicInviteMessage(t *testing.T) {
	message := publicInviteMessage("1234")
	if message != "An invite has been sent to your email <@1234>" {
		t.Fatalf("message = %q", message)
	}
}

func TestRateLimitMessage(t *testing.T) {
	next := time.Unix(1795649467, 0).UTC()
	message := rateLimitMessage(next)
	if !strings.Contains(message, "<t:1795649467:R>") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "24 hours") {
		t.Fatalf("message = %q", message)
	}
}
