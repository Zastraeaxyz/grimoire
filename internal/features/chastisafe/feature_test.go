package chastisafe

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	cs "github.com/zastraeaxyz/chastisafe-go"
	"grimoire/internal/cryptobox"
	"grimoire/internal/store"
)

type fakeClient struct {
	*cs.Client

	identity   *cs.Identity
	whoErr     error
	profile    *cs.Profile
	profileErr error
	loaded     []cs.LoadedLock
	lockees    []cs.LoadedLock
	templates  []cs.Template
	dataErr    error
}

func (f *fakeClient) Who(context.Context) (*cs.Identity, error) {
	if f.whoErr != nil {
		return nil, f.whoErr
	}
	return f.identity, nil
}

func (f *fakeClient) ProfileFor(context.Context, string) (*cs.Profile, error) {
	if f.profileErr != nil {
		return nil, f.profileErr
	}
	return f.profile, nil
}

func (f *fakeClient) Loaded(context.Context) ([]cs.LoadedLock, error) {
	if f.dataErr != nil {
		return nil, f.dataErr
	}
	return f.loaded, nil
}

func (f *fakeClient) Lockees(context.Context) ([]cs.LoadedLock, error) {
	if f.dataErr != nil {
		return nil, f.dataErr
	}
	return f.lockees, nil
}

func (f *fakeClient) Templates(context.Context) ([]cs.Template, error) {
	if f.dataErr != nil {
		return nil, f.dataErr
	}
	return f.templates, nil
}

func testBox(t *testing.T) *cryptobox.Box {
	t.Helper()
	key := make([]byte, cryptobox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := cryptobox.New(key)
	if err != nil {
		t.Fatalf("cryptobox.New: %v", err)
	}
	return box
}

func newTestFeature(t *testing.T, client *fakeClient) (*Feature, *store.Store) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "grimoire.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})

	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	f := New(Options{
		Store:  st,
		Box:    testBox(t),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return now },
	})
	f.newClient = func(token string) (cs.API, error) {
		if strings.TrimSpace(token) == "" {
			return nil, cs.ErrUnauthorized
		}
		return client, nil
	}
	return f, st
}

func TestAuthenticateStoresEncryptedCode(t *testing.T) {
	ctx := context.Background()
	client := &fakeClient{identity: &cs.Identity{User: "lockee#0", ID: "user-1"}}
	f, st := newTestFeature(t, client)

	identity, err := f.authenticate(ctx, "user-1", "  secret-code  ")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if identity.User != "lockee#0" {
		t.Fatalf("identity = %+v", identity)
	}

	cred, err := st.GetChastiSafeCredential(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetChastiSafeCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("credential is nil")
	}
	if strings.Contains(string(cred.Secret), "secret-code") {
		t.Fatal("stored secret contains plaintext")
	}
	plaintext, err := f.box.Open(cred.Secret)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plaintext) != "secret-code" {
		t.Fatalf("decrypted code = %q", plaintext)
	}
	if cred.ChastiSafeID != "user-1" || cred.ChastiSafeUser != "lockee#0" {
		t.Fatalf("credential = %+v", cred)
	}
}

func TestAuthenticateRejectsAnotherAccount(t *testing.T) {
	ctx := context.Background()
	client := &fakeClient{identity: &cs.Identity{User: "someone#0", ID: "user-2"}}
	f, st := newTestFeature(t, client)

	if _, err := f.authenticate(ctx, "user-1", "secret-code"); !errors.Is(err, errNotOwner) {
		t.Fatalf("authenticate error = %v, want errNotOwner", err)
	}
	cred, err := st.GetChastiSafeCredential(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetChastiSafeCredential: %v", err)
	}
	if cred != nil {
		t.Fatalf("credential was stored: %+v", cred)
	}
}

func TestAuthenticateRejectsUnauthorizedCode(t *testing.T) {
	ctx := context.Background()
	client := &fakeClient{whoErr: cs.ErrUnauthorized}
	f, _ := newTestFeature(t, client)

	if _, err := f.authenticate(ctx, "user-1", "wrong"); !errors.Is(err, cs.ErrUnauthorized) {
		t.Fatalf("authenticate error = %v, want ErrUnauthorized", err)
	}
}

func TestAPIFailureDeletesRejectedCode(t *testing.T) {
	ctx := context.Background()
	client := &fakeClient{identity: &cs.Identity{User: "lockee#0", ID: "user-1"}}
	f, st := newTestFeature(t, client)

	if _, err := f.authenticate(ctx, "user-1", "secret-code"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	message := f.apiFailure(ctx, "user-1", cs.ErrUnauthorized)
	if !strings.Contains(message, "/cs login") {
		t.Fatalf("message = %q", message)
	}
	cred, err := st.GetChastiSafeCredential(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetChastiSafeCredential: %v", err)
	}
	if cred != nil {
		t.Fatalf("rejected credential was kept: %+v", cred)
	}
}

func TestServiceForCachesPerMember(t *testing.T) {
	ctx := context.Background()
	client := &fakeClient{identity: &cs.Identity{User: "lockee#0", ID: "user-1"}}
	f, _ := newTestFeature(t, client)
	if _, err := f.authenticate(ctx, "user-1", "secret-code"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	first, _, err := f.serviceFor(ctx, "user-1")
	if err != nil {
		t.Fatalf("serviceFor: %v", err)
	}
	second, _, err := f.serviceFor(ctx, "user-1")
	if err != nil {
		t.Fatalf("serviceFor again: %v", err)
	}
	if first != second {
		t.Fatal("service was not cached")
	}

	f.invalidateService("user-1")
	third, _, err := f.serviceFor(ctx, "user-1")
	if err != nil {
		t.Fatalf("serviceFor after invalidate: %v", err)
	}
	if third == first {
		t.Fatal("service cache was not invalidated")
	}
}

func TestLoginLimiter(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(2, time.Minute, func() time.Time { return now })

	if !limiter.allow("user-1") || !limiter.allow("user-1") {
		t.Fatal("limiter rejected an attempt below the limit")
	}
	if limiter.allow("user-1") {
		t.Fatal("limiter accepted an attempt above the limit")
	}
	if !limiter.allow("user-2") {
		t.Fatal("limiter leaked across members")
	}

	now = now.Add(2 * time.Minute)
	if !limiter.allow("user-1") {
		t.Fatal("limiter did not expire old attempts")
	}
}

func TestSplitText(t *testing.T) {
	chunks := splitText("one\ntwo\nthree", 7)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if chunks[0] != "one\ntwo" || chunks[1] != "three" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if got := splitText("", 10); got != nil {
		t.Fatalf("empty split = %#v", got)
	}

	long := strings.Repeat("x", 25)
	chunks = splitText(long, 10)
	if len(chunks) != 3 {
		t.Fatalf("long chunks = %d, want 3", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > 10 {
			t.Fatalf("chunk is too long: %q", chunk)
		}
	}
}

func TestModalValue(t *testing.T) {
	components := []discordgo.MessageComponent{
		&discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			&discordgo.TextInput{CustomID: "code", Value: "abc"},
		}},
	}
	if got := modalValue(components, "code"); got != "abc" {
		t.Fatalf("modalValue = %q", got)
	}
	if got := modalValue(components, "missing"); got != "" {
		t.Fatalf("modalValue missing = %q", got)
	}
}
