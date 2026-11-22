package chastisafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := New(Options{
		BaseURL: srv.URL,
		Token:   "test-token",
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestNewValidatesOptions(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("New accepted an empty token")
	}
	if _, err := New(Options{Token: "x", BaseURL: "not-a-url"}); err == nil {
		t.Fatal("New accepted a relative base URL")
	}
	if _, err := New(Options{Token: "x", BaseURL: "ftp://example.com"}); err == nil {
		t.Fatal("New accepted a non-http base URL")
	}
}

func TestWho(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/who" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("chastisafedev"); got != "test-token" {
			t.Errorf("chastisafedev = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("User-Agent is empty")
		}
		_, _ = io.WriteString(w, `{"status":0,"user":"testuser#0","id":"990454274196668516","displayMode":5}`)
	})

	identity, err := client.Who(context.Background())
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if identity.User != "testuser#0" || identity.ID != "990454274196668516" {
		t.Fatalf("identity = %+v", identity)
	}
	if !identity.OwnedBy("990454274196668516") {
		t.Fatal("OwnedBy returned false for the matching Discord ID")
	}
	if identity.OwnedBy("0") {
		t.Fatal("OwnedBy accepted a different Discord ID")
	}
}

func TestWhoUnauthorized(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":1}`)
	})
	_, err := client.Who(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Who error = %v, want ErrUnauthorized", err)
	}
}

func TestProfileRedactsDeveloperCode(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profile" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write(fixture(t, "profile.json"))
	})

	profile, err := client.Profile(context.Background())
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if profile.DeveloperCode != "secret-code" {
		t.Fatalf("developer code = %q", profile.DeveloperCode)
	}
	if !strings.Contains(profile.String(), "[redacted]") || strings.Contains(profile.String(), "secret-code") {
		t.Fatalf("String leaked the developer code: %s", profile.String())
	}
	if profile.Stats.Chastity() != 89123 {
		t.Fatalf("chastity = %d", profile.Stats.Chastity())
	}
	if profile.ID != "990454274196668516" {
		t.Fatalf("id = %q", profile.ID)
	}
}

func TestLoadedCardAndTimer(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loaded" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body := append(fixture(t, "loaded_card.json"), []byte("\n")...)
		_, _ = w.Write(body)
	})

	locks, err := client.Loaded(context.Background())
	if err != nil {
		t.Fatalf("Loaded: %v", err)
	}
	if len(locks) != 1 {
		t.Fatalf("locks = %d", len(locks))
	}
	card := locks[0]
	if !card.IsCardLock() || card.IsTimerLock() {
		t.Fatalf("type = %q", card.Type)
	}
	if !card.IsSelfLock() {
		t.Fatal("IsSelfLock returned false")
	}
	if card.Deck.Greens() != 1 || card.Deck.Reds() != 1 || card.Deck.Total() != 2 {
		t.Fatalf("deck = %+v", card.Deck)
	}
	if card.Draws.Count(CardGreen) != 1 {
		t.Fatalf("draws green = %d", card.Draws.Count(CardGreen))
	}
	if !card.IsReady() {
		t.Fatal("card lock should be ready")
	}
	if card.CardsRemaining() != 2 {
		t.Fatalf("cards remaining = %d", card.CardsRemaining())
	}
	if card.GreensRemaining() != 1 {
		t.Fatalf("greens remaining = %d", card.GreensRemaining())
	}
}

func TestLoadedTimer(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture(t, "loaded_timer.json"))
	})
	locks, err := client.Loaded(context.Background())
	if err != nil {
		t.Fatalf("Loaded: %v", err)
	}
	lock := locks[0]
	if !lock.IsTimerLock() || lock.IsCardLock() {
		t.Fatalf("type = %q", lock.Type)
	}
	if lock.Remaining() != 94*time.Second {
		t.Fatalf("remaining = %s", lock.Remaining())
	}
	if lock.IsReady() {
		t.Fatal("timer lock should not be ready")
	}
}

func TestLoadRequest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/load" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("Content-Type = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["templateid"] != "template-id" || body["code"] != "1234" {
			t.Errorf("body = %+v", body)
		}
		if body["lockpurpose"] != "CHASTITY" {
			t.Errorf("lockpurpose = %v", body["lockpurpose"])
		}
		fakes, ok := body["fakes"].([]any)
		if !ok || len(fakes) != 0 {
			t.Errorf("fakes = %#v, want an empty array", body["fakes"])
		}
		if value, exists := body["passcode"]; !exists || value != nil {
			t.Errorf("passcode = %#v, want an explicit null", value)
		}
		_, _ = w.Write([]byte(`{"locks":[{"id":"lock-id","lockname":"test","type":"timer","timeremaining":60,"readyToUnlock":false}]}`))
	})

	locks, err := client.Load(context.Background(), LoadRequest{
		TemplateID: "template-id",
		Code:       "1234",
		Purpose:    PurposeChastity,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(locks) != 1 || locks[0].ID != "lock-id" {
		t.Fatalf("locks = %+v", locks)
	}
}

func TestAddTimePayload(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/update" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["id"] != "lock-id" || body["addtime"] != float64(60) {
			t.Errorf("body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"id":"lock-id","type":"timer","timeremaining":154}`))
	})

	lock, err := client.AddTime(context.Background(), "lock-id", time.Minute)
	if err != nil {
		t.Fatalf("AddTime: %v", err)
	}
	if lock == nil || lock.TimerRemaining != 154 {
		t.Fatalf("lock = %+v", lock)
	}
}

func TestUpdatePayloads(t *testing.T) {
	cases := []struct {
		name string
		call func(*Client) error
		want map[string]any
	}{
		{
			name: "freeze",
			call: func(c *Client) error {
				_, err := c.Freeze(context.Background(), "lock-id", true)
				return err
			},
			want: map[string]any{"id": "lock-id", "frozenbykh": true},
		},
		{
			name: "hide",
			call: func(c *Client) error {
				_, err := c.Hide(context.Background(), "lock-id", false)
				return err
			},
			want: map[string]any{"id": "lock-id", "hidden": false},
		},
		{
			name: "purpose",
			call: func(c *Client) error {
				_, err := c.SetPurpose(context.Background(), "lock-id", PurposeBondage)
				return err
			},
			want: map[string]any{"id": "lock-id", "lockPurpose": "BONDAGE"},
		},
		{
			name: "remaining",
			call: func(c *Client) error {
				_, err := c.SetRemainingTime(context.Background(), "lock-id", 5*time.Minute)
				return err
			},
			want: map[string]any{"id": "lock-id", "remainingtime": float64(300)},
		},
		{
			name: "cards",
			call: func(c *Client) error {
				_, err := c.UpdateCards(context.Background(), "lock-id", []CardChange{
					{Card: CardGreen, Delta: 1},
					{Card: CardRed, Delta: -1},
				}, UpdateOptions{Message: "hello", Show: true})
				return err
			},
			want: map[string]any{
				"id":         "lock-id",
				"message":    "hello",
				"showupdate": true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				for key, want := range tc.want {
					if body[key] != want {
						t.Errorf("%s = %#v, want %#v", key, body[key], want)
					}
				}
				if tc.name == "cards" {
					cards, ok := body["cards"].([]any)
					if !ok || len(cards) != 2 {
						t.Errorf("cards = %#v", body["cards"])
					}
				}
				_, _ = w.Write([]byte(`{"id":"lock-id","type":"card"}`))
			})
			if err := tc.call(client); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
		})
	}
}

func TestOpen(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open/ready":
			_, _ = io.WriteString(w, `{"status":0,"locked":false,"code":"1234"}`)
		case "/open/locked":
			_, _ = io.WriteString(w, `{"status":1,"locked":true}`)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	})

	ready, err := client.Open(context.Background(), "ready")
	if err != nil {
		t.Fatalf("Open ready: %v", err)
	}
	if ready.Code != "1234" || ready.Locked {
		t.Fatalf("ready = %+v", ready)
	}

	locked, err := client.Open(context.Background(), "locked")
	if !errors.Is(err, ErrLockNotReady) {
		t.Fatalf("Open locked error = %v", err)
	}
	if locked == nil || !locked.Locked {
		t.Fatalf("locked = %+v", locked)
	}
}

func TestAbandon(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abandon/lock-id" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"status":0,"locked":false}`)
	})
	if err := client.Abandon(context.Background(), "lock-id"); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
}

func TestPick(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pick/lock-id" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"status":0,"card":"GREEN","done":true,"dice":4,"lock":{"id":"lock-id","type":"card","deck":{"RED":1}}}`)
	})
	result, err := client.Pick(context.Background(), "lock-id")
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if result.Card != CardGreen || !result.Done || result.Dice != 4 {
		t.Fatalf("result = %+v", result)
	}
	if result.Lock == nil || result.Lock.Deck.Reds() != 1 {
		t.Fatalf("lock = %+v", result.Lock)
	}
}

func TestTrust(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trust" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["lockId"] != "lock-id" || body["time"] != float64(3600) {
			t.Errorf("body = %+v", body)
		}
		_, _ = io.WriteString(w, `{"status":0,"locked":true,"lock":{"id":"lock-id","trustendtime":"2027-01-21T07:29:29Z","remainingguaranteed":86399}}`)
	})
	lock, err := client.Trust(context.Background(), "lock-id", time.Hour)
	if err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if lock == nil || lock.TrustEndTime.IsZero() || lock.RemainingGuaranteed != 86399 {
		t.Fatalf("lock = %+v", lock)
	}
}

func TestSplit(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/split" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["lockid"] != "lock-id" || body["count"] != float64(2) {
			t.Errorf("body = %+v", body)
		}
		_, _ = io.WriteString(w, `{"status":0,"locks":[{"id":"fake-1","type":"card"},{"id":"lock-id","type":"card"}]}`)
	})
	locks, err := client.Split(context.Background(), "lock-id", 2)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(locks) != 2 || locks[0].ID != "fake-1" {
		t.Fatalf("locks = %+v", locks)
	}
}

func TestUnlockUsesKeyholderPath(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/unlock/lock-id" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"status":0}`)
	})
	if _, err := client.Unlock(context.Background(), "lock-id"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func TestAPIReasonError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":1,"reason":"locknotfound"}`)
	})
	_, _, err := client.LoadedLock(context.Background(), "missing")
	if !errors.Is(err, ErrLockNotFound) {
		t.Fatalf("error = %v, want ErrLockNotFound", err)
	}
	if !IsReason(err, "locknotfound") {
		t.Fatalf("IsReason = false for %v", err)
	}
}

func TestHTTPError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "<html><body>boom</body></html>")
	})
	_, err := client.Templates(context.Background())
	if err == nil {
		t.Fatal("Templates succeeded")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.HTTPStatus != http.StatusInternalServerError {
		t.Fatalf("HTTPStatus = %d", apiErr.HTTPStatus)
	}
	if !strings.Contains(apiErr.Message, "boom") {
		t.Fatalf("Message = %q", apiErr.Message)
	}
}

func TestRecentAndLog(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/recent":
			_, _ = w.Write(fixture(t, "recent.json"))
		case "/log":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode log body: %v", err)
			}
			if body["lockId"] != "lock-id" || body["count"] != float64(100) {
				t.Errorf("log body = %+v", body)
			}
			_, _ = w.Write(fixture(t, "log.json"))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	})

	recent, err := client.Recent(context.Background())
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 || recent[0].LockID != "e164982d-749e-2eda-a235-1738d516957f" {
		t.Fatalf("recent = %+v", recent)
	}
	if UnlockReasonString(recent[0].UnlockReason) != "unlocked by keyholder" {
		t.Fatalf("unlock reason = %q", UnlockReasonString(recent[0].UnlockReason))
	}

	entries, err := client.Log(context.Background(), "lock-id", 0, 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(entries) != 4 || entries[1].Card != "r" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestWithToken(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("chastisafedev"); got != "other-token" {
			t.Errorf("chastisafedev = %q", got)
		}
		_, _ = io.WriteString(w, `{"status":0,"user":"other#0","id":"1"}`)
	})
	other := client.WithToken("other-token")
	if _, err := other.Who(context.Background()); err != nil {
		t.Fatalf("Who: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := client.Loaded(ctx); err == nil {
		t.Fatal("Loaded succeeded with a cancelled context")
	}
}

func TestCreateTemplateAndDelete(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/create":
			_, _ = io.WriteString(w, `{"status":0}`)
		case r.URL.Path == "/delete/template-id":
			_, _ = io.WriteString(w, `{"status":0}`)
		case r.URL.Path == "/simulate":
			_, _ = io.WriteString(w, `{"status":0,"avgduration":63,"minduration":30,"maxduration":90,"avgcards":1,"mincards":1,"maxcards":2,"numberOfSimulations":50}`)
		case r.URL.Path == "/updatecardrange":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode card range body: %v", err)
			}
			if body["templateId"] != "template-id" || body["cardName"] != "GREEN" {
				t.Errorf("card range body = %+v", body)
			}
			if body["minCount"] != float64(2) || body["maxCount"] != float64(2) {
				t.Errorf("card range counts = %+v", body)
			}
			_, _ = io.WriteString(w, `{"status":0}`)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	})

	if err := client.CreateTemplate(context.Background(), map[string]any{
		"lockname": "test",
		"locktype": "CARDLOCK",
	}); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if err := client.DeleteTemplate(context.Background(), "template-id"); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	greens := 1001
	if err := client.UpdateCardRange(context.Background(), "template-id", CardRange{
		Card:           CardGreen,
		Min:            2,
		Max:            2,
		GreensRequired: &greens,
	}); err != nil {
		t.Fatalf("UpdateCardRange: %v", err)
	}
	sim, err := client.Simulate(context.Background(), "template-id")
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if sim.AvgDuration != 63 || sim.NumberOfSimulations != 50 {
		t.Fatalf("sim = %+v", sim)
	}
}

func TestDeleteScriptUsesDeleteMethod(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/scripts/template-id/script-id" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"status":0}`)
	})
	if err := client.DeleteScript(context.Background(), "template-id", "script-id"); err != nil {
		t.Fatalf("DeleteScript: %v", err)
	}
}
