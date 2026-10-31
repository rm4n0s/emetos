package timeline

import (
	"math"
	"strconv"
	"time"
)

// FromNowOptions are the options of FromNow (VS Code's appendAgoLabel, useFullTimeWords and disallowNow).
type FromNowOptions struct {
	// Ago appends " ago" ("3 mins ago").
	Ago bool
	// FullWords writes "minutes" in place of "mins".
	FullWords bool
	// DisallowNow writes "5 secs" in place of "now" for a time less than 30 seconds back.
	DisallowNow bool
}

const (
	minute = 60
	hour   = minute * 60
	day    = hour * 24
	week   = day * 7
	month  = day * 30
	year   = day * 365
)

// jsRound is JavaScript's Math.round: halves go up, toward positive infinity.
func jsRound(x float64) int { return int(math.Floor(x + 0.5)) }

// FromNow writes how long before now a time is, as the Timeline view writes the age of an item ("35 secs", "3 mins", "2 days",
// "1 yr"; "now" for less than 30 seconds, "in 4 hours" for the future). The text is English.
//
// Ported from VS Code: src/vs/base/common/date.ts (fromNow)
func FromNow(now, t time.Time, o FromNowOptions) string {
	seconds := jsRound(float64(now.Sub(t).Milliseconds()) / 1000)
	if seconds < -30 {
		return "in " + FromNow(now, now.Add(time.Duration(seconds)*time.Second), FromNowOptions{FullWords: o.FullWords, DisallowNow: o.DisallowNow})
	}
	if !o.DisallowNow && seconds < 30 {
		return "now"
	}
	var value int
	var short, full string
	switch {
	case seconds < minute:
		value, short, full = seconds, "sec", "second"
	case seconds < hour:
		value, short, full = jsRound(float64(seconds)/minute), "min", "minute"
	case seconds < day:
		value, short, full = jsRound(float64(seconds)/hour), "hr", "hour"
	case seconds < week:
		value, short, full = jsRound(float64(seconds)/day), "day", "day"
	case seconds < month:
		value, short, full = jsRound(float64(seconds)/week), "wk", "week"
	case seconds < year:
		value, short, full = jsRound(float64(seconds)/month), "mo", "month"
	default:
		value, short, full = jsRound(float64(seconds)/year), "yr", "year"
	}
	unit := short
	if o.FullWords {
		unit = full
	}
	if value != 1 {
		unit += "s"
	}
	text := strconv.Itoa(value) + " " + unit
	if o.Ago {
		text += " ago"
	}
	return text
}
