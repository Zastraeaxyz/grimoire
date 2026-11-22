package chastisafe

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// FormatDuration renders seconds as "2 days 3 hours 4 minutes". It never
// returns an empty string.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	days := total / 86400
	total -= days * 86400
	hours := total / 3600
	total -= hours * 3600
	minutes := total / 60
	seconds := total - minutes*60

	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, plural(int(days), "day"))
	}
	if hours > 0 {
		parts = append(parts, plural(int(hours), "hour"))
	}
	if minutes > 0 {
		parts = append(parts, plural(int(minutes), "minute"))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, plural(int(seconds), "second"))
	}
	return strings.Join(parts, " ")
}

// FormatMinutes renders a minute count as a duration. Card regularity,
// check-in periods, hygiene periods and auto-reset periods are minutes.
func FormatMinutes(minutes int) string {
	return FormatDuration(time.Duration(minutes) * time.Minute)
}

// FormatShortDuration is the compact form used in tables, for example
// "2d 3h 4m".
func FormatShortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	days := total / 86400
	total -= days * 86400
	hours := total / 3600
	total -= hours * 3600
	minutes := total / 60
	seconds := total - minutes*60

	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
}

func plural(value int, name string) string {
	if value == 1 {
		return fmt.Sprintf("1 %s", name)
	}
	return fmt.Sprintf("%d %ss", value, name)
}

// DeckSummary lists the remaining cards, for example "3 green, 1 red".
func DeckSummary(deck Deck) string {
	if len(deck) == 0 {
		return "no cards"
	}
	order := []Card{
		CardGreen, CardRed,
		CardYellowP1, CardYellowP2, CardYellowP3,
		CardYellowM1, CardYellowM2, CardYellow, CardYellowUK, CardYellowRN,
		CardFreeze, CardArctic, CardStun, CardSticky,
		CardDoubleUp, CardReset, CardDice, CardPink, CardGoAgain,
	}
	seen := make(map[Card]bool, len(deck))
	parts := make([]string, 0, len(deck))
	for _, card := range order {
		count := deck[card]
		seen[card] = true
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, card.Short()))
		}
	}
	rest := make([]string, 0)
	for card, count := range deck {
		if seen[card] || count <= 0 {
			continue
		}
		rest = append(rest, fmt.Sprintf("%d %s", count, card.Short()))
	}
	sort.Strings(rest)
	parts = append(parts, rest...)
	return strings.Join(parts, ", ")
}

// LockSummary is a one-line description of a loaded lock.
func LockSummary(lock *LoadedLock) string {
	if lock == nil {
		return ""
	}
	var b strings.Builder
	name := lock.LockName
	if name == "" {
		name = "lock"
	}
	fmt.Fprintf(&b, "%s", name)
	if lock.Keyholder != "" {
		if lock.IsSelfLock() {
			b.WriteString(" (self-locked)")
		} else {
			fmt.Fprintf(&b, " (held by %s)", lock.Keyholder)
		}
	}
	if lock.IsFrozen() {
		b.WriteString(": frozen")
	} else if lock.IsReady() {
		b.WriteString(": ready to unlock")
	} else if lock.IsTimerLock() {
		fmt.Fprintf(&b, ": %s remaining", FormatDuration(lock.Remaining()))
	} else if lock.IsCardLock() {
		fmt.Fprintf(&b, ": %d cards left", lock.CardsRemaining())
	}
	return b.String()
}
