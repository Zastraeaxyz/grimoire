package chastisafe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestIntegrationRead is read-only and only runs when CHASTISAFE_DEV_CODE is
// set to a real developer code.
func TestIntegrationRead(t *testing.T) {
	code := os.Getenv("CHASTISAFE_DEV_CODE")
	if code == "" {
		t.Skip("CHASTISAFE_DEV_CODE is not set")
	}
	client, err := New(Options{Token: code})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identity, err := client.Who(ctx)
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	t.Logf("identity: %s (%s)", identity.User, identity.ID)

	profile, err := client.Profile(ctx)
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	t.Logf("profile: roles=%d chastity=%d", profile.Roles, profile.Stats.Chastity())

	templates, err := client.Templates(ctx)
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	t.Logf("templates: %d", len(templates))

	loaded, err := client.Loaded(ctx)
	if err != nil {
		t.Fatalf("Loaded: %v", err)
	}
	for _, lock := range loaded {
		t.Logf("loaded: %s type=%s status=%s", lock.LockName, lock.Type, lock.Status())
	}

	recent, err := client.Recent(ctx)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	t.Logf("recent: %d", len(recent))

	leaderboard, err := client.Leaderboard(ctx, StatsChastity)
	if err != nil {
		t.Fatalf("Leaderboard: %v", err)
	}
	if len(leaderboard) == 0 {
		t.Fatal("Leaderboard returned no rows")
	}
	t.Logf("leaderboard: %d rows, first %s with %d chastity locks", len(leaderboard), leaderboard[0].Name, leaderboard[0].ChastityLocks)

	var publicID string
	for _, row := range leaderboard {
		if row.Visible {
			publicID = row.ID.String()
			break
		}
	}
	if publicID != "" {
		publicProfile, err := client.ProfileFor(ctx, publicID)
		if err != nil {
			t.Fatalf("ProfileFor: %v", err)
		}
		if publicProfile.User == "" {
			t.Fatal("ProfileFor returned no user")
		}
		t.Logf("public profile: %s keyholder levels=%v", publicProfile.User, publicProfile.KeyholderLevels)
	}

	for _, row := range leaderboard {
		if row.Visible {
			continue
		}
		if _, err := client.ProfileFor(ctx, row.ID.String()); !errors.Is(err, ErrProfileNotPublic) {
			t.Fatalf("ProfileFor private %s error = %v, want ErrProfileNotPublic", row.ID, err)
		}
		t.Logf("private profile %s correctly rejected", row.ID)
		break
	}

	specificProfile, err := client.ProfileFor(ctx, "616062132982906886")
	if err != nil {
		t.Fatalf("ProfileFor specific: %v", err)
	}
	t.Logf("specific profile: %s levels=%v keyholder levels=%v", specificProfile.User, specificProfile.Levels, specificProfile.KeyholderLevels)
}

// TestIntegrationTemplateLifecycle creates and removes a test template. It is
// destructive and only runs when CHASTISAFE_WRITE_TEST=1 and
// CHASTISAFE_DEV_CODE are set.
func TestIntegrationTemplateLifecycle(t *testing.T) {
	if os.Getenv("CHASTISAFE_WRITE_TEST") != "1" {
		t.Skip("CHASTISAFE_WRITE_TEST is not 1")
	}
	code := os.Getenv("CHASTISAFE_DEV_CODE")
	if code == "" {
		t.Skip("CHASTISAFE_DEV_CODE is not set")
	}
	client, err := New(Options{Token: code})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	name := fmt.Sprintf("grimoire smoke %d", time.Now().Unix())
	spec := cardTemplateSpec(name, []map[string]any{{"name": "GREEN", "min": 1, "max": 1}}, 1, 0)
	if err := client.CreateTemplate(ctx, spec); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	var created *Template
	templates, err := client.Templates(ctx)
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	for i := range templates {
		if templates[i].LockName == name {
			created = &templates[i]
			break
		}
	}
	if created == nil {
		t.Fatalf("created template %q was not listed", name)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := client.DeleteTemplate(cleanupCtx, created.ID); err != nil {
			t.Logf("cleanup DeleteTemplate: %v", err)
		}
	})
	t.Logf("created template %s", created.ID)

	if err := client.SetBackground(ctx, created.ID, "pexels-pixabay-65650.jpg"); err != nil {
		t.Fatalf("SetBackground: %v", err)
	}

	got, err := client.Template(ctx, created.ID)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if got.LockName != name {
		t.Fatalf("template name = %q", got.LockName)
	}
	if !got.IsCardLock() {
		t.Fatalf("template type = %q", got.LockType)
	}

	if err := client.UpdateTemplate(ctx, map[string]any{
		"id":              created.ID,
		"lockname":        name,
		"locktype":        "CARDLOCK",
		"hidden":          "no",
		"startfrozen":     "no",
		"cumulative":      "yes",
		"regularity":      60,
		"cards":           []map[string]any{{"name": "GREEN", "min": 2, "max": 2}, {"name": "RED", "min": 1, "max": 1}},
		"requiredgreens":  1001,
		"status":          "ENABLED",
		"allowedpurposes": 3,
		"allowupdates":    true,
		"description":     "grimoire smoke test updated",
	}); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	greens := 1
	if err := client.UpdateCardRange(ctx, created.ID, CardRange{
		Card:           CardGreen,
		Min:            1,
		Max:            1,
		GreensRequired: &greens,
	}); err != nil {
		t.Fatalf("UpdateCardRange: %v", err)
	}

	edited, err := client.TemplateForEdit(ctx, created.ID)
	if err != nil {
		t.Fatalf("TemplateForEdit: %v", err)
	}
	if edited.Description != "grimoire smoke test updated" {
		t.Fatalf("edited description = %q", edited.Description)
	}

	sim, err := client.Simulate(ctx, created.ID)
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	t.Logf("simulation: avg=%ds min=%ds max=%ds", sim.AvgDuration, sim.MinDuration, sim.MaxDuration)

	locks, err := client.Load(ctx, LoadRequest{
		TemplateID: created.ID,
		Code:       "1234",
		Purpose:    PurposeChastity,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(locks) == 0 {
		t.Fatal("Load returned no locks")
	}
	lock := locks[0]
	t.Cleanup(func() { cleanupLoadedLock(t, client, lock.ID) })
	t.Logf("loaded lock %s type=%s cards=%d", lock.ID, lock.Type, lock.CardsRemaining())

	picked, err := client.Pick(ctx, lock.ID)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	t.Logf("picked %s done=%v", picked.Card, picked.Done)
	if !picked.Done {
		t.Fatalf("pick did not finish the one-green lock")
	}

	opened, err := client.Open(ctx, lock.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Code != "1234" {
		t.Fatalf("opened code = %q", opened.Code)
	}
}

func cleanupLoadedLock(t *testing.T, client *Client, lockID ID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	loaded, err := client.Loaded(ctx)
	if err != nil {
		t.Logf("cleanup Loaded: %v", err)
		return
	}
	for _, lock := range loaded {
		if lock.ID == lockID {
			if err := client.Abandon(ctx, lockID); err != nil {
				t.Logf("cleanup Abandon: %v", err)
			}
			return
		}
	}
}

// TestIntegrationTimerLifecycle creates and removes a timer template and
// exercises add-time plus the keyholder-only error path. It is destructive and
// only runs when CHASTISAFE_WRITE_TEST=1 and CHASTISAFE_DEV_CODE are set.
func TestIntegrationTimerLifecycle(t *testing.T) {
	if os.Getenv("CHASTISAFE_WRITE_TEST") != "1" {
		t.Skip("CHASTISAFE_WRITE_TEST is not 1")
	}
	code := os.Getenv("CHASTISAFE_DEV_CODE")
	if code == "" {
		t.Skip("CHASTISAFE_DEV_CODE is not set")
	}
	client, err := New(Options{Token: code})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	name := fmt.Sprintf("grimoire timer smoke %d", time.Now().Unix())
	spec := timerTemplateSpec(name, 300, 600, 0)
	if err := client.CreateTemplate(ctx, spec); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	var created *Template
	templates, err := client.Templates(ctx)
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	for i := range templates {
		if templates[i].LockName == name {
			created = &templates[i]
			break
		}
	}
	if created == nil {
		t.Fatalf("created timer template %q was not listed", name)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := client.DeleteTemplate(cleanupCtx, created.ID); err != nil {
			t.Logf("cleanup DeleteTemplate: %v", err)
		}
	})

	locks, err := client.Load(ctx, LoadRequest{
		TemplateID: created.ID,
		Code:       "5678",
		Purpose:    PurposeChastity,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(locks) == 0 {
		t.Fatal("Load returned no locks")
	}
	lock := locks[0]
	t.Cleanup(func() { cleanupLoadedLock(t, client, lock.ID) })
	if !lock.IsTimerLock() {
		t.Fatalf("lock type = %q", lock.Type)
	}
	before := lock.Remaining()
	t.Logf("loaded timer remaining=%s", before)

	updated, err := client.AddTime(ctx, lock.ID, 60*time.Second)
	if err != nil {
		t.Fatalf("AddTime: %v", err)
	}
	if updated != nil && updated.TimerRemaining < int(before/time.Second)+55 {
		t.Fatalf("remaining after add = %d, want at least %d", updated.TimerRemaining, int(before/time.Second)+55)
	}

	if _, err := client.SetRemainingTime(ctx, lock.ID, 10*time.Minute); !errors.Is(err, ErrLockNotFound) {
		t.Fatalf("SetRemainingTime error = %v, want ErrLockNotFound for a self-lock", err)
	}
	if _, err := client.Freeze(ctx, lock.ID, true); !errors.Is(err, ErrLockNotFound) {
		t.Fatalf("Freeze error = %v, want ErrLockNotFound for a self-lock", err)
	}
	if _, err := client.Hide(ctx, lock.ID, true); !errors.Is(err, ErrLockNotFound) {
		t.Fatalf("Hide error = %v, want ErrLockNotFound for a self-lock", err)
	}

	if err := client.Abandon(ctx, lock.ID); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
}

// TestIntegrationLockeeCardUpdate verifies the lockee card-update path. A
// lockee may change some cards (yellow variants) when the template allows
// updates, but not a green card. It is destructive and gated by
// CHASTISAFE_WRITE_TEST.
func TestIntegrationLockeeCardUpdate(t *testing.T) {
	if os.Getenv("CHASTISAFE_WRITE_TEST") != "1" {
		t.Skip("CHASTISAFE_WRITE_TEST is not 1")
	}
	code := os.Getenv("CHASTISAFE_DEV_CODE")
	if code == "" {
		t.Skip("CHASTISAFE_DEV_CODE is not set")
	}
	client, err := New(Options{Token: code})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	name := fmt.Sprintf("grimoire cards smoke %d", time.Now().Unix())
	spec := cardTemplateSpec(name, []map[string]any{{"name": "GREEN", "min": 1, "max": 1}, {"name": "YELLOWP1", "min": 1, "max": 1}}, 1001, 0)
	if err := client.CreateTemplate(ctx, spec); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	var created *Template
	templates, err := client.Templates(ctx)
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	for i := range templates {
		if templates[i].LockName == name {
			created = &templates[i]
			break
		}
	}
	if created == nil {
		t.Fatalf("created card template %q was not listed", name)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := client.DeleteTemplate(cleanupCtx, created.ID); err != nil {
			t.Logf("cleanup DeleteTemplate: %v", err)
		}
	})

	locks, err := client.Load(ctx, LoadRequest{
		TemplateID: created.ID,
		Code:       "1234",
		Purpose:    PurposeChastity,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	lock := locks[0]
	t.Cleanup(func() { cleanupLoadedLock(t, client, lock.ID) })
	t.Logf("loaded deck: %v", lock.Deck)

	if _, err := client.UpdateCards(ctx, lock.ID, []CardChange{
		{Card: CardYellowP1, Delta: 1},
	}, UpdateOptions{Message: "grimoire card update", Show: false}); err != nil {
		t.Fatalf("UpdateCards yellow: %v", err)
	}
	found, _, err := client.LoadedLock(ctx, lock.ID)
	if err != nil {
		t.Fatalf("LoadedLock after yellow update: %v", err)
	}
	t.Logf("deck after yellow update: %v", found.Deck)
	if found.Deck.Yellows() < 2 {
		t.Fatalf("yellow cards after update = %d, want at least 2", found.Deck.Yellows())
	}

	if _, err := client.UpdateCards(ctx, lock.ID, []CardChange{
		{Card: CardGreen, Delta: 1},
	}, UpdateOptions{Message: "grimoire green update", Show: false}); err == nil {
		t.Fatal("UpdateCards green succeeded for a lockee, want an error")
	} else {
		t.Logf("green update denied: %v", err)
	}

	if err := client.Abandon(ctx, lock.ID); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
}

func templateSpecBase(name, lockType string) map[string]any {
	return map[string]any{
		"id":                       "id",
		"keyholder":                nil,
		"lockname":                 name,
		"locktype":                 lockType,
		"hidden":                   "no",
		"startfrozen":              "no",
		"checkinperiod":            0,
		"checkintolerance":         0,
		"firstcheckin":             0,
		"checkInMissPenalty":       "",
		"timerMin":                 0,
		"timerMax":                 0,
		"cumulative":               "no",
		"regularity":               0,
		"autoresetperiod":          0,
		"autoresetcount":           0,
		"cards":                    []map[string]any{},
		"requiredgreens":           0,
		"botid":                    nil,
		"botname":                  nil,
		"contactkeyholder":         false,
		"blocklocked":              false,
		"fakesmin":                 0,
		"fakesmax":                 0,
		"trusttime":                0,
		"dicecount":                1,
		"dicefaces":                6,
		"lockees":                  0,
		"hygieneunlockperiod":      0,
		"hygieneunlockduration":    0,
		"firsthygieneunlock":       0,
		"hygieneRelockMissPenalty": "",
		"taskrollerid":             nil,
		"taskMissPenalty":          "",
		"status":                   "ENABLED",
		"allowedpurposes":          3,
		"minimumduration":          0,
		"minimumdurationenabled":   false,
		"allowupdates":             true,
		"description":              "grimoire smoke test",
		"passcode":                 nil,
		"pinkscript":               "",
		"background":               "pexels-pixabay-65650.jpg",
	}
}

func timerTemplateSpec(name string, minSeconds, maxSeconds, trustSeconds int) map[string]any {
	spec := templateSpecBase(name, "TIMEDLOCK")
	spec["timerMin"] = minSeconds
	spec["timerMax"] = maxSeconds
	spec["trusttime"] = trustSeconds
	spec["cumulative"] = "no"
	spec["regularity"] = 0
	return spec
}

func cardTemplateSpec(name string, cards []map[string]any, requiredGreens, trustSeconds int) map[string]any {
	spec := templateSpecBase(name, "CARDLOCK")
	spec["cards"] = cards
	spec["requiredgreens"] = requiredGreens
	spec["trusttime"] = trustSeconds
	spec["cumulative"] = "yes"
	spec["regularity"] = 60
	return spec
}

func findTemplateByName(t *testing.T, client *Client, name string) *Template {
	t.Helper()
	templates, err := client.Templates(context.Background())
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	for i := range templates {
		if templates[i].LockName == name {
			return &templates[i]
		}
	}
	return nil
}

// TestIntegrationKeyholderActions exercises the keyholder-only paths with two
// real accounts. It creates and removes templates and locks, so it is gated by
// CHASTISAFE_WRITE_TEST, CHASTISAFE_KEYHOLDER_DEV_CODE and
// CHASTISAFE_LOCKEE_DEV_CODE.
func TestIntegrationKeyholderActions(t *testing.T) {
	if os.Getenv("CHASTISAFE_WRITE_TEST") != "1" {
		t.Skip("CHASTISAFE_WRITE_TEST is not 1")
	}
	keyholderCode := os.Getenv("CHASTISAFE_KEYHOLDER_DEV_CODE")
	lockeeCode := os.Getenv("CHASTISAFE_LOCKEE_DEV_CODE")
	if keyholderCode == "" || lockeeCode == "" {
		t.Skip("CHASTISAFE_KEYHOLDER_DEV_CODE and CHASTISAFE_LOCKEE_DEV_CODE are required")
	}
	keyholder, err := New(Options{Token: keyholderCode})
	if err != nil {
		t.Fatalf("New keyholder: %v", err)
	}
	lockee, err := New(Options{Token: lockeeCode})
	if err != nil {
		t.Fatalf("New lockee: %v", err)
	}
	keyholderWho, err := keyholder.Who(context.Background())
	if err != nil {
		t.Fatalf("keyholder Who: %v", err)
	}
	lockeeWho, err := lockee.Who(context.Background())
	if err != nil {
		t.Fatalf("lockee Who: %v", err)
	}
	if keyholderWho.ID == lockeeWho.ID {
		t.Fatal("keyholder and lockee tokens resolve to the same account")
	}
	t.Logf("keyholder %s (%s), lockee %s (%s)", keyholderWho.User, keyholderWho.ID, lockeeWho.User, lockeeWho.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	timerName := fmt.Sprintf("grimoire kh timer %d", time.Now().Unix())
	if err := keyholder.CreateTemplate(ctx, timerTemplateSpec(timerName, 300, 600, 3600)); err != nil {
		t.Fatalf("CreateTemplate timer: %v", err)
	}
	timerTemplate := findTemplateByName(t, keyholder, timerName)
	if timerTemplate == nil {
		t.Fatalf("timer template %q not found", timerName)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := keyholder.DeleteTemplate(cleanupCtx, timerTemplate.ID); err != nil {
			t.Logf("cleanup timer template: %v", err)
		}
	})

	timerLocks, err := lockee.Load(ctx, LoadRequest{
		TemplateID: timerTemplate.ID,
		Code:       "1234",
		Purpose:    PurposeChastity,
	})
	if err != nil {
		t.Fatalf("Load timer: %v", err)
	}
	timerLock := timerLocks[0]
	t.Cleanup(func() { cleanupLoadedLock(t, lockee, timerLock.ID) })
	t.Logf("loaded timer lock %s", timerLock.ID)

	lockees, err := keyholder.Lockees(ctx)
	if err != nil {
		t.Fatalf("Lockees: %v", err)
	}
	found := false
	for _, entry := range lockees {
		if entry.ID == timerLock.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("keyholder does not see lock %s", timerLock.ID)
	}

	frozen, err := keyholder.Freeze(ctx, timerLock.ID, true)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if frozen != nil && !frozen.FrozenByKeyholder {
		t.Fatalf("Freeze response not frozen: %+v", frozen)
	}
	state, _, err := lockee.LoadedLock(ctx, timerLock.ID)
	if err != nil {
		t.Fatalf("LoadedLock after freeze: %v", err)
	}
	if !state.FrozenByKeyholder {
		t.Fatal("lockee does not see the keyholder freeze")
	}

	if _, err := keyholder.Hide(ctx, timerLock.ID, true); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	state, _, err = lockee.LoadedLock(ctx, timerLock.ID)
	if err != nil {
		t.Fatalf("LoadedLock after hide: %v", err)
	}
	if !state.Hidden {
		t.Fatal("lockee does not see the hidden flag")
	}

	if _, err := keyholder.Freeze(ctx, timerLock.ID, false); err != nil {
		t.Fatalf("Unfreeze: %v", err)
	}
	if _, err := keyholder.Hide(ctx, timerLock.ID, false); err != nil {
		t.Fatalf("Unhide: %v", err)
	}
	updated, err := keyholder.SetRemainingTime(ctx, timerLock.ID, 10*time.Minute)
	if err != nil {
		t.Fatalf("SetRemainingTime: %v", err)
	}
	if updated != nil && updated.TimerRemaining < 590 {
		t.Fatalf("remaining after set = %d, want about 600", updated.TimerRemaining)
	}
	state, _, err = lockee.LoadedLock(ctx, timerLock.ID)
	if err != nil {
		t.Fatalf("LoadedLock after remaining: %v", err)
	}
	if state.TimerRemaining < 590 {
		t.Fatalf("lockee remaining = %d, want about 600", state.TimerRemaining)
	}

	trusted, err := lockee.Trust(ctx, timerLock.ID, time.Hour)
	if err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if trusted != nil && trusted.TrustEndTime.IsZero() && trusted.RemainingGuaranteed == 0 {
		t.Fatalf("trust response has no trust end: %+v", trusted)
	}

	if _, err := keyholder.Unlock(ctx, timerLock.ID); err != nil {
		t.Fatalf("Unlock timer: %v", err)
	}
	loaded, err := lockee.Loaded(ctx)
	if err != nil {
		t.Fatalf("Loaded after timer unlock: %v", err)
	}
	for _, lock := range loaded {
		if lock.ID == timerLock.ID {
			t.Fatal("timer lock still loaded after keyholder unlock")
		}
	}

	cardName := fmt.Sprintf("grimoire kh card %d", time.Now().Unix())
	cardSpec := cardTemplateSpec(cardName, []map[string]any{
		{"name": "GREEN", "min": 1, "max": 1},
		{"name": "RED", "min": 1, "max": 1},
	}, 1, 3600)
	if err := keyholder.CreateTemplate(ctx, cardSpec); err != nil {
		t.Fatalf("CreateTemplate card: %v", err)
	}
	cardTemplate := findTemplateByName(t, keyholder, cardName)
	if cardTemplate == nil {
		t.Fatalf("card template %q not found", cardName)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := keyholder.DeleteTemplate(cleanupCtx, cardTemplate.ID); err != nil {
			t.Logf("cleanup card template: %v", err)
		}
	})

	cardLocks, err := lockee.Load(ctx, LoadRequest{
		TemplateID: cardTemplate.ID,
		Code:       "4321",
		Purpose:    PurposeChastity,
	})
	if err != nil {
		t.Fatalf("Load card: %v", err)
	}
	cardLock := cardLocks[0]
	t.Cleanup(func() { cleanupLoadedLock(t, lockee, cardLock.ID) })
	t.Logf("loaded card lock %s deck=%v", cardLock.ID, cardLock.Deck)

	if _, err := keyholder.UpdateCards(ctx, cardLock.ID, []CardChange{
		{Card: CardGreen, Delta: 1},
		{Card: CardRed, Delta: -1},
	}, UpdateOptions{Message: "keyholder card update", Show: true}); err != nil {
		t.Fatalf("UpdateCards keyholder: %v", err)
	}
	state, _, err = lockee.LoadedLock(ctx, cardLock.ID)
	if err != nil {
		t.Fatalf("LoadedLock after card update: %v", err)
	}
	if state.Deck.Greens() != 2 || state.Deck.Reds() != 0 {
		t.Fatalf("deck after keyholder update = %v, want 2 green and 0 red", state.Deck)
	}

	split, err := keyholder.Split(ctx, cardLock.ID, 2)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(split) < 2 {
		t.Fatalf("Split returned %d locks", len(split))
	}
	for _, lock := range split {
		lock := lock
		t.Cleanup(func() { cleanupLoadedLock(t, lockee, lock.ID) })
		if _, err := keyholder.Unlock(ctx, lock.ID); err != nil {
			t.Fatalf("Unlock split lock %s: %v", lock.ID, err)
		}
	}
	loaded, err = lockee.Loaded(ctx)
	if err != nil {
		t.Fatalf("Loaded after card unlock: %v", err)
	}
	for _, lock := range loaded {
		if lock.ID == cardLock.ID || lock.RealLockID == cardLock.ID {
			t.Fatalf("card lock still loaded after split unlock: %+v", lock)
		}
	}
}
