package engine

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// cookieServer sets the cookie named by /set?name=value and shows the cookies it receives.
func cookieServer(t *testing.T) (srvURL string) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/set":
			for k, v := range r.URL.Query() {
				http.SetCookie(w, &http.Cookie{Name: k, Value: v[0], Path: "/"})
			}
		case "/session":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1", Path: "/api", HttpOnly: true})
		case "/expire":
			http.SetCookie(w, &http.Cookie{Name: "a", Value: "", Path: "/", MaxAge: -1})
		case "/maxage":
			http.SetCookie(w, &http.Cookie{Name: "m", Value: "1", Path: "/", MaxAge: 3600})
		}
		var names []string
		for _, c := range r.Cookies() {
			names = append(names, c.Name+"="+c.Value)
		}
		_, _ = w.Write([]byte(strings.Join(names, ";")))
	})
	return srv.URL
}

func sendEnv(t *testing.T, e *Engine, env, target string) string {
	t.Helper()
	in := get(target)
	in.Environment = env
	return bodyString(t, send(t, e, in))
}

func TestCookiesAcrossRequestsAndEnvironments(t *testing.T) {
	base := cookieServer(t)
	e := newEngine(t, Config{})
	if got := sendEnv(t, e, "Dev", base+"/set?a=1&b=2"); got != "" {
		t.Fatalf("the first request sent %q", got)
	}
	got := sendEnv(t, e, "Dev", base+"/")
	if got != "a=1;b=2" && got != "b=2;a=1" {
		t.Fatalf("Dev sent %q", got)
	}
	if got := sendEnv(t, e, "Prod", base+"/"); got != "" {
		t.Fatalf("Prod sent %q, the jars are per environment", got)
	}
	if got := sendEnv(t, e, "", base+"/"); got != "" {
		t.Fatalf("no environment sent %q", got)
	}
	resp := send(t, e, func() Input { in := get(base + "/set?c=3"); in.Environment = "Prod"; return in }())
	if len(resp.Cookies) != 1 || resp.Cookies[0].Name != "c" {
		t.Fatalf("response cookies %v", resp.Cookies)
	}
	if got := sendEnv(t, e, "Prod", base+"/"); got != "c=3" {
		t.Fatalf("Prod sent %q", got)
	}
	// a user's Cookie header is kept, the jar's cookies are added
	in := get(base + "/")
	in.Environment = "Prod"
	in.Request.Headers = append(in.Request.Headers, pair("Cookie", "mine=1"))
	if got := bodyString(t, send(t, e, in)); got != "mine=1;c=3" {
		t.Fatalf("sent %q", got)
	}
}

func TestCookiesKeptInTheState(t *testing.T) {
	base := cookieServer(t)
	state := newState()
	first := newEngine(t, Config{Jars: NewJars(state)})
	sendEnv(t, first, "Dev", base+"/set?a=1")
	sendEnv(t, first, "Dev", base+"/session")
	sendEnv(t, first, "Dev", base+"/maxage")
	if _, ok := state.Get(CookieStateKey); !ok {
		t.Fatal("nothing in the state")
	}
	// a new engine, a new Jars, the same state: the editor was restarted
	second := newEngine(t, Config{Jars: NewJars(state)})
	got := sendEnv(t, second, "Dev", base+"/")
	if got != "a=1;m=1" && got != "m=1;a=1" {
		t.Fatalf("after the restart %q", got)
	}
	if got := sendEnv(t, second, "Dev", base+"/api/x"); !strings.Contains(got, "sid=s1") {
		t.Fatalf("the cookie of the path /api was not sent: %q", got)
	}
	list := second.Jars().Jar("Dev").List()
	if len(list) != 3 || list[0].Name != "a" || list[1].Name != "m" || list[1].Expires.IsZero() || !list[0].HostOnly || list[0].Domain != "127.0.0.1" {
		t.Fatalf("list %+v", list)
	}
	var sid StoredCookie
	for _, c := range list {
		if c.Name == "sid" {
			sid = c
		}
	}
	if !sid.HTTPOnly || sid.Path != "/api" {
		t.Fatalf("sid %+v", sid)
	}
}

func TestCookieExpiryAndDelete(t *testing.T) {
	base := cookieServer(t)
	state := newState()
	e := newEngine(t, Config{Jars: NewJars(state)})
	sendEnv(t, e, "Dev", base+"/set?a=1&b=2")
	sendEnv(t, e, "Dev", base+"/expire")
	if got := sendEnv(t, e, "Dev", base+"/"); got != "b=2" {
		t.Fatalf("after Max-Age=-1: %q", got)
	}
	jar := e.Jars().Jar("Dev")
	jar.Delete("example.test")
	if len(jar.List()) != 1 {
		t.Fatal("deleting another domain removed a cookie")
	}
	jar.Delete("127.0.0.1")
	if len(jar.List()) != 0 {
		t.Fatalf("list %+v", jar.List())
	}
	if err := e.Jars().Save("Dev"); err != nil {
		t.Fatal(err)
	}
	if got := sendEnv(t, e, "Dev", base+"/"); got != "" {
		t.Fatalf("after Delete: %q", got)
	}
	sendEnv(t, e, "Dev", base+"/set?z=9")
	jar.Delete("")
	if len(jar.List()) != 0 {
		t.Fatal("Delete of every cookie")
	}
}

func TestExpiredCookiesAreNotKept(t *testing.T) {
	now := time.Now()
	jars := NewJars(nil)
	jars.now = func() time.Time { return now }
	jar := jars.Jar("")
	u, _ := url.Parse("http://127.0.0.1/x/y")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "old", Value: "1", Expires: now.Add(-time.Hour)},
		{Name: "soon", Value: "1", Expires: now.Add(time.Minute)},
		{Name: "dom", Value: "1", Domain: ".example.test", Path: "/"},
	})
	if list := jar.List(); len(list) != 2 || list[0].Name != "soon" || list[0].Path != "/x" || list[1].Domain != "example.test" || list[1].HostOnly {
		t.Fatalf("list %+v", list)
	}
	now = now.Add(2 * time.Minute)
	if list := jar.List(); len(list) != 1 {
		t.Fatalf("after the time passed: %+v", list)
	}
}

func TestCookieSaveFailure(t *testing.T) {
	base := cookieServer(t)
	state := newState()
	state.err = errors.New("ExtensionStateInvalid", "full")
	e := newEngine(t, Config{Jars: NewJars(state)})
	resp := send(t, e, func() Input { in := get(base + "/set?a=1"); in.Environment = "Dev"; return in }())
	if len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "could not be kept") {
		t.Fatalf("warnings %v", resp.Warnings)
	}
	// the error of a state that is not from this module's errors package gets the tag of the engine
	state.err = errPlain("disk full")
	err := e.Jars().Save("Dev")
	wantRoute(t, err, "Jars.Save.EngineCookiesSaveFailed")
	// and the jar is saved again when the state works
	state.err = nil
	if err := e.Jars().Save("Dev"); err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Get(CookieStateKey); !ok {
		t.Fatal("not saved")
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }

func TestBrokenStateIsIgnored(t *testing.T) {
	state := newState()
	state.data[CookieStateKey] = []byte("{not json")
	jar := NewJars(state).Jar("Dev")
	if len(jar.List()) != 0 {
		t.Fatal("cookies from nothing")
	}
}
