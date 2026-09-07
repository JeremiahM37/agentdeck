package routines

import (
	"testing"
	"time"
)

// A routine dispatches real agents that can open pull requests, so a schedule
// that silently means something other than what was intended is worse than one
// that refuses to parse.

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestIntervalsRunFromNow(t *testing.T) {
	now := at("2026-09-07 14:30")
	for spec, want := range map[string]time.Time{
		"every 30m": at("2026-09-07 15:00"),
		"every 6h":  at("2026-09-07 20:30"),
		"every 2d":  at("2026-09-09 14:30"),
	} {
		got, err := ParseSchedule(spec, now)
		if err != nil {
			t.Errorf("%s: %v", spec, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%s: got %v want %v", spec, got, want)
		}
	}
}

func TestWallClockSchedules(t *testing.T) {
	cases := map[string]struct{ from, want string }{
		"later today":            {"2026-09-07 08:00", "2026-09-07 09:00"},
		"already passed today":   {"2026-09-07 10:00", "2026-09-08 09:00"},
		"exactly now rolls over": {"2026-09-07 09:00", "2026-09-08 09:00"},
	}
	for name, tc := range cases {
		got, err := ParseSchedule("daily at 09:00", at(tc.from))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !got.Equal(at(tc.want)) {
			t.Errorf("%s: got %v want %v", name, got, at(tc.want))
		}
	}
	// bare "daily" is midnight
	got, err := ParseSchedule("daily", at("2026-09-07 14:00"))
	if err != nil || !got.Equal(at("2026-09-08 00:00")) {
		t.Errorf("daily: %v %v", got, err)
	}
	// hourly is the top of the next hour, not "an hour from now"
	got, _ = ParseSchedule("hourly", at("2026-09-07 14:37"))
	if !got.Equal(at("2026-09-07 15:00")) {
		t.Errorf("hourly: %v", got)
	}
}

func TestWeeklyLandsOnTheNamedDay(t *testing.T) {
	// 2026-09-07 is a Monday
	if at("2026-09-07 12:00").Weekday() != time.Monday {
		t.Skip("calendar assumption changed")
	}
	got, err := ParseSchedule("weekly on wed at 08:30", at("2026-09-07 12:00"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Weekday() != time.Wednesday || got.Hour() != 8 || got.Minute() != 30 {
		t.Errorf("got %v", got)
	}
	// asking on the day itself, before the time, runs today
	sameDay, _ := ParseSchedule("weekly on mon at 18:00", at("2026-09-07 12:00"))
	if sameDay.Weekday() != time.Monday || sameDay.Day() != 7 {
		t.Errorf("same-day: %v", sameDay)
	}
	// and after the time, a week later
	nextWeek, _ := ParseSchedule("weekly on mon at 08:00", at("2026-09-07 12:00"))
	if nextWeek.Day() != 14 {
		t.Errorf("next week: %v", nextWeek)
	}
}

// A schedule it cannot read must be refused at the point of saving, not
// silently ignored or guessed at.
func TestUnreadableSchedulesAreRefused(t *testing.T) {
	for _, spec := range []string{
		"0 9 * * *",      // cron: deliberately not supported
		"every",          // no interval
		"every 3x",       // unknown unit
		"every 0h",       // zero
		"every -1h",      // negative
		"daily at 25:00", // not a time
		"daily at noon",
		"weekly on funday at 09:00",
		"whenever",
	} {
		if _, err := ParseSchedule(spec, time.Now()); err == nil {
			t.Errorf("%q should not have parsed", spec)
		}
		if err := Valid(spec); err == nil {
			t.Errorf("%q passed validation", spec)
		}
	}
}

// A routine dispatches real agents; "every 10s" is never what someone meant.
func TestVeryShortIntervalsAreRefused(t *testing.T) {
	for _, spec := range []string{"every 1m", "every 4m"} {
		if _, err := ParseSchedule(spec, time.Now()); err == nil {
			t.Errorf("%q should be refused as too frequent", spec)
		}
	}
	if _, err := ParseSchedule("every 5m", time.Now()); err != nil {
		t.Errorf("5m is the documented floor: %v", err)
	}
}

// No schedule at all is valid and means "only when I press the button".
func TestNoScheduleMeansManualOnly(t *testing.T) {
	if err := Valid(""); err != nil {
		t.Errorf("empty schedule should be allowed: %v", err)
	}
	if err := Valid("   "); err != nil {
		t.Errorf("blank schedule should be allowed: %v", err)
	}
	if _, err := ParseSchedule("", time.Now()); err == nil {
		t.Error("an empty schedule has no next run time")
	}
}
