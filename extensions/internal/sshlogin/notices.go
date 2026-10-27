package sshlogin

import (
	"net"
	"sync"
	"time"
)

// HostOf is the IP of a host:port address, or the address itself when it has no port.
func HostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// RefusalWords says a refusal to the user, for the notification "<Extension> refused a login from <ip>: <words>.".
func RefusalWords(r Refusal) string {
	switch r.Reason {
	case ReasonKeyNotListed:
		return "its key is not in the list (" + r.KeyFingerprint + ")"
	case ReasonKeyRefused:
		return "its key was refused"
	case ReasonPasswordRefused:
		return "it tried a password, which is never accepted"
	case ReasonMethodRefused:
		return "it tried a way of logging in that is not accepted (" + r.Method + ")"
	case ReasonRateLimited:
		return "too many attempts from this address failed; it is blocked for a minute"
	case ReasonChannelType:
		return "it asked for a channel of type " + r.Detail
	case ReasonRequest:
		return "it sent a request (" + r.Detail + ") that is never served"
	case ReasonNoChannel:
		return "it logged in and opened nothing"
	}
	return r.Reason
}

// NoticeThrottle is how often one address may raise a notification of a refused attempt.
const NoticeThrottle = time.Minute

// maxThrottled is how many addresses a Throttle remembers.
const maxThrottled = 256

// Throttle lets one notification through for each address a minute (SPEC §10.18 "As built (step 15.2)"): the extensions that
// listen use it for the notifications of refused attempts, so a scanner cannot flood the user. The block of an address is a
// notification of its own. It remembers at most 256 addresses. Its methods are safe for concurrent use.
type Throttle struct {
	mu   sync.Mutex
	last map[string]time.Time
}

// Notice tells whether a notification of the refusal should be raised at now, and counts it when it should. A connection that
// ended before it tried to log in (ReasonHandshakeFailed) is in the log and counts for the rate limit but is never a
// notification: it is also what a client does on its first use, while its user compares the fingerprint.
func (t *Throttle) Notice(r Refusal, now time.Time) bool {
	if r.Reason == ReasonHandshakeFailed {
		return false
	}
	key := r.Address
	if r.Reason == ReasonRateLimited {
		key += "|blocked"
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[string]time.Time{}
	}
	if last, ok := t.last[key]; ok && now.Sub(last) < NoticeThrottle {
		return false
	}
	if len(t.last) >= maxThrottled {
		for k, at := range t.last {
			if now.Sub(at) >= NoticeThrottle {
				delete(t.last, k)
			}
		}
		if len(t.last) >= maxThrottled {
			return false
		}
	}
	t.last[key] = now
	return true
}
