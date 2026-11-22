package stats

import (
	"fmt"
	"time"
)

// clockTime is a wall-clock time of day in the report timezone.
type clockTime struct {
	hour   int
	minute int
}

func parseClockTimes(raw []string) ([]clockTime, error) {
	times := make([]clockTime, 0, len(raw))
	for _, value := range raw {
		parsed, err := time.Parse("15:04", value)
		if err != nil {
			return nil, fmt.Errorf("stats time %q is not HH:MM", value)
		}
		times = append(times, clockTime{hour: parsed.Hour(), minute: parsed.Minute()})
	}
	return times, nil
}

// nextRun returns the first configured time strictly after now, walking at
// most a week ahead so a time that DST skips still resolves.
func nextRun(now time.Time, times []clockTime, loc *time.Location) time.Time {
	if len(times) == 0 {
		return time.Time{}
	}

	local := now.In(loc)
	for day := 0; day <= 7; day++ {
		date := time.Date(local.Year(), local.Month(), local.Day()+day, 0, 0, 0, 0, loc)
		for _, at := range times {
			candidate := time.Date(date.Year(), date.Month(), date.Day(), at.hour, at.minute, 0, 0, loc)
			if candidate.After(now) {
				return candidate
			}
		}
	}
	return now.Add(24 * time.Hour)
}
