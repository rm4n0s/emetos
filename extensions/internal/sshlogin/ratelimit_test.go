package sshlogin

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func TestLimiter(t *testing.T) {
	clock := newClock()
	l := NewLimiter(LimiterOptions{Now: clock.Now})
	const a = "10.0.0.1"
	for i := 1; i <= 4; i++ {
		if l.Fail(a) {
			t.Fatalf("blocked after %d failures", i)
		}
		if blocked, _ := l.Blocked(a); blocked {
			t.Fatalf("Blocked after %d failures", i)
		}
		clock.Advance(5 * time.Second)
	}
	if !l.Fail(a) {
		t.Fatal("not blocked after the fifth failure")
	}
	blocked, first := l.Blocked(a)
	if !blocked || !first {
		t.Fatalf("Blocked = %v, %v; want the first sight of a block", blocked, first)
	}
	if blocked, first := l.Blocked(a); !blocked || first {
		t.Fatalf("Blocked again = %v, %v; want blocked and not first", blocked, first)
	}
	if blocked, _ := l.Blocked("10.0.0.2"); blocked {
		t.Error("another address is blocked")
	}
	// the failures leave the window one by one: after the first one is 60 s old the address is free for one attempt
	clock.Advance(41 * time.Second) // the first failure is 61 s old, the second 56 s
	if blocked, _ := l.Blocked(a); blocked {
		t.Error("still blocked when the oldest failure left the window")
	}
	if !l.Fail(a) {
		t.Error("the new failure should block again: four were in the window")
	}
	if blocked, first := l.Blocked(a); !blocked || first {
		t.Errorf("Blocked = %v, %v; the same window is announced once", blocked, first)
	}
	clock.Advance(2 * time.Minute)
	if blocked, _ := l.Blocked(a); blocked || l.Len() != 0 {
		t.Errorf("blocked after two quiet minutes = %v, kept %d addresses", blocked, l.Len())
	}
}

func TestLimiterIsBounded(t *testing.T) {
	clock := newClock()
	l := NewLimiter(LimiterOptions{Now: clock.Now, MaxAddresses: 10})
	for i := 0; i < 1000; i++ {
		l.Fail("10.0." + strconv.Itoa(i/250) + "." + strconv.Itoa(i%250))
		clock.Advance(time.Millisecond)
		if l.Len() > 10 {
			t.Fatalf("the limiter keeps %d addresses, the limit is 10", l.Len())
		}
	}
	// the newest addresses are the ones kept
	if l.Fail("10.9.9.9"); l.Len() > 10 {
		t.Errorf("kept %d", l.Len())
	}
	// expired addresses make room before live ones are dropped
	clock.Advance(2 * time.Minute)
	l.Fail("10.8.8.8")
	if l.Len() != 1 {
		t.Errorf("kept %d addresses after they expired, want 1", l.Len())
	}
	// a long run of failures of one address keeps no more than the limit of times
	for i := 0; i < 100; i++ {
		l.Fail("10.7.7.7")
	}
	if n := len(l.byAddress["10.7.7.7"].times); n != 5 {
		t.Errorf("kept %d times of one address, want 5", n)
	}
}

func TestAddressKey(t *testing.T) {
	tests := []struct {
		addr net.Addr
		want string
	}{
		{&net.TCPAddr{IP: net.ParseIP("192.168.1.5"), Port: 4000}, "192.168.1.5"},
		{&net.TCPAddr{IP: net.ParseIP("::ffff:192.168.1.5"), Port: 1}, "192.168.1.5"},
		{&net.TCPAddr{IP: net.ParseIP("2001:db8:1:2:aaaa:bbbb:cccc:dddd"), Port: 1}, "2001:db8:1:2::/64"},
		{&net.TCPAddr{IP: net.ParseIP("2001:db8:1:2:1:2:3:4"), Port: 9}, "2001:db8:1:2::/64"},
		{&net.UnixAddr{Name: "/tmp/x", Net: "unix"}, "/tmp/x"},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := AddressKey(tt.addr); got != tt.want {
			t.Errorf("AddressKey(%v) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

// TestRateLimitOnTheServer: five failed attempts in a minute close the address's connections before the handshake, and a
// minute later it logs in again.
func TestRateLimitOnTheServer(t *testing.T) {
	r := newRig(t)
	listed, stranger := newKey(t), newKey(t)
	r.allow(listed.allowed("B"))
	r.pin()
	for i := 0; i < 5; i++ {
		if _, err := r.dial(SignerAuth(stranger.signer)); err == nil {
			t.Fatal("a stranger logged in")
		}
		clock := r.clock
		clock.Advance(time.Second)
	}
	// even the listed key is turned away now, without a handshake
	_, err := r.dial(SignerAuth(listed.signer))
	wantRoute(t, err, "dial.SSHLoginConnectFailed")
	var limited []Refusal
drain:
	for {
		select {
		case f := <-r.refusals:
			if f.Reason == ReasonRateLimited {
				limited = append(limited, f)
			}
		default:
			break drain
		}
	}
	if len(limited) != 1 {
		t.Errorf("the block was reported %d times, want once: %+v", len(limited), limited)
	}
	// two more refused connections are not reported again
	for i := 0; i < 2; i++ {
		_, _ = r.dial(SignerAuth(listed.signer))
	}
	select {
	case f := <-r.refusals:
		t.Errorf("reported again: %+v", f)
	case <-time.After(200 * time.Millisecond):
	}
	r.clock.Advance(time.Minute)
	c, err := r.dial(SignerAuth(listed.signer))
	if err != nil {
		t.Fatalf("after the minute: %v", err)
	}
	_ = c.Close()
}

func TestRefusalReasons(t *testing.T) {
	notListed := errors.New("SSHLoginKeyNotListed", "x")
	other := errors.New("Other", "y")
	tests := []struct {
		method string
		err    error
		want   string
	}{
		{"publickey", notListed, ReasonKeyNotListed},
		{"publickey", other, ReasonKeyRefused},
		{"password", other, ReasonPasswordRefused},
		{"keyboard-interactive", other, ReasonMethodRefused},
		{"hostbased", other, ReasonMethodRefused},
	}
	for _, tt := range tests {
		if got := refusalReason(tt.method, tt.err); got != tt.want {
			t.Errorf("refusalReason(%s) = %s, want %s", tt.method, got, tt.want)
		}
	}
}
