package plan

import (
	"fmt"
	"time"
)

type Kind string

const (
	Day   Kind = "day"
	Week  Kind = "week"
	Month Kind = "month"
)

const dateLayout = "2006-01-02"

type Range struct {
	Start, End time.Time
}

func (k Kind) days() int {
	switch k {
	case Day:
		return 1
	case Week:
		return 7
	case Month:
		return 30
	}
	return 0
}

func Window(kind Kind, from time.Time) Range {
	n := kind.days()
	if n == 0 {
		n = 1
	}
	y, m, d := from.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, from.Location())
	return Range{Start: start, End: start.AddDate(0, 0, n-1)}
}

func ParseFrom(s string) (time.Time, error) {
	t, err := time.ParseInLocation(dateLayout, s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}
