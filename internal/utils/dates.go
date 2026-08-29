package utils

import (
	"fmt"
	"time"
)

// DateLayout is the canonical business-date format across the system.
// Valid time is date-granular (docs/ARCHITECTURE.md); this mirrors the
// resolver package's internal layout. Keep the two in sync.
const DateLayout = "2006-01-02"

// ParseDate parses a strict YYYY-MM-DD business date. No timezone magic:
// business dates are calendar dates, not instants.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

// TodayUTC returns today's date in YYYY-MM-DD, evaluated in UTC. All
// effective-dating decisions anchor to UTC to keep every replica consistent.
func TodayUTC() string {
	return time.Now().UTC().Format(DateLayout)
}
