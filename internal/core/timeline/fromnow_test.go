package timeline

import (
	"testing"
	"time"
)

// TestFromNow checks FromNow against VS Code's fromNow: the three cases of date.test.ts (appendAgoLabel, useFullTimeWords,
// disallowNow) and the rounding and the units of date.ts.
func TestFromNow(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	tests := []struct {
		name string
		t    time.Time
		o    FromNowOptions
		want string
	}{
		{"seconds", ago(35 * time.Second), FromNowOptions{}, "35 secs"},
		{"without ago", ago(35 * time.Second), FromNowOptions{Ago: false}, "35 secs"},
		{"with ago", ago(35 * time.Second), FromNowOptions{Ago: true}, "35 secs ago"},
		{"full words", ago(35 * time.Second), FromNowOptions{FullWords: true}, "35 seconds"},
		{"the future in full words", now.Add(4 * time.Hour), FromNowOptions{FullWords: true}, "in 4 hours"},
		{"now", ago(5 * time.Second), FromNowOptions{}, "now"},
		{"now, explicitly allowed", ago(5 * time.Second), FromNowOptions{DisallowNow: false}, "now"},
		{"now disallowed", ago(5 * time.Second), FromNowOptions{DisallowNow: true}, "5 secs"},
		{"30 seconds is not now", ago(30 * time.Second), FromNowOptions{}, "30 secs"},
		{"29.5 seconds rounds to 30", ago(29500 * time.Millisecond), FromNowOptions{}, "30 secs"},
		{"one minute", ago(time.Minute), FromNowOptions{}, "1 min"},
		{"90 seconds rounds up", ago(90 * time.Second), FromNowOptions{}, "2 mins"},
		{"one hour", ago(time.Hour), FromNowOptions{Ago: true}, "1 hr ago"},
		{"hours", ago(5 * time.Hour), FromNowOptions{}, "5 hrs"},
		{"one day", ago(24 * time.Hour), FromNowOptions{}, "1 day"},
		{"days", ago(3 * 24 * time.Hour), FromNowOptions{Ago: true}, "3 days ago"},
		{"one week", ago(7 * 24 * time.Hour), FromNowOptions{}, "1 wk"},
		{"weeks in full", ago(14 * 24 * time.Hour), FromNowOptions{FullWords: true}, "2 weeks"},
		{"a month of 30 days", ago(30 * 24 * time.Hour), FromNowOptions{}, "1 mo"},
		{"months", ago(90 * 24 * time.Hour), FromNowOptions{}, "3 mos"},
		{"a year of 365 days", ago(365 * 24 * time.Hour), FromNowOptions{}, "1 yr"},
		{"years", ago(3 * 365 * 24 * time.Hour), FromNowOptions{Ago: true}, "3 yrs ago"},
		{"the future is in", now.Add(2 * 24 * time.Hour), FromNowOptions{}, "in 2 days"},
		{"a little in the future is now", now.Add(10 * time.Second), FromNowOptions{}, "now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromNow(now, tt.t, tt.o); got != tt.want {
				t.Errorf("FromNow = %q, want %q", got, tt.want)
			}
		})
	}
}
