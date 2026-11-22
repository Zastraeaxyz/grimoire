package chastisafe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeAPI struct {
	*Client

	identity  *Identity
	templates []Template
	loaded    []LoadedLock
	lockees   []LoadedLock

	templateCalls int
	createPayload map[string]any
	updatePayload map[string]any
	deletedID     ID
	cardChange    *CardRange
	background    string
	simulation    *Simulation

	loadCalls   int
	addTime     time.Duration
	remaining   time.Duration
	frozen      *bool
	hidden      *bool
	purpose     *Purpose
	cardChanges []CardChange
	settings    *KeyholderSettings
	unlockedID  ID
	resetID     ID
	abandonedID ID
	splitCount  int
}

func (f *fakeAPI) Who(context.Context) (*Identity, error) {
	if f.identity == nil {
		return &Identity{User: "me#0", ID: "me"}, nil
	}
	return f.identity, nil
}

func (f *fakeAPI) Templates(context.Context) ([]Template, error) {
	f.templateCalls++
	return append([]Template(nil), f.templates...), nil
}

func (f *fakeAPI) Template(_ context.Context, id ID) (*Template, error) {
	for i := range f.templates {
		if f.templates[i].ID == id {
			copy := f.templates[i]
			return &copy, nil
		}
	}
	return nil, ErrLockNotFound
}

func (f *fakeAPI) TemplateForEdit(ctx context.Context, id ID) (*Template, error) {
	return f.Template(ctx, id)
}

func (f *fakeAPI) Loaded(context.Context) ([]LoadedLock, error) {
	return append([]LoadedLock(nil), f.loaded...), nil
}

func (f *fakeAPI) Lockees(context.Context) ([]LoadedLock, error) {
	return append([]LoadedLock(nil), f.lockees...), nil
}

func (f *fakeAPI) CreateTemplate(_ context.Context, spec map[string]any) error {
	f.createPayload = spec
	name, _ := spec["lockname"].(string)
	lockType, _ := spec["locktype"].(string)
	f.templates = append(f.templates, Template{
		ID:       "new-id",
		LockName: name,
		LockType: lockType,
	})
	return nil
}

func (f *fakeAPI) UpdateTemplate(_ context.Context, spec map[string]any) error {
	f.updatePayload = spec
	return nil
}

func (f *fakeAPI) DeleteTemplate(_ context.Context, id ID) error {
	f.deletedID = id
	return nil
}

func (f *fakeAPI) UpdateCardRange(_ context.Context, _ ID, change CardRange) error {
	f.cardChange = &change
	return nil
}

func (f *fakeAPI) SetBackground(_ context.Context, _ ID, background string) error {
	f.background = background
	return nil
}

func (f *fakeAPI) Simulate(context.Context, ID) (*Simulation, error) {
	return f.simulation, nil
}

func (f *fakeAPI) Load(context.Context, LoadRequest) ([]LoadedLock, error) {
	f.loadCalls++
	return f.loaded, nil
}

func (f *fakeAPI) AddTime(_ context.Context, _ ID, delta time.Duration) (*LoadedLock, error) {
	f.addTime = delta
	return f.firstLoaded(), nil
}

func (f *fakeAPI) SetRemainingTime(_ context.Context, _ ID, remaining time.Duration) (*LoadedLock, error) {
	f.remaining = remaining
	return f.firstLoaded(), nil
}

func (f *fakeAPI) Freeze(_ context.Context, _ ID, frozen bool) (*LoadedLock, error) {
	f.frozen = &frozen
	return f.firstLoaded(), nil
}

func (f *fakeAPI) Hide(_ context.Context, _ ID, hidden bool) (*LoadedLock, error) {
	f.hidden = &hidden
	return f.firstLoaded(), nil
}

func (f *fakeAPI) SetPurpose(_ context.Context, _ ID, purpose Purpose) (*LoadedLock, error) {
	f.purpose = &purpose
	return f.firstLoaded(), nil
}

func (f *fakeAPI) UpdateSettings(_ context.Context, _ ID, settings KeyholderSettings) (*LoadedLock, error) {
	f.settings = &settings
	return f.firstLoaded(), nil
}

func (f *fakeAPI) UpdateCards(_ context.Context, _ ID, changes []CardChange, _ UpdateOptions) (*LoadedLock, error) {
	f.cardChanges = changes
	return f.firstLoaded(), nil
}

func (f *fakeAPI) Unlock(_ context.Context, id ID) (*OpenResult, error) {
	f.unlockedID = id
	return &OpenResult{Status: StatusOK, Code: "never-show-this"}, nil
}

func (f *fakeAPI) Reset(_ context.Context, id ID) (*OpenResult, error) {
	f.resetID = id
	return &OpenResult{Status: StatusOK, Code: "never-show-this"}, nil
}

func (f *fakeAPI) Abandon(_ context.Context, id ID) error {
	f.abandonedID = id
	return nil
}

func (f *fakeAPI) Split(_ context.Context, _ ID, count int) ([]LoadedLock, error) {
	f.splitCount = count
	return f.loaded, nil
}

func (f *fakeAPI) firstLoaded() *LoadedLock {
	if len(f.loaded) == 0 {
		return nil
	}
	copy := f.loaded[0]
	return &copy
}

func newTestService(api *fakeAPI) *Service {
	service := NewService(api)
	service.SetTTL(20 * time.Second)
	return service
}

func TestTemplateDraftPayload(t *testing.T) {
	draft := TemplateDraft{
		Name:            "Timer",
		Type:            TemplateTypeTimer,
		Description:     "desc",
		TimerMin:        2 * time.Hour,
		TimerMax:        3 * time.Hour,
		Regularity:      time.Hour,
		AllowedPurposes: PurposeMaskChastity,
	}
	payload, err := draft.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if payload["lockname"] != "Timer" || payload["locktype"] != "TIMEDLOCK" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["timerMin"] != 7200 || payload["timerMax"] != 10800 {
		t.Fatalf("timer bounds = %#v", payload)
	}
	if payload["regularity"] != 60 {
		t.Fatalf("regularity = %#v", payload["regularity"])
	}
	if payload["allowedpurposes"] != int(PurposeMaskChastity) {
		t.Fatalf("allowedpurposes = %#v", payload["allowedpurposes"])
	}
	if payload["allowupdates"] != true {
		t.Fatalf("allowupdates = %#v", payload["allowupdates"])
	}

	cardDraft := TemplateDraft{
		Name: "Cards",
		Type: TemplateTypeCard,
		Cards: []TemplateCard{
			{Name: CardGreen, Min: 1, Max: 2},
			{Name: CardRed, Min: 0, Max: 1},
		},
		Regularity: time.Hour,
	}
	payload, err = cardDraft.Payload()
	if err != nil {
		t.Fatalf("card Payload: %v", err)
	}
	if payload["locktype"] != "CARDLOCK" || payload["cumulative"] != "yes" {
		t.Fatalf("card payload = %#v", payload)
	}
	cards, ok := payload["cards"].([]map[string]any)
	if !ok || len(cards) != 2 {
		t.Fatalf("cards = %#v", payload["cards"])
	}
	if payload["requiredgreens"] != 1001 {
		t.Fatalf("requiredgreens = %#v", payload["requiredgreens"])
	}
}

func TestTemplateDraftValidation(t *testing.T) {
	cases := []TemplateDraft{
		{},
		{Name: "x", Type: "other"},
		{Name: "x", Type: TemplateTypeTimer, TimerMin: 2 * time.Hour, TimerMax: time.Hour},
		{Name: "x", Type: TemplateTypeTimer, Cards: []TemplateCard{{Name: CardGreen}}},
		{Name: "x", Type: TemplateTypeCard},
		{Name: "x", Type: TemplateTypeCard, Cards: []TemplateCard{{Name: CardGreen, Min: 2, Max: 1}}},
		{Name: "x", Type: TemplateTypeTimer, Regularity: 90 * time.Second},
	}
	for i, draft := range cases {
		if _, err := draft.Payload(); err == nil {
			t.Errorf("case %d unexpectedly succeeded", i)
		}
	}
}

func TestTemplatePatchPayload(t *testing.T) {
	name := "Renamed"
	hidden := true
	minutes := 90 * time.Minute
	payload, err := TemplatePatch{
		Name:       &name,
		Hidden:     &hidden,
		Regularity: &minutes,
	}.Payload("id-1")
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if payload["templateId"] != ID("id-1") || payload["lockname"] != "Renamed" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["hidden"] != "yes" || payload["regularity"] != 90 {
		t.Fatalf("payload = %#v", payload)
	}
	if _, exists := payload["description"]; exists {
		t.Fatal("unset field was sent")
	}

	if _, err := (TemplatePatch{}).Payload("id-1"); err == nil {
		t.Fatal("empty patch succeeded")
	}
}

func TestResolveTemplate(t *testing.T) {
	api := &fakeAPI{templates: []Template{
		{ID: "1", LockName: "Steel"},
		{ID: "2", LockName: "Steel long"},
		{ID: "3", LockName: "Leather"},
	}}
	service := newTestService(api)

	tests := map[string]string{
		"1":        "1",
		"steel":    "1",
		"STEEL":    "1",
		"leath":    "3",
		"Steel lo": "2",
	}
	for ref, want := range tests {
		got, err := service.ResolveTemplate(context.Background(), ref)
		if err != nil {
			t.Fatalf("ResolveTemplate(%q): %v", ref, err)
		}
		if got.ID.String() != want {
			t.Fatalf("ResolveTemplate(%q) = %s, want %s", ref, got.ID, want)
		}
	}

	if _, err := service.ResolveTemplate(context.Background(), "ste"); !errors.Is(err, ErrRefAmbiguous) {
		t.Fatalf("ambiguous error = %v", err)
	}
	if _, err := service.ResolveTemplate(context.Background(), "nope"); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("missing error = %v", err)
	}
	if _, err := service.ResolveTemplate(context.Background(), ""); !errors.Is(err, ErrRefEmpty) {
		t.Fatalf("empty error = %v", err)
	}
}

func TestResolveLockAndRoles(t *testing.T) {
	api := &fakeAPI{
		identity: &Identity{User: "me#0", ID: "me"},
		loaded: []LoadedLock{
			{ID: "1", LockName: "Mine", Lockee: "me#0", LockeeID: "me", Keyholder: "them#0", KeyholderID: "them"},
			{ID: "2", LockName: "Held", Lockee: "sub#0", LockeeID: "sub", Keyholder: "me#0", KeyholderID: "me"},
		},
		lockees: []LoadedLock{
			{ID: "2", LockName: "Held", Lockee: "sub#0", LockeeID: "sub", Keyholder: "me#0", KeyholderID: "me"},
		},
	}
	service := newTestService(api)

	mine, err := service.Lock(context.Background(), "Mine")
	if err != nil {
		t.Fatalf("Lock(Mine): %v", err)
	}
	if mine.Role != RoleLockee || !mine.IsLockee() || mine.IsKeyholder() {
		t.Fatalf("mine role = %q", mine.Role)
	}

	held, err := service.Lock(context.Background(), "sub")
	if err != nil {
		t.Fatalf("Lock(sub): %v", err)
	}
	if held.Role != RoleKeyholder || held.IsLockee() || !held.IsKeyholder() {
		t.Fatalf("held role = %q", held.Role)
	}

	all, err := service.Locks(context.Background(), LockFilter{Scope: ScopeAll})
	if err != nil {
		t.Fatalf("Locks: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all locks = %d, want 2", len(all))
	}

	mineOnly, err := service.Locks(context.Background(), LockFilter{Scope: ScopeMine})
	if err != nil {
		t.Fatalf("Locks(mine): %v", err)
	}
	if len(mineOnly) != 1 || mineOnly[0].Lock.ID != "1" {
		t.Fatalf("mine locks = %+v", mineOnly)
	}

	heldOnly, err := service.Locks(context.Background(), LockFilter{Scope: ScopeLockees})
	if err != nil {
		t.Fatalf("Locks(lockees): %v", err)
	}
	if len(heldOnly) != 1 || heldOnly[0].Lock.ID != "2" {
		t.Fatalf("lockee locks = %+v", heldOnly)
	}
}

func TestLockMutations(t *testing.T) {
	api := &fakeAPI{
		loaded: []LoadedLock{{ID: "1", LockName: "Mine", LockeeID: "me"}},
	}
	service := newTestService(api)
	ctx := context.Background()

	if _, err := service.AddLockTime(ctx, "Mine", time.Hour); err != nil {
		t.Fatalf("AddLockTime: %v", err)
	}
	if api.addTime != time.Hour {
		t.Fatalf("addTime = %s", api.addTime)
	}
	if _, err := service.SetLockTime(ctx, "1", 2*time.Hour); err != nil {
		t.Fatalf("SetLockTime: %v", err)
	}
	if api.remaining != 2*time.Hour {
		t.Fatalf("remaining = %s", api.remaining)
	}
	if _, err := service.FreezeLock(ctx, "1", true); err != nil {
		t.Fatalf("FreezeLock: %v", err)
	}
	if api.frozen == nil || !*api.frozen {
		t.Fatal("freeze was not sent")
	}
	if _, err := service.HideLock(ctx, "1", true); err != nil {
		t.Fatalf("HideLock: %v", err)
	}
	if api.hidden == nil || !*api.hidden {
		t.Fatal("hide was not sent")
	}
	if _, err := service.SetLockPurpose(ctx, "1", PurposeChastity); err != nil {
		t.Fatalf("SetLockPurpose: %v", err)
	}
	if api.purpose == nil || *api.purpose != PurposeChastity {
		t.Fatal("purpose was not sent")
	}
	if _, err := service.UpdateLockCards(ctx, "1", []CardChange{{Card: CardGreen, Delta: 1}}, UpdateOptions{}); err != nil {
		t.Fatalf("UpdateLockCards: %v", err)
	}
	if len(api.cardChanges) != 1 {
		t.Fatalf("card changes = %+v", api.cardChanges)
	}
	if _, err := service.UnlockLock(ctx, "1"); err != nil {
		t.Fatalf("UnlockLock: %v", err)
	}
	if api.unlockedID != "1" {
		t.Fatalf("unlocked = %s", api.unlockedID)
	}
	if err := service.AbandonLock(ctx, "1"); err != nil {
		t.Fatalf("AbandonLock: %v", err)
	}
	if api.abandonedID != "1" {
		t.Fatalf("abandoned = %s", api.abandonedID)
	}
	if _, err := service.SplitLock(ctx, "1", 3); err != nil {
		t.Fatalf("SplitLock: %v", err)
	}
	if api.splitCount != 3 {
		t.Fatalf("splitCount = %d", api.splitCount)
	}
}

func TestLockSettingsPatch(t *testing.T) {
	period := 90 * time.Minute
	next := 2 * time.Hour
	maxPicks := 5
	purpose := PurposeChastity
	out, err := (LockSettingsPatch{
		Regularity:        &period,
		TimeToNextCheckIn: &next,
		MaxPicks:          &maxPicks,
		LockPurpose:       &purpose,
	}).keyholderSettings()
	if err != nil {
		t.Fatalf("keyholderSettings: %v", err)
	}
	if out.Regularity == nil || *out.Regularity != 90 {
		t.Fatalf("Regularity = %v", out.Regularity)
	}
	if out.TimeToNextCheckIn == nil || *out.TimeToNextCheckIn != 7200 {
		t.Fatalf("TimeToNextCheckIn = %v", out.TimeToNextCheckIn)
	}
	if out.MaxPicks == nil || *out.MaxPicks != 5 {
		t.Fatalf("MaxPicks = %v", out.MaxPicks)
	}
	if out.LockPurpose == nil || *out.LockPurpose != PurposeChastity {
		t.Fatalf("LockPurpose = %v", out.LockPurpose)
	}

	if _, err := (LockSettingsPatch{}).keyholderSettings(); err == nil {
		t.Fatal("empty settings patch succeeded")
	}
	if _, err := (LockSettingsPatch{Regularity: durationPtr(90 * time.Second)}).keyholderSettings(); err == nil {
		t.Fatal("partial minute regularity succeeded")
	}
}

func TestServiceCacheAndInvalidation(t *testing.T) {
	api := &fakeAPI{templates: []Template{{ID: "1", LockName: "One"}}}
	service := newTestService(api)
	ctx := context.Background()

	if _, err := service.Templates(ctx); err != nil {
		t.Fatalf("Templates: %v", err)
	}
	if _, err := service.Templates(ctx); err != nil {
		t.Fatalf("Templates again: %v", err)
	}
	if api.templateCalls != 1 {
		t.Fatalf("template calls = %d, want 1", api.templateCalls)
	}
	service.Invalidate()
	if _, err := service.Templates(ctx); err != nil {
		t.Fatalf("Templates after invalidate: %v", err)
	}
	if api.templateCalls != 2 {
		t.Fatalf("template calls = %d, want 2", api.templateCalls)
	}
}

func TestCreateTemplateReturnsNewRow(t *testing.T) {
	api := &fakeAPI{templates: []Template{{ID: "old", LockName: "Old"}}}
	service := newTestService(api)

	created, err := service.CreateTemplate(context.Background(), TemplateDraft{
		Name: "New",
		Type: TemplateTypeTimer,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if created.ID != "new-id" || created.LockName != "New" {
		t.Fatalf("created = %+v", created)
	}
	if api.createPayload["lockname"] != "New" {
		t.Fatalf("payload = %#v", api.createPayload)
	}
}

func TestParseHelpers(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"90m":   90 * time.Minute,
		"2h30m": 2*time.Hour + 30*time.Minute,
		"3d":    72 * time.Hour,
		"1d12h": 36 * time.Hour,
		"-1h":   -time.Hour,
	} {
		got, err := ParseDuration(raw)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseDuration(%q) = %s, want %s", raw, got, want)
		}
	}
	if _, err := ParseDuration("nope"); err == nil {
		t.Fatal("ParseDuration accepted junk")
	}

	cards, err := ParseCards("green:2,g:1,red=1")
	if err == nil {
		t.Fatalf("ParseCards accepted red=1: %+v", cards)
	}
	cards, err = ParseCards("green:2,g:1,red:1")
	if err != nil {
		t.Fatalf("ParseCards: %v", err)
	}
	if cards[CardGreen] != 3 || cards[CardRed] != 1 {
		t.Fatalf("cards = %+v", cards)
	}
	if _, err := ParseCards("nope:1"); err == nil {
		t.Fatal("ParseCards accepted an unknown card")
	}

	mask, err := ParsePurposes("chastity,task")
	if err != nil {
		t.Fatalf("ParsePurposes: %v", err)
	}
	if mask != PurposeMaskChastity|PurposeMaskTask {
		t.Fatalf("mask = %d", mask)
	}
	if mask, err := ParsePurposes("all"); err != nil || mask != PurposeMaskTest|PurposeMaskChastity|PurposeMaskBondage|PurposeMaskTask|PurposeMaskLoyalty|PurposeMaskEmergency {
		t.Fatalf("all mask = %d, %v", mask, err)
	}
	if _, err := ParsePurposes("nope"); err == nil {
		t.Fatal("ParsePurposes accepted junk")
	}

	if got, err := ParseTemplateType("cardlock"); err != nil || got != TemplateTypeCard {
		t.Fatalf("ParseTemplateType = %q, %v", got, err)
	}
	if got, err := ParsePurpose("chastity"); err != nil || got != PurposeChastity {
		t.Fatalf("ParsePurpose = %q, %v", got, err)
	}
}

func durationPtr(value time.Duration) *time.Duration { return &value }

func TestPresentationRedactsCodes(t *testing.T) {
	view := &LockView{
		Lock: LoadedLock{
			ID:          "1",
			LockName:    "Lock",
			Lockee:      "sub#0",
			Keyholder:   "key#0",
			Type:        "timer",
			Combination: "1234",
		},
		Role: RoleKeyholder,
	}
	text := LockDetailText(view)
	if strings.Contains(text, "1234") {
		t.Fatalf("detail exposed the combination: %s", text)
	}

	template := &Template{ID: "1", LockName: "Template", Passcode: "secret"}
	text = TemplateDetailText(template)
	if strings.Contains(text, "secret") {
		t.Fatalf("detail exposed the passcode: %s", text)
	}
	if !strings.Contains(text, "Passcode: set") {
		t.Fatalf("detail did not report the passcode: %s", text)
	}
}
