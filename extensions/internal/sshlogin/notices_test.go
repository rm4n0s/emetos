package sshlogin

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHostOf(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"192.0.2.7:4000", "192.0.2.7"},
		{"[2001:db8::1]:22", "2001:db8::1"},
		{"192.0.2.7", "192.0.2.7"},
		{"", ""},
	} {
		if got := HostOf(tc.in); got != tc.want {
			t.Errorf("HostOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRefusalWordsNameEveryReason(t *testing.T) {
	for _, tc := range []struct {
		r    Refusal
		part string
	}{
		{Refusal{Reason: ReasonKeyNotListed, KeyFingerprint: "SHA256:abc"}, "not in the list (SHA256:abc)"},
		{Refusal{Reason: ReasonKeyRefused}, "key was refused"},
		{Refusal{Reason: ReasonPasswordRefused}, "password"},
		{Refusal{Reason: ReasonMethodRefused, Method: "keyboard-interactive"}, "keyboard-interactive"},
		{Refusal{Reason: ReasonRateLimited}, "blocked"},
		{Refusal{Reason: ReasonChannelType, Detail: "session"}, "session"},
		{Refusal{Reason: ReasonRequest, Detail: "shell"}, "shell"},
		{Refusal{Reason: ReasonNoChannel}, "opened nothing"},
		{Refusal{Reason: "Other"}, "Other"},
	} {
		if got := RefusalWords(tc.r); !strings.Contains(got, tc.part) {
			t.Errorf("RefusalWords(%s) = %q, want it to contain %q", tc.r.Reason, got, tc.part)
		}
	}
}

func TestThrottleLetsOneNoticeThroughForEachAddressAMinute(t *testing.T) {
	var th Throttle
	t0 := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	r := Refusal{Address: "192.0.2.7", Reason: ReasonKeyNotListed}
	blocked := Refusal{Address: "192.0.2.7", Reason: ReasonRateLimited}
	steps := []struct {
		name string
		r    Refusal
		at   time.Duration
		want bool
	}{
		{"the first", r, 0, true},
		{"the same address 30 s later", r, 30 * time.Second, false},
		{"another address", Refusal{Address: "192.0.2.8", Reason: ReasonKeyNotListed}, 30 * time.Second, true},
		{"the block of the address is a notice of its own", blocked, 31 * time.Second, true},
		{"the block again", blocked, 40 * time.Second, false},
		{"a connection that never tried to log in", Refusal{Address: "192.0.2.9", Reason: ReasonHandshakeFailed}, 40 * time.Second, false},
		{"a minute later", r, time.Minute, true},
	}
	for _, s := range steps {
		if got := th.Notice(s.r, t0.Add(s.at)); got != s.want {
			t.Errorf("%s: Notice = %v, want %v", s.name, got, s.want)
		}
	}
}

func TestThrottleKeepsAtMostAFixedNumberOfAddresses(t *testing.T) {
	var th Throttle
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	for i := 0; i < maxThrottled; i++ {
		if !th.Notice(Refusal{Address: "10.0.0." + strconv.Itoa(i), Reason: ReasonKeyNotListed}, now) {
			t.Fatalf("address %d was throttled", i)
		}
	}
	// the table is full of addresses that raised a notice a moment ago: a new one raises nothing, memory stays bounded
	if th.Notice(Refusal{Address: "10.1.0.1", Reason: ReasonKeyNotListed}, now.Add(time.Second)) {
		t.Error("a new address got through a full table")
	}
	// a minute later the old entries are forgotten and the new address is let through
	if !th.Notice(Refusal{Address: "10.1.0.1", Reason: ReasonKeyNotListed}, now.Add(2*time.Minute)) {
		t.Error("the table kept entries that are older than a minute")
	}
}
