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
	spec := map[string]any{
		"id":                       "id",
		"keyholder":                nil,
		"lockname":                 name,
		"locktype":                 "CARDLOCK",
		"hidden":                   "no",
		"startfrozen":              "no",
		"checkinperiod":            0,
		"checkintolerance":         0,
		"firstcheckin":             0,
		"checkInMissPenalty":       "",
		"timerMin":                 0,
		"timerMax":                 0,
		"cumulative":               "yes",
		"regularity":               60,
		"autoresetperiod":          0,
		"autoresetcount":           0,
		"cards":                    []map[string]any{{"name": "GREEN", "min": 2, "max": 2}, {"name": "RED", "min": 1, "max": 1}},
		"requiredgreens":           1001,
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
	greens := 1001
	if err := client.UpdateCardRange(ctx, created.ID, CardRange{
		Card:           CardGreen,
		Min:            2,
		Max:            2,
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

	if err := client.Abandon(ctx, lock.ID); err != nil {
		t.Fatalf("Abandon: %v", err)
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
	spec := map[string]any{
		"id":                       "id",
		"keyholder":                nil,
		"lockname":                 name,
		"locktype":                 "TIMEDLOCK",
		"hidden":                   "no",
		"startfrozen":              "no",
		"checkinperiod":            0,
		"checkintolerance":         0,
		"firstcheckin":             0,
		"checkInMissPenalty":       "",
		"timerMin":                 300,
		"timerMax":                 600,
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
		"description":              "grimoire timer smoke test",
		"passcode":                 nil,
		"pinkscript":               "",
		"background":               "pexels-pixabay-65650.jpg",
	}
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
