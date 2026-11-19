package store

import (
	"context"
	"testing"
	"time"
)

func TestBDSMLRConfirmationLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	c := &BDSMLRConfirmation{
		Token:     "token-1",
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		UserID:    "user-1",
		Email:     "friend@example.com",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	}
	if err := st.CreateBDSMLRConfirmation(ctx, c); err != nil {
		t.Fatalf("CreateBDSMLRConfirmation: %v", err)
	}

	got, err := st.GetBDSMLRConfirmation(ctx, "token-1")
	if err != nil {
		t.Fatalf("GetBDSMLRConfirmation: %v", err)
	}
	if got == nil || got.Email != c.Email || got.UserID != c.UserID || got.ChannelID != c.ChannelID {
		t.Fatalf("confirmation = %+v", got)
	}

	if err := st.DeleteBDSMLRConfirmation(ctx, "token-1"); err != nil {
		t.Fatalf("DeleteBDSMLRConfirmation: %v", err)
	}
	got, err = st.GetBDSMLRConfirmation(ctx, "token-1")
	if err != nil {
		t.Fatalf("GetBDSMLRConfirmation after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("confirmation still present: %+v", got)
	}
}

func TestBDSMLRConfirmationReplaceAndPrune(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	for _, token := range []string{"old", "new"} {
		if err := st.CreateBDSMLRConfirmation(ctx, &BDSMLRConfirmation{
			Token:     token,
			GuildID:   "guild-1",
			UserID:    "user-1",
			Email:     "friend@example.com",
			ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		}); err != nil {
			t.Fatalf("CreateBDSMLRConfirmation(%s): %v", token, err)
		}
	}

	if err := st.DeleteBDSMLRConfirmationsForUser(ctx, "guild-1", "user-1"); err != nil {
		t.Fatalf("DeleteBDSMLRConfirmationsForUser: %v", err)
	}
	for _, token := range []string{"old", "new"} {
		got, err := st.GetBDSMLRConfirmation(ctx, token)
		if err != nil {
			t.Fatalf("GetBDSMLRConfirmation(%s): %v", token, err)
		}
		if got != nil {
			t.Fatalf("confirmation %s still present", token)
		}
	}

	if err := st.CreateBDSMLRConfirmation(ctx, &BDSMLRConfirmation{
		Token:     "expired",
		GuildID:   "guild-1",
		UserID:    "user-1",
		Email:     "friend@example.com",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateBDSMLRConfirmation(expired): %v", err)
	}
	if err := st.PruneBDSMLRConfirmations(ctx); err != nil {
		t.Fatalf("PruneBDSMLRConfirmations: %v", err)
	}
	got, err := st.GetBDSMLRConfirmation(ctx, "expired")
	if err != nil {
		t.Fatalf("GetBDSMLRConfirmation(expired): %v", err)
	}
	if got != nil {
		t.Fatalf("expired confirmation still present")
	}
}

func TestClaimBDSMLRConfirmation(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if err := st.CreateBDSMLRConfirmation(ctx, &BDSMLRConfirmation{
		Token:     "token-1",
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		UserID:    "user-1",
		Email:     "friend@example.com",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateBDSMLRConfirmation: %v", err)
	}

	claimed, err := st.ClaimBDSMLRConfirmation(ctx, "token-1")
	if err != nil {
		t.Fatalf("ClaimBDSMLRConfirmation: %v", err)
	}
	if !claimed {
		t.Fatal("first claim should succeed")
	}
	claimed, err = st.ClaimBDSMLRConfirmation(ctx, "token-1")
	if err != nil {
		t.Fatalf("ClaimBDSMLRConfirmation again: %v", err)
	}
	if claimed {
		t.Fatal("second claim should fail")
	}
}

func TestBDSMLRInviteRateLimit(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	last, err := st.LastBDSMLRInvite(ctx, "guild-1", "user-1")
	if err != nil {
		t.Fatalf("LastBDSMLRInvite: %v", err)
	}
	if !last.IsZero() {
		t.Fatalf("last = %v, want zero", last)
	}

	if err := st.RecordBDSMLRInvite(ctx, "guild-1", "user-1"); err != nil {
		t.Fatalf("RecordBDSMLRInvite: %v", err)
	}
	last, err = st.LastBDSMLRInvite(ctx, "guild-1", "user-1")
	if err != nil {
		t.Fatalf("LastBDSMLRInvite: %v", err)
	}
	if last.IsZero() {
		t.Fatal("expected a recorded invite")
	}
	if time.Since(last) > time.Minute {
		t.Fatalf("recorded invite is stale: %v", last)
	}

	other, err := st.LastBDSMLRInvite(ctx, "guild-1", "user-2")
	if err != nil {
		t.Fatalf("LastBDSMLRInvite(other): %v", err)
	}
	if !other.IsZero() {
		t.Fatalf("other user has an invite: %v", other)
	}
}

func TestBDSMLRMethodsNeedNoSettings(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// The invite tables must work on a fresh database, before /welcome setup.
	if err := st.RecordBDSMLRInvite(ctx, "guild-1", "user-1"); err != nil {
		t.Fatalf("RecordBDSMLRInvite: %v", err)
	}
	if err := st.DeleteBDSMLRConfirmation(ctx, "missing"); err != nil {
		t.Fatalf("DeleteBDSMLRConfirmation: %v", err)
	}
	if err := st.DeleteBDSMLRConfirmationsForUser(ctx, "guild-1", "user-1"); err != nil {
		t.Fatalf("DeleteBDSMLRConfirmationsForUser: %v", err)
	}
	if _, err := st.GetBDSMLRConfirmation(ctx, "missing"); err != nil {
		t.Fatalf("GetBDSMLRConfirmation: %v", err)
	}
}
