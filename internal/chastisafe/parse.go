package chastisafe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var dayDurationPattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)d`)

// ParseDuration accepts Go durations plus a "d" day suffix, for example
// "90m", "2h30m" or "3d". A leading minus sign is allowed for removing time.
func ParseDuration(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("chastisafe: duration is empty")
	}

	sign := ""
	if strings.HasPrefix(value, "-") {
		sign = "-"
		value = strings.TrimPrefix(value, "-")
	}
	value = dayDurationPattern.ReplaceAllStringFunc(value, func(match string) string {
		number := strings.TrimSuffix(strings.TrimSuffix(match, "d"), "D")
		days, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return match
		}
		return strconv.FormatFloat(days*24, 'f', -1, 64) + "h"
	})

	duration, err := time.ParseDuration(sign + value)
	if err != nil {
		return 0, fmt.Errorf("chastisafe: %q is not a duration", raw)
	}
	return duration, nil
}

// ParseCards accepts "green:5,red:2" or "g:5,r:2" and returns positive counts.
func ParseCards(raw string) (map[Card]int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, fmt.Errorf("chastisafe: card list is empty")
	}

	cards := make(map[Card]int)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, countText, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("chastisafe: card %q must be name:count", part)
		}
		count, err := strconv.Atoi(strings.TrimSpace(countText))
		if err != nil || count <= 0 {
			return nil, fmt.Errorf("chastisafe: card %q has an invalid count", part)
		}

		card := NormalizeCard(name)
		if len(strings.TrimSpace(name)) == 1 {
			if letterCard, ok := CardForLetter(strings.ToLower(strings.TrimSpace(name))[0]); ok {
				card = letterCard
			}
		}
		if !card.Valid() {
			return nil, fmt.Errorf("chastisafe: unknown card %q", name)
		}
		cards[card] += count
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("chastisafe: card list is empty")
	}
	return cards, nil
}

// ParsePurposes accepts "chastity,task", "all", or a single purpose. An empty
// value returns zero, so callers can decide whether to apply a default.
func ParsePurposes(raw string) (PurposeMask, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	if strings.EqualFold(value, "all") {
		return PurposeMaskTest | PurposeMaskChastity | PurposeMaskBondage |
			PurposeMaskTask | PurposeMaskLoyalty | PurposeMaskEmergency, nil
	}

	var mask PurposeMask
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '|'
	}) {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "test":
			mask |= PurposeMaskTest
		case "chastity":
			mask |= PurposeMaskChastity
		case "bondage":
			mask |= PurposeMaskBondage
		case "task":
			mask |= PurposeMaskTask
		case "loyalty":
			mask |= PurposeMaskLoyalty
		case "emergency":
			mask |= PurposeMaskEmergency
		default:
			return 0, fmt.Errorf("chastisafe: unknown purpose %q", part)
		}
	}
	return mask, nil
}

// ParseTemplateType accepts "timer", "timedlock", "card" or "cardlock".
func ParseTemplateType(raw string) (TemplateType, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "timer", "timedlock", "timed":
		return TemplateTypeTimer, nil
	case "card", "cardlock":
		return TemplateTypeCard, nil
	default:
		return "", fmt.Errorf("chastisafe: template type must be timer or card")
	}
}

// ParsePurpose accepts one purpose name.
func ParsePurpose(raw string) (Purpose, error) {
	purpose := Purpose(strings.ToUpper(strings.TrimSpace(raw)))
	if !purpose.Valid() {
		return "", fmt.Errorf("chastisafe: unknown purpose %q", raw)
	}
	return purpose, nil
}

// FormatPurposes renders a mask for messages.
func FormatPurposes(mask PurposeMask) string {
	values := mask.Strings()
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}
