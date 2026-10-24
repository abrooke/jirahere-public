package plan

import (
	"testing"
	"time"
)

func date(t *testing.T, loc *time.Location, y int, m time.Month, d int) time.Time {
	t.Helper()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func TestWindow(t *testing.T) {
	utc := time.UTC
	tests := []struct {
		name     string
		kind     Kind
		from     time.Time
		wantEnd  time.Time
		wantDays int
	}{
		{"day", Day, date(t, utc, 2026, 10, 16), date(t, utc, 2026, 10, 16), 1},
		{"week", Week, date(t, utc, 2026, 10, 16), date(t, utc, 2026, 10, 22), 7},
		{"month", Month, date(t, utc, 2026, 10, 16), date(t, utc, 2026, 11, 14), 30},
		{"week crosses month", Week, date(t, utc, 2026, 10, 28), date(t, utc, 2026, 11, 3), 7},
		{"week crosses year", Week, date(t, utc, 2026, 12, 29), date(t, utc, 2027, 1, 4), 7},
		{"month crosses year", Month, date(t, utc, 2026, 12, 15), date(t, utc, 2027, 1, 13), 30},
		{"month crosses leap day", Month, date(t, utc, 2028, 2, 10), date(t, utc, 2028, 3, 10), 30},
		{"month non-leap february", Month, date(t, utc, 2027, 2, 10), date(t, utc, 2027, 3, 11), 30},
		{"time of day dropped", Week, time.Date(2026, 10, 16, 23, 59, 59, 5, utc), date(t, utc, 2026, 10, 22), 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Window(tt.kind, tt.from)
			wantStart := time.Date(tt.from.Year(), tt.from.Month(), tt.from.Day(), 0, 0, 0, 0, utc)
			if !got.Start.Equal(wantStart) {
				t.Errorf("Start = %v, want %v", got.Start, wantStart)
			}
			if !got.End.Equal(tt.wantEnd) {
				t.Errorf("End = %v, want %v", got.End, tt.wantEnd)
			}
			if n := int(got.End.Sub(got.Start).Hours()/24) + 1; n != tt.wantDays {
				t.Errorf("inclusive days = %d, want %d", n, tt.wantDays)
			}
		})
	}
}

func TestWindowUnknownKindIsOneDay(t *testing.T) {
	from := date(t, time.UTC, 2026, 10, 16)
	for _, k := range []Kind{"", Kind("year"), Kind("bogus")} {
		got := Window(k, from)
		if !got.Start.Equal(from) || !got.End.Equal(from) {
			t.Errorf("Window(%q) = %v..%v, want single day %v", k, got.Start, got.End, from)
		}
	}
}

func TestWindowDSTKeepsCalendarDates(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	got := Window(Week, time.Date(2026, 3, 5, 15, 0, 0, 0, loc))
	want := time.Date(2026, 3, 11, 0, 0, 0, 0, loc)
	if !got.End.Equal(want) || got.End.Hour() != 0 {
		t.Errorf("End = %v, want %v", got.End, want)
	}

	got = Window(Month, time.Date(2026, 10, 20, 9, 0, 0, 0, loc))
	want = time.Date(2026, 11, 18, 0, 0, 0, 0, loc)
	if !got.End.Equal(want) || got.End.Hour() != 0 {
		t.Errorf("End = %v, want %v", got.End, want)
	}
}

func TestParseFrom(t *testing.T) {
	good := []struct {
		in      string
		y, m, d int
	}{
		{"2026-10-16", 2026, 10, 16},
		{"2028-02-29", 2028, 2, 29},
		{"2026-12-31", 2026, 12, 31},
	}
	for _, g := range good {
		got, err := ParseFrom(g.in)
		if err != nil {
			t.Errorf("ParseFrom(%q) error: %v", g.in, err)
			continue
		}
		y, m, d := got.Date()
		if y != g.y || int(m) != g.m || d != g.d || got.Hour() != 0 || got.Location() != time.Local {
			t.Errorf("ParseFrom(%q) = %v", g.in, got)
		}
	}

	bad := []string{
		"", "2026-10-16T00:00:00Z", "2026-10-16 ", " 2026-10-16", "2026/10/16",
		"20261016", "16-10-2026", "2026-1-2", "2026-13-01", "2026-02-30",
		"2027-02-29", "2026-10-00", "abc", "2026-10-1x", "+2026-10-16",
		"-2026-10-16", "2026-01-2", "2026-1-02",
	}
	for _, in := range bad {
		if _, err := ParseFrom(in); err == nil {
			t.Errorf("ParseFrom(%q) succeeded, want error", in)
		}
	}
}
