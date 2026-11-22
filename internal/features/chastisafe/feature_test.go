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

	cs "grimoire/internal/chastisafe"
	"grimoire/internal/cryptobox"
	"grimoire/internal/store"
)

type fakeClient struct {
	identity  *cs.Identity
	whoErr    error
	loaded    []cs.LoadedLock
	lockees   []cs.LoadedLock
	templates []cs.Template
	dataErr   error
}

func (f *fakeClient) Who(context.Context) (*cs.Identity, error) {
	if f.whoErr != nil {
		return nil, f.whoErr
	}
	return f.identity, nil
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
	f.newClient = func(token string) (lockClient, error) {
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
	if !strings.Contains(message, "/login") {
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

func TestLockeesTextSortsByUrgency(t *testing.T) {
	locks := []cs.LoadedLock{
		{
			LockName:       "Long timer",
			Lockee:         "bob",
			Type:           "timer",
			TimerRemaining: int((2 * time.Hour).Seconds()),
		},
		{
			LockName:          "Frozen timer",
			Lockee:            "alice",
			Type:              "timer",
			FrozenByKeyholder: true,
			TimerRemaining:    int((10 * time.Hour).Seconds()),
		},
		{
			LockName:      "Ready cards",
			Lockee:        "cara",
			Type:          "card",
			ReadyToUnlock: true,
			CardCount:     2,
		},
	}

	text := lockeesText(locks)
	if !strings.Contains(text, "**3 active lockees**") {
		t.Fatalf("text missing header: %s", text)
	}
	if !strings.Contains(text, "Frozen: 1") || !strings.Contains(text, "Ready: 1") {
		t.Fatalf("text missing summary: %s", text)
	}

	cara := strings.Index(text, "cara")
	alice := strings.Index(text, "alice")
	bob := strings.Index(text, "bob")
	if cara < 0 || alice < 0 || bob < 0 {
		t.Fatalf("text missing a lockee: %s", text)
	}
	if !(cara < alice && alice < bob) {
		t.Fatalf("lockees are not sorted by urgency: %s", text)
	}
}

func TestLockeesTextEmpty(t *testing.T) {
	if got := lockeesText(nil); got != "No active lockees." {
		t.Fatalf("text = %q", got)
	}
}

func TestLocksTextSortsByPopularity(t *testing.T) {
	templates := []cs.Template{
		{LockName: "Rare", Lockees: 1, LockType: "TIMEDLOCK", Status: "ENABLED"},
		{LockName: "Popular", Lockees: 9, LockType: "CARDLOCK", Status: "ENABLED"},
		{LockName: "Unused", Lockees: 0, LockType: "TIMEDLOCK", Status: "DISABLED"},
	}

	text := locksText(templates)
	popular := strings.Index(text, "Popular")
	rare := strings.Index(text, "Rare")
	unused := strings.Index(text, "Unused")
	if popular < 0 || rare < 0 || unused < 0 {
		t.Fatalf("text missing a template: %s", text)
	}
	if !(popular < rare && rare < unused) {
		t.Fatalf("templates are not sorted by popularity: %s", text)
	}
	if !strings.Contains(text, "9 lockees") {
		t.Fatalf("text missing popularity: %s", text)
	}
}

func TestPersonalStatusTextFiltersKeyholderLocks(t *testing.T) {
	locks := []cs.LoadedLock{
		{LockName: "Mine", LockeeID: "user-1", Type: "timer", TimerRemaining: 3600},
		{LockName: "Someone else", LockeeID: "user-2", Type: "timer", TimerRemaining: 3600},
	}

	text := personalStatusText(locks, "user-1")
	if !strings.Contains(text, "Mine") {
		t.Fatalf("text missing own lock: %s", text)
	}
	if strings.Contains(text, "Someone else") {
		t.Fatalf("text includes another member's lock: %s", text)
	}
}

func TestPersonalStatusTextWithoutIdentity(t *testing.T) {
	locks := []cs.LoadedLock{{LockName: "Lock", Type: "timer", TimerRemaining: 60}}
	text := personalStatusText(locks, "")
	if !strings.Contains(text, "Lock") {
		t.Fatalf("text = %q", text)
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
