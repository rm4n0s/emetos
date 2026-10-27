package sshlogin

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// Limiter counts the failed login attempts of each address in a sliding window and says when an address has had too many
// (5 a minute by default). It is bounded: it keeps at most MaxAddresses addresses, each with at most Failures times, and
// forgets an address when its last failure is older than the window. An IPv6 address counts as its /64. Its clock is given,
// so a test moves time by hand. It is safe for concurrent use.
type Limiter struct {
	mu        sync.Mutex
	now       func() time.Time
	failures  int
	window    time.Duration
	maxAddrs  int
	byAddress map[string]*addressFailures
}

// addressFailures are the failures of one address inside the window, oldest first, and when the block was last announced.
type addressFailures struct {
	times     []time.Time
	announced time.Time
}

// LimiterOptions are the options of NewLimiter; a zero field takes its default.
type LimiterOptions struct {
	// Failures is how many failed attempts in the Window block an address (5).
	Failures int
	// Window is how long a failure counts (one minute).
	Window time.Duration
	// MaxAddresses bounds the memory: at most this many addresses are kept, and the one whose last failure is oldest is
	// forgotten for a new one (4096).
	MaxAddresses int
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// NewLimiter makes a limiter.
func NewLimiter(opts LimiterOptions) *Limiter {
	l := &Limiter{now: opts.Now, failures: opts.Failures, window: opts.Window, maxAddrs: opts.MaxAddresses, byAddress: map[string]*addressFailures{}}
	if l.now == nil {
		l.now = time.Now
	}
	if l.failures <= 0 {
		l.failures = 5
	}
	if l.window <= 0 {
		l.window = time.Minute
	}
	if l.maxAddrs <= 0 {
		l.maxAddrs = 4096
	}
	return l
}

// AddressKey is the key the limiter counts an address under: the IP of a network address without its port, an IPv6 address
// cut to its /64, an IPv4-mapped IPv6 address as the IPv4 one. An address that is not an IP is used as it is.
func AddressKey(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host := addr.String()
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	ip = ip.WithZone("").Unmap()
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

// Blocked tells whether the address has had Failures failed attempts in the last Window. first is true for the first
// call that sees a block in a window, so that the caller logs and announces it once and not at every refused connection.
func (l *Limiter) Blocked(key string) (blocked, first bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.prune(key)
	if f == nil || len(f.times) < l.failures {
		return false, false
	}
	now := l.now()
	if f.announced.IsZero() || now.Sub(f.announced) >= l.window {
		f.announced = now
		return true, true
	}
	return true, false
}

// Fail records a failed attempt of the address and tells whether the address is blocked now.
func (l *Limiter) Fail(key string) (blocked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.prune(key)
	if f == nil {
		if len(l.byAddress) >= l.maxAddrs {
			l.evict()
		}
		f = &addressFailures{}
		l.byAddress[key] = f
	}
	f.times = append(f.times, l.now())
	if len(f.times) > l.failures {
		f.times = f.times[len(f.times)-l.failures:]
	}
	return len(f.times) >= l.failures
}

// Len is the number of addresses the limiter keeps.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.byAddress)
}

// prune drops the failures of the address that left the window, and the address when none is left. The caller holds mu.
func (l *Limiter) prune(key string) *addressFailures {
	f := l.byAddress[key]
	if f == nil {
		return nil
	}
	cut := l.now().Add(-l.window)
	i := 0
	for i < len(f.times) && !f.times[i].After(cut) {
		i++
	}
	f.times = f.times[i:]
	if len(f.times) == 0 {
		delete(l.byAddress, key)
		return nil
	}
	return f
}

// evict makes room: it drops the addresses whose failures all left the window and, when none did, the address whose last
// failure is the oldest. The caller holds mu.
func (l *Limiter) evict() {
	cut := l.now().Add(-l.window)
	oldestKey := ""
	var oldest time.Time
	for k, f := range l.byAddress {
		last := f.times[len(f.times)-1]
		if !last.After(cut) {
			delete(l.byAddress, k)
			continue
		}
		if oldestKey == "" || last.Before(oldest) {
			oldestKey, oldest = k, last
		}
	}
	if len(l.byAddress) >= l.maxAddrs && oldestKey != "" {
		delete(l.byAddress, oldestKey)
	}
}
