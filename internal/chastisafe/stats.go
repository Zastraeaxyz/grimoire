package chastisafe

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// StatsCriterion is a leaderboard sort criterion.
type StatsCriterion string

const (
	StatsChastity StatsCriterion = "Chastity"
	StatsTask     StatsCriterion = "Task"
	StatsBondage  StatsCriterion = "Bondage"
	StatsLoyalty  StatsCriterion = "Loyalty"
	StatsTotal    StatsCriterion = "Total"
)

// Valid reports whether c is a known leaderboard criterion.
func (c StatsCriterion) Valid() bool {
	switch c {
	case StatsChastity, StatsTask, StatsBondage, StatsLoyalty, StatsTotal:
		return true
	}
	return false
}

func canonicalCriterion(value StatsCriterion) (StatsCriterion, bool) {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "chastity":
		return StatsChastity, true
	case "task":
		return StatsTask, true
	case "bondage":
		return StatsBondage, true
	case "loyalty":
		return StatsLoyalty, true
	case "total":
		return StatsTotal, true
	}
	return "", false
}

// LeaderboardEntry is one account row on the #stats leaderboard.
type LeaderboardEntry struct {
	Name          string `json:"Name"`
	ID            ID     `json:"Id"`
	Visible       bool   `json:"Visible"`
	ChastityLocks int    `json:"ChastityLocks"`
	BondageLocks  int    `json:"BondageLocks"`
	TaskLocks     int    `json:"TaskLocks"`
	LoyaltyLocks  int    `json:"LoyaltyLocks"`
	TotalLocks    int    `json:"TotalLocks"`
}

// Value returns the row's score for a criterion.
func (e LeaderboardEntry) Value(criterion StatsCriterion) int {
	switch criterion {
	case StatsChastity:
		return e.ChastityLocks
	case StatsTask:
		return e.TaskLocks
	case StatsBondage:
		return e.BondageLocks
	case StatsLoyalty:
		return e.LoyaltyLocks
	case StatsTotal:
		return e.TotalLocks
	default:
		return 0
	}
}

// Leaderboard returns the #stats leaderboard sorted by criterion, highest
// first. The server already sorts, but the client sorts again so callers can
// rely on the order.
func (c *Client) Leaderboard(ctx context.Context, criterion StatsCriterion) ([]LeaderboardEntry, error) {
	canonical, ok := canonicalCriterion(criterion)
	if !ok {
		return nil, fmt.Errorf("chastisafe: unknown stats criterion %q", criterion)
	}
	const op = "GET /stats/{criterion}"
	body, err := c.get(ctx, "/stats/"+url.PathEscape(string(canonical)))
	if err != nil {
		return nil, err
	}
	var out struct {
		Status Status             `json:"status"`
		Reason string             `json:"reason"`
		Users  []LeaderboardEntry `json:"users"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		return nil, newAPIError(op, 200, nil, body)
	}
	sort.SliceStable(out.Users, func(i, j int) bool {
		left := out.Users[i].Value(canonical)
		right := out.Users[j].Value(canonical)
		if left != right {
			return left > right
		}
		if out.Users[i].TotalLocks != out.Users[j].TotalLocks {
			return out.Users[i].TotalLocks > out.Users[j].TotalLocks
		}
		return out.Users[i].Name < out.Users[j].Name
	})
	return out.Users, nil
}
