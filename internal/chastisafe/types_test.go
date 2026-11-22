package chastisafe

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTimeAcceptsRFC3339AndEpoch(t *testing.T) {
	var values struct {
		Loaded Time `json:"loaded"`
		Log    Time `json:"log"`
		Millis Time `json:"millis"`
		Empty  Time `json:"empty"`
	}
	if err := json.Unmarshal([]byte(`{
		"loaded":"2026-11-22T06:57:20Z",
		"log":1790810209,
		"millis":1790810209000,
		"empty":null
	}`), &values); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if values.Loaded.Year() != 2026 || values.Loaded.Month() != time.November || values.Loaded.Day() != 22 {
		t.Fatalf("loaded = %s", values.Loaded)
	}
	if values.Log.Unix() != 1790810209 {
		t.Fatalf("log = %s", values.Log)
	}
	if values.Millis.Unix() != 1790810209 {
		t.Fatalf("millis = %s", values.Millis)
	}
	if !values.Empty.IsZero() {
		t.Fatalf("empty = %s", values.Empty)
	}
}

func TestStatusAcceptsBoolAndNumber(t *testing.T) {
	cases := map[string]Status{
		`0`:      StatusOK,
		`1`:      StatusError,
		`false`:  StatusOK,
		`true`:   StatusError,
		`"0"`:    StatusOK,
		`"true"`: StatusError,
	}
	for input, want := range cases {
		var got Status
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if got != want {
			t.Fatalf("%s = %d, want %d", input, got, want)
		}
	}
}

func TestDeckNormalizesCards(t *testing.T) {
	var deck Deck
	if err := json.Unmarshal([]byte(`{"yellow":2,"GREEN":3,"YELLOWM1":1}`), &deck); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if deck.Total() != 6 {
		t.Fatalf("total = %d", deck.Total())
	}
	if deck.Greens() != 3 {
		t.Fatalf("greens = %d", deck.Greens())
	}
	if deck.Yellows() != 3 {
		t.Fatalf("yellows = %d", deck.Yellows())
	}
	if !deck.Has(CardYellow) || !deck.Has(CardYellowM1) {
		t.Fatalf("deck = %+v", deck)
	}
}

func TestDraws(t *testing.T) {
	draws := Draws("grf3x")
	cards := draws.Cards()
	if len(cards) != 5 {
		t.Fatalf("cards = %+v", cards)
	}
	if cards[0] != CardGreen || cards[1] != CardRed || cards[2] != CardFreeze {
		t.Fatalf("cards = %+v", cards)
	}
	if cards[3] != CardYellowP3 || cards[4] != CardYellow {
		t.Fatalf("cards = %+v", cards)
	}
	if draws.Count(CardGreen) != 1 || draws.Count(CardRed) != 1 {
		t.Fatalf("draws = %q", draws)
	}
}

func TestPurposeMask(t *testing.T) {
	mask := PurposeMaskTest | PurposeMaskChastity | PurposeMaskBondage | PurposeMaskTask
	if !mask.Has(PurposeTest) || !mask.Has(PurposeChastity) || !mask.Has(PurposeBondage) || !mask.Has(PurposeTask) {
		t.Fatalf("mask = %d", mask)
	}
	if mask.Has(PurposeLoyalty) || mask.Has(PurposeEmergency) {
		t.Fatalf("mask = %d", mask)
	}
	if got := len(mask.Strings()); got != 4 {
		t.Fatalf("strings = %v", mask.Strings())
	}
}

func TestCardLabels(t *testing.T) {
	if CardGreen.Label() != "Green card" {
		t.Fatalf("green label = %q", CardGreen.Label())
	}
	if CardYellowP1.Label() == "" || CardYellowP1.Description() == "" {
		t.Fatalf("yellow card has no label or description")
	}
	if NormalizeCard("yellow") != CardYellow {
		t.Fatalf("normalize yellow = %q", NormalizeCard("yellow"))
	}
	if !CardGreen.Valid() || Card("NOPE").Valid() {
		t.Fatal("Valid is wrong")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                            "0 seconds",
		time.Second:                  "1 second",
		2 * time.Second:              "2 seconds",
		time.Minute:                  "1 minute",
		90 * time.Minute:             "1 hour 30 minutes",
		25*time.Hour + 2*time.Minute: "1 day 1 hour 2 minutes",
		86400 * time.Second:          "1 day",
	}
	for input, want := range cases {
		if got := FormatDuration(input); got != want {
			t.Fatalf("FormatDuration(%s) = %q, want %q", input, got, want)
		}
	}
	if got := FormatMinutes(60); got != "1 hour" {
		t.Fatalf("FormatMinutes(60) = %q", got)
	}
	if got := FormatShortDuration(90 * time.Minute); got != "1h 30m" {
		t.Fatalf("FormatShortDuration = %q", got)
	}
}

func TestDeckSummaryAndLockSummary(t *testing.T) {
	deck := Deck{CardGreen: 3, CardRed: 1, CardFreeze: 2}
	got := DeckSummary(deck)
	if got != "3 green, 1 red, 2 freeze" {
		t.Fatalf("DeckSummary = %q", got)
	}

	lock := &LoadedLock{
		LockName:       "test",
		Keyholder:      "<self>",
		KeyholderID:    "0",
		Type:           "timer",
		TimerRemaining: 3600,
	}
	if got := LockSummary(lock); got != "test (self-locked): 1 hour remaining" {
		t.Fatalf("LockSummary = %q", got)
	}
}
