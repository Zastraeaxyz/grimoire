package chastisafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ID is a ChastiSafe identifier. The API is inconsistent: some fields are
// JSON strings and some are JSON numbers, so ID accepts both.
type ID string

func (id *ID) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*id = ""
		return nil
	}
	if len(text) >= 2 && text[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*id = ID(strings.TrimSpace(value))
		return nil
	}
	if _, err := strconv.ParseInt(text, 10, 64); err != nil {
		return fmt.Errorf("chastisafe: %q is not an identifier", text)
	}
	*id = ID(text)
	return nil
}

func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(id))
}

func (id ID) String() string { return string(id) }

func (id ID) IsZero() bool { return id == "" }

// Time accepts the RFC3339 strings used by loaded locks and the epoch seconds
// used by history and logs.
type Time struct {
	time.Time
}

func (t *Time) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		t.Time = time.Time{}
		return nil
	}
	if text[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		parsed, err := parseTimeString(value)
		if err != nil {
			return err
		}
		t.Time = parsed
		return nil
	}

	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("chastisafe: %q is not a timestamp", text)
	}
	t.Time = timeFromUnixNumber(seconds)
	return nil
}

func (t Time) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Time.UTC().Format(time.RFC3339Nano))
}

func (t Time) IsZero() bool { return t.Time.IsZero() }

func parseTimeString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return timeFromUnixNumber(seconds), nil
	}
	return time.Time{}, fmt.Errorf("chastisafe: %q is not a timestamp", value)
}

func timeFromUnixNumber(value int64) time.Time {
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

// FlexString accepts a JSON string, number, boolean or null. Several lock
// settings are numeric in one response and text in another.
type FlexString string

func (s *FlexString) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*s = ""
		return nil
	}
	if text[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = FlexString(value)
		return nil
	}
	*s = FlexString(text)
	return nil
}

func (s FlexString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

func (s FlexString) String() string { return string(s) }

// Purpose is a lock purpose.
type Purpose string

const (
	PurposeTest      Purpose = "TEST"
	PurposeChastity  Purpose = "CHASTITY"
	PurposeBondage   Purpose = "BONDAGE"
	PurposeTask      Purpose = "TASK"
	PurposeLoyalty   Purpose = "LOYALTY"
	PurposeEmergency Purpose = "EMERGENCY"
)

// Valid reports whether p is a known purpose.
func (p Purpose) Valid() bool {
	switch p {
	case PurposeTest, PurposeChastity, PurposeBondage, PurposeTask, PurposeLoyalty, PurposeEmergency:
		return true
	}
	return false
}

// PurposeMask is the allowedpurposes bit set.
type PurposeMask uint8

const (
	PurposeMaskTest PurposeMask = 1 << iota
	PurposeMaskChastity
	PurposeMaskBondage
	PurposeMaskTask
	PurposeMaskLoyalty
	PurposeMaskEmergency
)

// Has reports whether the mask contains p.
func (m PurposeMask) Has(p Purpose) bool {
	switch p {
	case PurposeTest:
		return m&PurposeMaskTest != 0
	case PurposeChastity:
		return m&PurposeMaskChastity != 0
	case PurposeBondage:
		return m&PurposeMaskBondage != 0
	case PurposeTask:
		return m&PurposeMaskTask != 0
	case PurposeLoyalty:
		return m&PurposeMaskLoyalty != 0
	case PurposeEmergency:
		return m&PurposeMaskEmergency != 0
	}
	return false
}

// Strings returns the purposes in the mask.
func (m PurposeMask) Strings() []string {
	all := []struct {
		mask    PurposeMask
		purpose Purpose
	}{
		{PurposeMaskTest, PurposeTest},
		{PurposeMaskChastity, PurposeChastity},
		{PurposeMaskBondage, PurposeBondage},
		{PurposeMaskTask, PurposeTask},
		{PurposeMaskLoyalty, PurposeLoyalty},
		{PurposeMaskEmergency, PurposeEmergency},
	}
	out := make([]string, 0, len(all))
	for _, entry := range all {
		if m&entry.mask != 0 {
			out = append(out, string(entry.purpose))
		}
	}
	return out
}

// Card is a card in a card lock deck.
type Card string

const (
	CardGreen    Card = "GREEN"
	CardRed      Card = "RED"
	CardSticky   Card = "STICKY"
	CardDoubleUp Card = "DOUBLEUP"
	CardReset    Card = "RESET"
	CardYellowP1 Card = "YELLOWP1"
	CardYellowP2 Card = "YELLOWP2"
	CardYellowP3 Card = "YELLOWP3"
	CardYellowM1 Card = "YELLOWM1"
	CardYellowM2 Card = "YELLOWM2"
	CardYellow   Card = "YELLOW"
	CardYellowUK Card = "YELLOWUK"
	CardYellowRN Card = "YELLOWRND"
	CardFreeze   Card = "FREEZE"
	CardArctic   Card = "ARCTIC"
	CardStun     Card = "STUN"
	CardDice     Card = "DICE"
	CardPink     Card = "PINK"
	CardGoAgain  Card = "GOAGAIN"
)

var cardLetters = map[Card]string{
	CardGreen:    "g",
	CardRed:      "r",
	CardFreeze:   "f",
	CardArctic:   "a",
	CardReset:    "e",
	CardSticky:   "s",
	CardStun:     "t",
	CardDoubleUp: "d",
	CardYellowM1: "y",
	CardYellowM2: "w",
	CardYellowP1: "1",
	CardYellowP2: "2",
	CardYellowP3: "3",
	CardYellow:   "x",
	CardYellowUK: "l",
	CardDice:     "i",
	CardPink:     "p",
	CardGoAgain:  "o",
}

var letterCards = map[byte]Card{
	'g': CardGreen,
	'r': CardRed,
	'f': CardFreeze,
	'a': CardArctic,
	'e': CardReset,
	's': CardSticky,
	't': CardStun,
	'd': CardDoubleUp,
	'y': CardYellowM1,
	'w': CardYellowM2,
	'1': CardYellowP1,
	'2': CardYellowP2,
	'3': CardYellowP3,
	'x': CardYellow,
	'l': CardYellowUK,
	'i': CardDice,
	'p': CardPink,
	'o': CardGoAgain,
}

func (c *Card) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*c = NormalizeCard(value)
	return nil
}

// NormalizeCard uppercases a card name and maps the app's lowercase yellow
// alias to YELLOW.
func NormalizeCard(value string) Card {
	card := Card(strings.ToUpper(strings.TrimSpace(value)))
	if card == "YELLOWRND" {
		return CardYellowRN
	}
	return card
}

// Valid reports whether c is a known card.
func (c Card) Valid() bool {
	switch c {
	case CardGreen, CardRed, CardSticky, CardDoubleUp, CardReset,
		CardYellowP1, CardYellowP2, CardYellowP3, CardYellowM1, CardYellowM2,
		CardYellow, CardYellowUK, CardYellowRN, CardFreeze, CardArctic,
		CardStun, CardDice, CardPink, CardGoAgain:
		return true
	}
	return false
}

// Label is the short name shown in the app.
func (c Card) Label() string {
	switch c {
	case CardGreen:
		return "Green card"
	case CardRed:
		return "Red card"
	case CardFreeze:
		return "Freeze card"
	case CardArctic:
		return "Arctic card"
	case CardReset:
		return "Reset card"
	case CardSticky:
		return "Sticky card"
	case CardStun:
		return "Stun card"
	case CardDoubleUp:
		return "Double up card"
	case CardDice:
		return "Dice card"
	case CardPink:
		return "Pink card"
	case CardGoAgain:
		return "Go again card"
	case CardYellowP1:
		return "Yellow card adding a red"
	case CardYellowP2:
		return "Yellow card adding 2 reds"
	case CardYellowP3:
		return "Yellow card adding 3 reds"
	case CardYellowM1:
		return "Yellow card removing a red"
	case CardYellowM2:
		return "Yellow card removing 2 reds"
	case CardYellow, CardYellowUK, CardYellowRN:
		return "Yellow card"
	}
	return string(c)
}

// Short is the compact card name used in summaries.
func (c Card) Short() string {
	switch c {
	case CardGreen:
		return "green"
	case CardRed:
		return "red"
	case CardFreeze:
		return "freeze"
	case CardArctic:
		return "arctic"
	case CardReset:
		return "reset"
	case CardSticky:
		return "sticky"
	case CardStun:
		return "stun"
	case CardDoubleUp:
		return "double up"
	case CardDice:
		return "dice"
	case CardPink:
		return "pink"
	case CardGoAgain:
		return "go again"
	case CardYellowP1:
		return "+1 red"
	case CardYellowP2:
		return "+2 red"
	case CardYellowP3:
		return "+3 red"
	case CardYellowM1:
		return "-1 red"
	case CardYellowM2:
		return "-2 red"
	case CardYellow, CardYellowUK, CardYellowRN:
		return "yellow"
	}
	return strings.ToLower(c.Label())
}

// Description is the app's one-sentence explanation of a card.
func (c Card) Description() string {
	switch c {
	case CardGreen:
		return "a green card."
	case CardRed:
		return "a red card"
	case CardFreeze:
		return "a freeze card. Wait until the freeze is over."
	case CardArctic:
		return "a sticky freeze card. Wait until the freeze is over. The card goes back to the deck."
	case CardReset:
		return "a reset card. Your deck was reset."
	case CardSticky:
		return "a sticky card. It goes back to the deck."
	case CardStun:
		return "a stinging freeze card. Picking it loses all accumulated chances."
	case CardDice:
		return "a dice throwing card."
	case CardDoubleUp:
		return "a double up card. Your reds and yellows were doubled."
	case CardYellowM1:
		return "a yellow card that removed one red from your deck."
	case CardYellowM2:
		return "a yellow card that removed two reds from your deck."
	case CardYellowP1:
		return "a yellow card that added one red to your deck."
	case CardYellowP2:
		return "a yellow card that added two reds to your deck."
	case CardYellowP3:
		return "a yellow card that added three reds to your deck."
	case CardYellow, CardYellowUK, CardYellowRN:
		return "a yellow card."
	case CardPink:
		return "a pink card that updates the lock in mysterious ways."
	case CardGoAgain:
		return "a go again card."
	}
	return "an unknown card."
}

// Letter returns the compact draw letter for a card.
func (c Card) Letter() string { return cardLetters[c] }

// CardForLetter maps a draw letter to a card.
func CardForLetter(letter byte) (Card, bool) {
	card, ok := letterCards[letter]
	return card, ok
}

// Deck is the remaining cards in a card lock.
type Deck map[Card]int

func (d *Deck) UnmarshalJSON(data []byte) error {
	text := bytes.TrimSpace(data)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		*d = nil
		return nil
	}
	var raw map[string]int
	if err := json.Unmarshal(text, &raw); err != nil {
		return err
	}
	out := make(Deck, len(raw))
	for name, count := range raw {
		out[NormalizeCard(name)] += count
	}
	*d = out
	return nil
}

func (d Deck) MarshalJSON() ([]byte, error) {
	if d == nil {
		return []byte("null"), nil
	}
	raw := make(map[string]int, len(d))
	for card, count := range d {
		raw[string(card)] = count
	}
	return json.Marshal(raw)
}

// Count returns the number of a named card.
func (d Deck) Count(card Card) int { return d[card] }

// Has reports whether the deck contains at least one of a card.
func (d Deck) Has(card Card) bool { return d[card] > 0 }

// Total is the number of cards left.
func (d Deck) Total() int {
	total := 0
	for _, count := range d {
		total += count
	}
	return total
}

// Greens is the number of green cards left.
func (d Deck) Greens() int { return d.Count(CardGreen) }

// Reds is the number of red cards left.
func (d Deck) Reds() int { return d.Count(CardRed) }

// Yellows is the number of every yellow variant left.
func (d Deck) Yellows() int {
	return d.Count(CardYellow) + d.Count(CardYellowUK) + d.Count(CardYellowRN) +
		d.Count(CardYellowP1) + d.Count(CardYellowP2) + d.Count(CardYellowP3) +
		d.Count(CardYellowM1) + d.Count(CardYellowM2)
}

// Clone copies a deck.
func (d Deck) Clone() Deck {
	if d == nil {
		return nil
	}
	out := make(Deck, len(d))
	for card, count := range d {
		out[card] = count
	}
	return out
}

// Draws is the compact sequence of cards already drawn, for example "gfr".
type Draws string

// Cards expands the draw letters into cards.
func (d Draws) Cards() []Card {
	out := make([]Card, 0, len(d))
	for i := 0; i < len(d); i++ {
		if card, ok := CardForLetter(d[i]); ok {
			out = append(out, card)
		}
	}
	return out
}

// Count returns how many times a card appears in the draws.
func (d Draws) Count(card Card) int {
	count := 0
	letter := card.Letter()
	if letter == "" {
		return 0
	}
	for i := 0; i < len(d); i++ {
		if string(d[i]) == letter {
			count++
		}
	}
	return count
}
