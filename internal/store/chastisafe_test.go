package store

import (
	"bytes"
	"context"
	"testing"
)

func TestChastiSafeCredentialRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if cred, err := st.GetChastiSafeCredential(ctx, "user-1"); err != nil {
		t.Fatalf("GetChastiSafeCredential: %v", err)
	} else if cred != nil {
		t.Fatalf("credential = %+v, want nil", cred)
	}

	secret := []byte{0x01, 0x02, 0x03}
	if err := st.SaveChastiSafeCredential(ctx, &ChastiSafeCredential{
		DiscordUserID:  "user-1",
		ChastiSafeID:   "cs-1",
		ChastiSafeUser: "lockee#0",
		Secret:         secret,
	}); err != nil {
		t.Fatalf("SaveChastiSafeCredential: %v", err)
	}

	cred, err := st.GetChastiSafeCredential(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetChastiSafeCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("credential is nil")
	}
	if cred.ChastiSafeID != "cs-1" || cred.ChastiSafeUser != "lockee#0" {
		t.Fatalf("credential = %+v", cred)
	}
	if !bytes.Equal(cred.Secret, secret) {
		t.Fatalf("secret = %v, want %v", cred.Secret, secret)
	}
	if cred.CreatedAt.IsZero() || cred.UpdatedAt.IsZero() {
		t.Fatalf("timestamps are zero: %+v", cred)
	}

	// Saving again replaces the code and keeps one row.
	updated := []byte{0x04, 0x05}
	if err := st.SaveChastiSafeCredential(ctx, &ChastiSafeCredential{
		DiscordUserID:  "user-1",
		ChastiSafeID:   "cs-2",
		ChastiSafeUser: "keyholder#0",
		Secret:         updated,
	}); err != nil {
		t.Fatalf("SaveChastiSafeCredential(update): %v", err)
	}
	cred, err = st.GetChastiSafeCredential(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetChastiSafeCredential(update): %v", err)
	}
	if cred.ChastiSafeID != "cs-2" || !bytes.Equal(cred.Secret, updated) {
		t.Fatalf("credential after update = %+v", cred)
	}

	if err := st.DeleteChastiSafeCredential(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteChastiSafeCredential: %v", err)
	}
	cred, err = st.GetChastiSafeCredential(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetChastiSafeCredential after delete: %v", err)
	}
	if cred != nil {
		t.Fatalf("credential after delete = %+v, want nil", cred)
	}

	// Deleting again is harmless.
	if err := st.DeleteChastiSafeCredential(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteChastiSafeCredential again: %v", err)
	}
}
