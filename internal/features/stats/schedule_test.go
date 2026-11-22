package stats

import (
	"testing"
	"time"
)

func TestParseClockTimes(t *testing.T) {
	times, err := parseClockTimes([]string{"09:00", "21:30"})
	if err != nil {
		t.Fatalf("parseClockTimes: %v", err)
	}
	if len(times) != 2 || times[0].hour != 9 || times[1].hour != 21 || times[1].minute != 30 {
		t.Fatalf("times = %+v", times)
	}

	if _, err := parseClockTimes([]string{"9am"}); err == nil {
		t.Fatal("expected an error for a non HH:MM time")
	}
}

func TestNextRunUTC(t *testing.T) {
	at := []clockTime{{hour: 9}, {hour: 21}}

	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before the first run",
			now:  time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC),
			want: time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC),
		},
		{
			name: "between runs",
			now:  time.Date(2026, 11, 22, 9, 30, 0, 0, time.UTC),
			want: time.Date(2026, 11, 22, 21, 0, 0, 0, time.UTC),
		},
		{
			name: "after the last run",
			now:  time.Date(2026, 11, 22, 21, 30, 0, 0, time.UTC),
			want: time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC),
		},
		{
			name: "exactly on a run rolls forward",
			now:  time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, 11, 22, 21, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextRun(tc.now, at, time.UTC); !got.Equal(tc.want) {
				t.Fatalf("nextRun = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextRunUsesTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}

	// 07:30 UTC is 08:30 in Berlin, so the next 09:00 run is 08:00 UTC.
	now := time.Date(2026, 11, 22, 7, 30, 0, 0, time.UTC)
	want := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	got := nextRun(now, []clockTime{{hour: 9}, {hour: 21}}, loc)
	if !got.Equal(want) {
		t.Fatalf("nextRun = %v, want %v", got, want)
	}
}

func TestNextRunWithoutTimes(t *testing.T) {
	if got := nextRun(time.Now(), nil, time.UTC); !got.IsZero() {
		t.Fatalf("nextRun = %v, want zero", got)
	}
}
