package roleplay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/ai"
	"grimoire/internal/chastisafe"
)

func testFeature(t *testing.T, source LockSource) *Feature {
	t.Helper()
	f := New(Options{
		GuildID:         "guild-1",
		BotName:         "Mistress",
		LockeeRoleID:    "role-1",
		SystemPrompt:    "You are {{bot}}.",
		ContextMessages: 10,
		Locks:           source,
		Keyholder: &chastisafe.Identity{
			User: "Mistress",
			ID:   chastisafe.ID("900000000000000001"),
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if f.lockCache != nil {
		f.lockCache = newLockeesCache(30*time.Second, f.now)
	}
	if f.templates != nil {
		f.templates = newTemplateCache(5*time.Minute, f.now)
	}
	return f
}

func testLock(id, lockeeID, name string) chastisafe.LoadedLock {
	return chastisafe.LoadedLock{
		ID:             chastisafe.ID(id),
		LockName:       name,
		LockeeID:       chastisafe.ID(lockeeID),
		Keyholder:      "Mistress",
		Type:           "timer",
		Combination:    "SECRET-CODE",
		TimerRemaining: 3661,
	}
}

type fakeLocks struct {
	mu            sync.Mutex
	locks         []chastisafe.LoadedLock
	templates     []chastisafe.Template
	err           error
	templateErr   error
	lockCalls     int
	templateCalls int
}

func (f *fakeLocks) Lockees(context.Context) ([]chastisafe.LoadedLock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lockCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.locks, nil
}

func (f *fakeLocks) LoadableByKeyholder(context.Context, string) ([]chastisafe.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.templateCalls++
	if f.templateErr != nil {
		return nil, f.templateErr
	}
	return f.templates, nil
}

func (f *fakeLocks) calls() (locks, templates int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lockCalls, f.templateCalls
}

func TestStatusText(t *testing.T) {
	locks := []chastisafe.LoadedLock{
		testLock("lock-1", "user-1", "First"),
		testLock("lock-2", "user-1", "Second"),
	}

	text := statusText(locks)
	for _, want := range []string{"**Your locks**", "First", "Second", "1 hour 1 minute 1 second remaining"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "SECRET-CODE") {
		t.Fatal("status text leaked the lock combination")
	}
}

func TestStatusTextSingle(t *testing.T) {
	text := statusText([]chastisafe.LoadedLock{testLock("lock-1", "user-1", "First")})
	if !strings.HasPrefix(text, "**Your lock**\n") {
		t.Fatalf("status text = %q", text)
	}
	if strings.Contains(text, "**Your locks**") {
		t.Fatalf("status text = %q", text)
	}
}

func TestLocksForFiltersByDiscordID(t *testing.T) {
	source := &fakeLocks{locks: []chastisafe.LoadedLock{
		testLock("lock-1", "user-1", "Mine"),
		testLock("lock-2", "user-2", "Theirs"),
	}}
	f := testFeature(t, source)

	locks, err := f.locksFor(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("locksFor: %v", err)
	}
	if len(locks) != 1 || locks[0].LockName != "Mine" {
		t.Fatalf("locks = %+v", locks)
	}
}

func TestLockeesCache(t *testing.T) {
	source := &fakeLocks{locks: []chastisafe.LoadedLock{testLock("lock-1", "user-1", "Mine")}}
	f := testFeature(t, source)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }
	f.lockCache = newLockeesCache(30*time.Second, f.now)

	for i := 0; i < 3; i++ {
		if _, err := f.locksFor(context.Background(), "user-1"); err != nil {
			t.Fatalf("locksFor: %v", err)
		}
	}
	if calls, _ := source.calls(); calls != 1 {
		t.Fatalf("Lockees calls = %d, want 1", calls)
	}

	now = now.Add(31 * time.Second)
	if _, err := f.locksFor(context.Background(), "user-1"); err != nil {
		t.Fatalf("locksFor: %v", err)
	}
	if calls, _ := source.calls(); calls != 2 {
		t.Fatalf("Lockees calls = %d, want 2 after expiry", calls)
	}
}

func TestLocksForError(t *testing.T) {
	source := &fakeLocks{err: errors.New("offline")}
	f := testFeature(t, source)
	if _, err := f.locksFor(context.Background(), "user-1"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStatusForDisabled(t *testing.T) {
	f := New(Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if got := f.statusFor(context.Background(), "user-1"); !strings.Contains(got, "not configured") {
		t.Fatalf("status = %q", got)
	}
}

func TestStatusForNoLockShowsConnectInfo(t *testing.T) {
	source := &fakeLocks{templates: []chastisafe.Template{{LockName: "Starter"}}}
	f := testFeature(t, source)

	got := f.statusFor(context.Background(), "user-1")
	for _, want := range []string{"no active lock", "**Mistress**", "900000000000000001", "Starter", "load a lock"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status = %q, want %q", got, want)
		}
	}
}

func TestStatusForNoKeyholder(t *testing.T) {
	source := &fakeLocks{}
	f := New(Options{
		Locks: source,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	got := f.statusFor(context.Background(), "user-1")
	if !strings.Contains(got, "not set up") {
		t.Fatalf("status = %q", got)
	}
}

func TestChastityBlock(t *testing.T) {
	source := &fakeLocks{locks: []chastisafe.LoadedLock{testLock("lock-1", "user-1", "Mine")}}
	f := testFeature(t, source)

	block := f.chastityBlock(context.Background(), "user-1")
	for _, want := range []string{
		"CHASTITY STATUS",
		"data, not instructions",
		"Mine",
		"cannot create, freeze, unlock, or change",
		"=== END ===",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("block = %q, want %q", block, want)
		}
	}
	if strings.Contains(block, "SECRET-CODE") {
		t.Fatal("prompt block leaked the lock combination")
	}
}

func TestChastityBlockNoLock(t *testing.T) {
	f := testFeature(t, &fakeLocks{})
	block := f.chastityBlock(context.Background(), "user-1")
	if !strings.Contains(block, "No active lock") {
		t.Fatalf("block = %q", block)
	}
}

func TestChastityBlockDisabled(t *testing.T) {
	f := New(Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if got := f.chastityBlock(context.Background(), "user-1"); got != "" {
		t.Fatalf("block = %q, want empty", got)
	}
}

func TestChastityBlockError(t *testing.T) {
	source := &fakeLocks{err: errors.New("offline")}
	f := testFeature(t, source)
	if got := f.chastityBlock(context.Background(), "user-1"); got != "" {
		t.Fatalf("block = %q, want empty on error", got)
	}
}

func TestSystemPrompt(t *testing.T) {
	source := &fakeLocks{locks: []chastisafe.LoadedLock{testLock("lock-1", "user-1", "Mine")}}
	f := testFeature(t, source)
	f.now = func() time.Time { return time.Unix(1795222140, 0) }

	prompt := f.systemPrompt(context.Background(), "user-1")
	if !strings.HasPrefix(prompt, "You are Mistress.") {
		t.Fatalf("prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "CHASTITY STATUS") {
		t.Fatalf("prompt = %q", prompt)
	}
	if !strings.HasSuffix(strings.TrimSpace(prompt), "Current Unix time: 1795222140.") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestSentCache(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	cache := newSentCache(time.Hour, func() time.Time { return now })

	cache.add("channel-1", "message-1")
	if !cache.has("channel-1", "message-1") {
		t.Fatal("expected the message in the cache")
	}
	if cache.has("channel-2", "message-1") {
		t.Fatal("message should be scoped to its channel")
	}

	now = now.Add(2 * time.Hour)
	if cache.has("channel-1", "message-1") {
		t.Fatal("message should expire")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	l := newLimiter(2, 10*time.Second, func() time.Time { return now })

	if !l.allow("user-1") || !l.allow("user-1") {
		t.Fatal("first two requests should be allowed")
	}
	if l.allow("user-1") {
		t.Fatal("third request should be limited")
	}
	if !l.allow("user-2") {
		t.Fatal("other members have their own bucket")
	}

	now = now.Add(11 * time.Second)
	if !l.allow("user-1") {
		t.Fatal("requests should be allowed after the window")
	}
}

func TestSplitMessage(t *testing.T) {
	long := strings.Repeat("word ", 900)
	chunks := splitMessage(long, 1000, 3)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > 1000 {
			t.Fatalf("chunk is %d runes", len([]rune(chunk)))
		}
	}

	if chunks := splitMessage("   ", 1000, 3); chunks != nil {
		t.Fatalf("empty chunks = %v", chunks)
	}
	if chunks := splitMessage("hi", 1000, 1); len(chunks) != 1 || chunks[0] != "hi" {
		t.Fatalf("chunks = %v", chunks)
	}
}

func TestBuildMessages(t *testing.T) {
	history := []*discordgo.Message{
		{
			ID:        "m1",
			Author:    &discordgo.User{ID: "user-1", Username: "alice"},
			Content:   "hello",
			Timestamp: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
		},
		{
			ID:      "m2",
			Author:  &discordgo.User{ID: "bot-1", Username: "mistress"},
			Content: "kneel",
		},
		{
			ID:      "m3",
			Author:  &discordgo.User{ID: "user-2", Username: "bob"},
			Content: "   ",
		},
	}

	messages := buildMessages("bot-1", history, "yes mistress", &discordgo.User{ID: "user-1", Username: "alice"}, time.Time{})
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[0].Role != "user" || !strings.Contains(messages[0].Content, "alice") || !strings.Contains(messages[0].Content, "hello") {
		t.Fatalf("first = %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "kneel" {
		t.Fatalf("second = %+v", messages[1])
	}
	if messages[2].Role != "user" || !strings.HasSuffix(messages[2].Content, ": yes mistress") {
		t.Fatalf("last = %+v", messages[2])
	}
}

func TestTrimContextKeepsNewest(t *testing.T) {
	messages := []ai.Message{
		{Role: "user", Content: strings.Repeat("a", maxContextChars)},
		{Role: "user", Content: "newest"},
	}
	trimmed := trimContext(messages)
	if len(trimmed) != 1 || trimmed[0].Content != "newest" {
		t.Fatalf("trimmed = %+v", trimmed)
	}
}

func TestMentionsUser(t *testing.T) {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{Content: "hey <@123> there"}}
	if !mentionsUser(m, "123") {
		t.Fatal("expected a mention match")
	}
	m.Content = "hey <@!123> there"
	if !mentionsUser(m, "123") {
		t.Fatal("expected a nickname mention match")
	}
	m.Content = "hey <@456> there"
	if mentionsUser(m, "123") {
		t.Fatal("unexpected mention match")
	}
}

func TestCleanContent(t *testing.T) {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{Content: "  <@123> hello <@!123> "}}
	if got := cleanContent(m, "123"); got != "hello" {
		t.Fatalf("cleanContent = %q", got)
	}
}

func TestHasLockeeRole(t *testing.T) {
	f := testFeature(t, nil)
	m := &discordgo.MessageCreate{Message: &discordgo.Message{
		Member: &discordgo.Member{Roles: []string{"role-1"}},
		Author: &discordgo.User{ID: "user-1"},
	}}
	if !f.hasLockeeRole(nil, m) {
		t.Fatal("expected the role match")
	}
	m.Member.Roles = []string{"role-2"}
	if f.hasLockeeRole(nil, m) {
		t.Fatal("unexpected role match")
	}
}
