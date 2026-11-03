package engine

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// The examples of RFC 7616 section 3.9.1: the user Mufasa, the password "Circle of Life".
func TestDigestRFC7616Vectors(t *testing.T) {
	const nonce = "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v"
	const opaque = "FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"
	const cnonce = "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ"
	tests := []struct {
		algorithm string
		want      string
	}{
		{"MD5", "8ca523f5e9506fed4657c9700eebdbec"},
		{"SHA-256", "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1"},
	}
	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			ch := digestChallenge{realm: "http-auth@example.org", nonce: nonce, opaque: opaque, algorithm: tt.algorithm, qopAuth: true}
			got := digestAuthorization(ch, "Mufasa", "Circle of Life", "GET", "/dir/index.html", cnonce)
			if !strings.Contains(got, `response="`+tt.want+`"`) {
				t.Fatalf("Authorization %s\nwant response %s", got, tt.want)
			}
			for _, part := range []string{`username="Mufasa"`, `uri="/dir/index.html"`, "qop=auth", "nc=00000001", `opaque="` + opaque + `"`} {
				if !strings.Contains(got, part) {
					t.Errorf("%s lacks %s", got, part)
				}
			}
		})
	}
}

func TestParseDigest(t *testing.T) {
	tests := []struct {
		name      string
		headers   []string
		wantFound bool
		wantAlg   string
		wantQop   bool
		wantRoute string
	}{
		{name: "none", headers: []string{`Basic realm="x"`}},
		{name: "md5", headers: []string{`Digest realm="r", nonce="n1", qop="auth, auth-int", opaque="o"`}, wantFound: true, wantAlg: "MD5", wantQop: true},
		{name: "sha-256 is preferred", headers: []string{`Digest realm="r", nonce="n1", algorithm=MD5, qop="auth"`, `Digest realm="r", nonce="n2", algorithm=SHA-256, qop="auth"`}, wantFound: true, wantAlg: "SHA-256", wantQop: true},
		{name: "several schemes in one header", headers: []string{`Basic realm="b", Digest realm="r", nonce="n", algorithm=SHA-256-sess, qop=auth`}, wantFound: true, wantAlg: "SHA-256-SESS", wantQop: true},
		{name: "no qop", headers: []string{`Digest realm="r", nonce="n"`}, wantFound: true, wantAlg: "MD5"},
		{name: "unsupported algorithm", headers: []string{`Digest realm="r", nonce="n", algorithm=SHA-512-256`}, wantFound: true, wantRoute: "parseDigest.EngineDigestUnsupported"},
		{name: "userhash", headers: []string{`Digest realm="r", nonce="n", userhash=true`}, wantFound: true, wantRoute: "parseDigest.EngineDigestUnsupported"},
		{name: "no nonce", headers: []string{`Digest realm="r"`}, wantFound: true, wantRoute: "parseDigest.EngineDigestChallengeInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, found, err := parseDigest(tt.headers)
			if found != tt.wantFound {
				t.Fatalf("found = %v", found)
			}
			if tt.wantRoute != "" {
				wantRoute(t, err, tt.wantRoute)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if found && (ch.algorithm != tt.wantAlg || ch.qopAuth != tt.wantQop) {
				t.Fatalf("challenge %+v", ch)
			}
		})
	}
}

// digestServer is a server that wants Digest and checks the answer with its own arithmetic.
func digestServer(t *testing.T, algorithm string, qop bool, user, pass string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var challenges atomic.Int32
	h := func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Digest ") {
			challenges.Add(1)
			c := `Digest realm="test", nonce="abc123", opaque="op", algorithm=` + algorithm
			if qop {
				c += `, qop="auth"`
			}
			w.Header().Set("WWW-Authenticate", c)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := map[string]string{}
		for _, kv := range strings.Split(strings.TrimPrefix(auth, "Digest "), ", ") {
			k, v, _ := strings.Cut(kv, "=")
			p[k] = strings.Trim(v, `"`)
		}
		hash := func(s string) string {
			if algorithm == "SHA-256" {
				sum := sha256.Sum256([]byte(s))
				return hex.EncodeToString(sum[:])
			}
			sum := md5.Sum([]byte(s))
			return hex.EncodeToString(sum[:])
		}
		ha1 := hash(fmt.Sprintf("%s:%s:%s", user, "test", pass))
		ha2 := hash(r.Method + ":" + r.URL.RequestURI())
		want := hash(ha1 + ":abc123:" + ha2)
		if qop {
			want = hash(fmt.Sprintf("%s:abc123:%s:%s:auth:%s", ha1, p["nc"], p["cnonce"], ha2))
		}
		if p["username"] != user || p["response"] != want || p["uri"] != r.URL.RequestURI() || p["opaque"] != "op" {
			w.Header().Set("WWW-Authenticate", `Digest realm="test", nonce="abc123"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("welcome " + p["username"]))
	}
	return newServer(t, h), &challenges
}

func TestDigestAuth(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		qop       bool
		password  string
		wantCode  int
		wantChain int
	}{
		{name: "md5 with qop", algorithm: "MD5", qop: true, password: "pw", wantCode: 200, wantChain: 1},
		{name: "sha-256 with qop", algorithm: "SHA-256", qop: true, password: "pw", wantCode: 200, wantChain: 1},
		{name: "md5 without qop", algorithm: "MD5", password: "pw", wantCode: 200, wantChain: 1},
		{name: "wrong password", algorithm: "MD5", qop: true, password: "nope", wantCode: 401, wantChain: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, challenges := digestServer(t, tt.algorithm, tt.qop, "ada", "pw")
			in := Input{Request: format.Request{Method: "GET", URL: srv.URL + "/secret/area?x=1", Auth: &format.Auth{Type: format.AuthDigest, Username: "ada", Password: tt.password}}}
			resp := send(t, newEngine(t, Config{}), in)
			if resp.StatusCode != tt.wantCode || len(resp.Chain) != tt.wantChain {
				t.Fatalf("status %d chain %d", resp.StatusCode, len(resp.Chain))
			}
			if resp.Chain[0].Reason != HopDigest || resp.Chain[0].StatusCode != 401 {
				t.Fatalf("hop %+v", resp.Chain[0])
			}
			if tt.wantCode == 200 && bodyString(t, resp) != "welcome ada" {
				t.Fatalf("body %q", bodyString(t, resp))
			}
			if n := challenges.Load(); n != 1 {
				t.Fatalf("%d challenges, want exactly one round trip", n)
			}
		})
	}
}

func TestDigestIsAnsweredOnlyOnce(t *testing.T) {
	var calls atomic.Int32
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("WWW-Authenticate", `Digest realm="r", nonce="n"`)
		w.WriteHeader(401)
	})
	in := Input{Request: format.Request{Method: "GET", URL: srv.URL, Auth: &format.Auth{Type: format.AuthDigest, Username: "u", Password: "p"}}}
	resp := send(t, newEngine(t, Config{}), in)
	if resp.StatusCode != 401 || calls.Load() != 2 {
		t.Fatalf("status %d after %d calls", resp.StatusCode, calls.Load())
	}
}

func TestDigestChallengeErrorsFromServer(t *testing.T) {
	tests := []struct{ name, challenge, route string }{
		{"unsupported", `Digest realm="r", nonce="n", algorithm=SHA-512-256`, "parseDigest.EngineDigestUnsupported"},
		{"no nonce", `Digest realm="r"`, "parseDigest.EngineDigestChallengeInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("WWW-Authenticate", tt.challenge)
				w.WriteHeader(401)
			})
			in := Input{Request: format.Request{Method: "GET", URL: srv.URL, Auth: &format.Auth{Type: format.AuthDigest, Username: "u", Password: "p"}}}
			_, err := newEngine(t, Config{}).Send(t.Context(), in)
			wantRoute(t, err, "Engine.Do->parseDigest")
			wantRoute(t, err, tt.route)
		})
	}
}
