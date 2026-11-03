package engine

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// CookieStateKey is the key of the cookies in the state of the workspace.
const CookieStateKey = "httpclient.cookies"

// State is the part of extension.State the cookies use (the same as variables.State).
type State interface {
	Get(key string) (json.RawMessage, bool)
	Set(key string, value any) error
}

// StoredCookie is a cookie as the jar keeps it and as the state stores it.
type StoredCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Domain is the host the cookie belongs to, without a leading dot.
	Domain string `json:"domain"`
	// HostOnly says that only Domain itself gets the cookie, not its subdomains.
	HostOnly bool   `json:"hostOnly,omitempty"`
	Path     string `json:"path"`
	// Expires is the zero time for a session cookie.
	Expires  time.Time `json:"expires,omitzero"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"httpOnly,omitempty"`
}

func (c StoredCookie) key() string { return c.Domain + "|" + c.Path + "|" + c.Name }

type storedJars struct {
	Jars map[string][]StoredCookie `json:"jars"`
}

// Jars keeps one cookie jar per environment ("" is the jar of requests sent without an environment), in the state of the
// workspace (SPEC §10.33 "The engine"). A jar is an http.CookieJar that remembers what it was given, because net/http/cookiejar
// cannot be listed or written out.
type Jars struct {
	state State
	now   func() time.Time

	mu   sync.Mutex
	jars map[string]*Jar
}

// NewJars makes the jars, kept in state (nil: the cookies live as long as the Jars).
func NewJars(state State) *Jars {
	return &Jars{state: state, now: time.Now, jars: map[string]*Jar{}}
}

// Jar returns the jar of the environment, loading it from the state the first time.
func (j *Jars) Jar(env string) *Jar {
	j.mu.Lock()
	defer j.mu.Unlock()
	if jar, ok := j.jars[env]; ok {
		return jar
	}
	jar := newJar(j.now)
	if j.state != nil {
		var s storedJars
		if raw, ok := j.state.Get(CookieStateKey); ok && json.Unmarshal(raw, &s) == nil {
			for _, c := range s.Jars[env] {
				jar.put(c)
			}
		}
	}
	jar.dirty = false
	j.jars[env] = jar
	return jar
}

// Save writes the jar of the environment to the state if it changed since it was loaded or saved.
func (j *Jars) Save(env string) error {
	jar := j.Jar(env)
	if !jar.takeDirty() {
		return nil
	}
	if j.state == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var s storedJars
	if raw, ok := j.state.Get(CookieStateKey); ok {
		if json.Unmarshal(raw, &s) != nil {
			s = storedJars{}
		}
	}
	if s.Jars == nil {
		s.Jars = map[string][]StoredCookie{}
	}
	list := jar.List()
	if len(list) == 0 {
		delete(s.Jars, env)
	} else {
		s.Jars[env] = list
	}
	if err := j.state.Set(CookieStateKey, s); err != nil {
		jar.markDirty()
		if _, ok := errors.FromError(err); ok {
			return err
		}
		return errors.NewErr("EngineCookiesSaveFailed", err).SetMetadata("environment", env)
	}
	return nil
}

// Jar is the cookie jar of one environment. It is safe for concurrent use.
type Jar struct {
	now func() time.Time

	mu      sync.Mutex
	inner   *cookiejar.Jar
	entries map[string]StoredCookie
	dirty   bool
}

func newJar(now func() time.Time) *Jar {
	inner, _ := cookiejar.New(nil) // New never fails without options
	return &Jar{now: now, inner: inner, entries: map[string]StoredCookie{}}
}

// SetCookies implements http.CookieJar.
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.inner.SetCookies(u, cookies)
	for _, c := range cookies {
		sc := StoredCookie{Name: c.Name, Value: c.Value, Path: c.Path, Secure: c.Secure, HTTPOnly: c.HttpOnly}
		if c.Domain != "" {
			sc.Domain = strings.ToLower(strings.TrimPrefix(c.Domain, "."))
		} else {
			sc.Domain, sc.HostOnly = strings.ToLower(u.Hostname()), true
		}
		if sc.Path == "" || sc.Path[0] != '/' {
			sc.Path = defaultPath(u.Path)
		}
		switch {
		case c.MaxAge < 0:
			sc.Expires = time.Unix(1, 0)
		case c.MaxAge > 0:
			sc.Expires = j.now().Add(time.Duration(c.MaxAge) * time.Second)
		case !c.Expires.IsZero():
			sc.Expires = c.Expires
		}
		if !sc.Expires.IsZero() && !sc.Expires.After(j.now()) {
			delete(j.entries, sc.key())
		} else {
			j.entries[sc.key()] = sc
		}
		j.dirty = true
	}
}

// Cookies implements http.CookieJar.
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.inner.Cookies(u)
}

// List returns the cookies that have not expired, sorted by domain, path and name.
func (j *Jar) List() []StoredCookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.listLocked()
}

func (j *Jar) listLocked() []StoredCookie {
	var out []StoredCookie
	for _, c := range j.entries {
		if c.Expires.IsZero() || c.Expires.After(j.now()) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if x.Domain != y.Domain {
			return x.Domain < y.Domain
		}
		if x.Path != y.Path {
			return x.Path < y.Path
		}
		return x.Name < y.Name
	})
	return out
}

// Delete removes the cookies of a domain (and of its subdomains, which a cookie set for the domain reaches); an empty domain
// removes every cookie.
func (j *Jar) Delete(domain string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	keep := map[string]StoredCookie{}
	for k, c := range j.entries {
		if domain != "" && (c.Domain == domain || (!c.HostOnly && strings.HasSuffix(domain, "."+c.Domain))) {
			continue
		}
		if domain != "" {
			keep[k] = c
		}
	}
	j.entries = keep
	j.rebuildLocked()
	j.dirty = true
}

// rebuildLocked makes the inner jar again from the entries.
func (j *Jar) rebuildLocked() {
	j.inner, _ = cookiejar.New(nil)
	for _, c := range j.listLocked() {
		j.replay(c)
	}
}

// put adds a stored cookie (from the state) to both the entries and the inner jar.
func (j *Jar) put(c StoredCookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !c.Expires.IsZero() && !c.Expires.After(j.now()) {
		return
	}
	j.entries[c.key()] = c
	j.replay(c)
}

func (j *Jar) replay(c StoredCookie) {
	scheme := "http"
	if c.Secure {
		scheme = "https"
	}
	ck := &http.Cookie{Name: c.Name, Value: c.Value, Path: c.Path, Expires: c.Expires, Secure: c.Secure, HttpOnly: c.HTTPOnly}
	if !c.HostOnly {
		ck.Domain = c.Domain
	}
	j.inner.SetCookies(&url.URL{Scheme: scheme, Host: c.Domain, Path: c.Path}, []*http.Cookie{ck})
}

func (j *Jar) takeDirty() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	d := j.dirty
	j.dirty = false
	return d
}

func (j *Jar) markDirty() {
	j.mu.Lock()
	j.dirty = true
	j.mu.Unlock()
}

// defaultPath is the default cookie path of RFC 6265 section 5.1.4.
func defaultPath(p string) string {
	if p == "" || p[0] != '/' {
		return "/"
	}
	i := strings.LastIndex(p, "/")
	if i == 0 {
		return "/"
	}
	return p[:i]
}
