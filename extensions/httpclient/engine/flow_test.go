package engine

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

func boolp(b bool) *bool { return &b }
func intp(i int) *int    { return &i }

func TestRedirects(t *testing.T) {
	type hit struct{ method, path, body, auth string }
	var hits []hit
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		hits = append(hits, hit{r.Method, r.URL.Path, string(data), r.Header.Get("Authorization")})
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusMovedPermanently)
		case "/b":
			http.Redirect(w, r, "c?x=1", http.StatusFound)
		case "/c":
			_, _ = w.Write([]byte("end"))
		case "/see":
			http.Redirect(w, r, "/c", http.StatusSeeOther)
		case "/keep":
			http.Redirect(w, r, "/c", http.StatusTemporaryRedirect)
		case "/perm":
			http.Redirect(w, r, "/c", http.StatusPermanentRedirect)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/bad":
			w.Header().Set("Location", "ftp://example.test/x")
			w.WriteHeader(302)
		}
	})
	dir := project(t, map[string]string{"b.txt": "payload"})
	post := func(path string, s *format.Settings) Input {
		return Input{Request: format.Request{Method: "POST", URL: srv.URL + path, Settings: s, Body: &format.Body{Type: format.BodyText, File: "b.txt"}}, Folder: dir}
	}
	tests := []struct {
		name       string
		in         Input
		cfg        Config
		wantStatus int
		wantChain  []string // method path status
		wantLast   hit
	}{
		{name: "followed, the chain kept", in: get(srv.URL + "/a"), wantStatus: 200,
			wantChain: []string{"GET /a 301", "GET /b 302"}, wantLast: hit{"GET", "/c", "", ""}},
		{name: "not followed by the request", in: Input{Request: format.Request{Method: "GET", URL: srv.URL + "/a", Settings: &format.Settings{FollowRedirects: boolp(false)}}},
			wantStatus: 301, wantLast: hit{"GET", "/a", "", ""}},
		{name: "not followed by the settings of the editor", in: get(srv.URL + "/a"), cfg: Config{FollowRedirects: boolp(false)}, wantStatus: 301, wantLast: hit{"GET", "/a", "", ""}},
		{name: "301 turns a POST into a GET", in: post("/a", nil), wantStatus: 200, wantChain: []string{"POST /a 301", "GET /b 302"}, wantLast: hit{"GET", "/c", "", ""}},
		{name: "303 turns a POST into a GET", in: post("/see", nil), wantStatus: 200, wantChain: []string{"POST /see 303"}, wantLast: hit{"GET", "/c", "", ""}},
		{name: "307 keeps the method and the body", in: post("/keep", nil), wantStatus: 200, wantChain: []string{"POST /keep 307"}, wantLast: hit{"POST", "/c", "payload", ""}},
		{name: "308 keeps the method and the body", in: post("/perm", nil), wantStatus: 200, wantChain: []string{"POST /perm 308"}, wantLast: hit{"POST", "/c", "payload", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits = nil
			resp := send(t, newEngine(t, tt.cfg), tt.in)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			var chain []string
			for _, h := range resp.Chain {
				u, _ := url.Parse(h.URL)
				chain = append(chain, fmt.Sprintf("%s %s %d", h.Method, u.Path, h.StatusCode))
				if h.Reason != HopRedirect || h.Header.Get("Location") == "" {
					t.Errorf("hop %+v", h)
				}
			}
			if strings.Join(chain, ";") != strings.Join(tt.wantChain, ";") {
				t.Fatalf("chain %v, want %v", chain, tt.wantChain)
			}
			if last := hits[len(hits)-1]; last != tt.wantLast {
				t.Fatalf("last request %+v, want %+v", last, tt.wantLast)
			}
		})
	}

	t.Run("too many redirects keeps the chain", func(t *testing.T) {
		e := newEngine(t, Config{MaxRedirects: 3})
		resp, err := e.Send(t.Context(), get(srv.URL+"/loop"))
		wantRoute(t, err, "Engine.Do.EngineTooManyRedirects")
		if resp == nil || len(resp.Chain) != 3 || resp.StatusCode != 302 || resp.Body == nil {
			t.Fatalf("response %+v", resp)
		}
	})
	t.Run("the limit of the request", func(t *testing.T) {
		in := get(srv.URL + "/a")
		in.Request.Settings = &format.Settings{MaxRedirects: intp(1)}
		_, err := newEngine(t, Config{}).Send(t.Context(), in)
		wantRoute(t, err, "Engine.Do.EngineTooManyRedirects")
	})
	t.Run("a redirect to something that is not http", func(t *testing.T) {
		_, err := newEngine(t, Config{}).Send(t.Context(), get(srv.URL+"/bad"))
		wantRoute(t, err, "Engine.Do.EngineRedirectInvalid")
	})
}

func TestRedirectToAnotherHostDropsCredentials(t *testing.T) {
	var gotAuth, gotKey, gotCookie string
	other := newServer(t, func(_ http.ResponseWriter, r *http.Request) {
		gotAuth, gotKey, gotCookie = r.Header.Get("Authorization"), r.Header.Get("X-Key"), r.Header.Get("Cookie")
	})
	first := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/there", http.StatusFound)
	})
	for _, auth := range []format.Auth{{Type: format.AuthBearer, Token: "t"}, {Type: format.AuthAPIKey, Key: "X-Key", Value: "v"}} {
		gotAuth, gotKey, gotCookie = "", "", ""
		a := auth
		in := Input{Request: format.Request{Method: "GET", URL: first.URL, Auth: &a, Headers: []format.Pair{pair("Cookie", "mine=1")}}}
		send(t, newEngine(t, Config{}), in)
		if gotAuth != "" || gotKey != "" || gotCookie != "" {
			t.Fatalf("%s: the other host got %q %q %q", auth.Type, gotAuth, gotKey, gotCookie)
		}
	}
}

func TestMemoryLimitAndCacheFile(t *testing.T) {
	big := strings.Repeat("0123456789", 1000) // 10,000 bytes
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(big)) })

	t.Run("under the limit stays in memory", func(t *testing.T) {
		resp := send(t, newEngine(t, Config{MemoryLimit: 10000}), get(srv.URL))
		if !resp.Body.InMemory() || resp.Body.Size() != 10000 || resp.Body.Path() != "" {
			t.Fatalf("body %+v", resp.Body)
		}
	})
	t.Run("over the limit goes to the cache", func(t *testing.T) {
		cache := filepath.Join(t.TempDir(), "cache", "deeper")
		resp := send(t, newEngine(t, Config{MemoryLimit: 9999, CacheDir: cache}), get(srv.URL))
		b := resp.Body
		if b.InMemory() || b.Size() != 10000 || filepath.Dir(b.Path()) != cache {
			t.Fatalf("body in memory %v size %d path %q", b.InMemory(), b.Size(), b.Path())
		}
		if got := bodyString(t, resp); got != big {
			t.Fatalf("the cache file has %d bytes", len(got))
		}
		page := make([]byte, 20)
		n, err := b.ReadAt(page, 9990)
		if string(page[:n]) != big[9990:] || !errors.Is(err, io.EOF) {
			t.Fatalf("page %q err %v", page[:n], err)
		}
		n, err = b.ReadAt(page, 5)
		if err != nil || string(page[:n]) != big[5:25] {
			t.Fatalf("page %q err %v", page[:n], err)
		}
		rc, err := b.Open()
		if err != nil {
			t.Fatal(err)
		}
		all, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(all) != big {
			t.Fatal("Open reads another body")
		}
		if err := b.Remove(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(b.Path()); err == nil {
			t.Fatal("the file is still there")
		}
	})
	t.Run("the default limit is 50 MiB", func(t *testing.T) {
		if got := (Config{}).memoryLimit(); got != 50<<20 {
			t.Fatalf("limit %d", got)
		}
	})
	t.Run("no cache folder", func(t *testing.T) {
		_, err := New(Config{MemoryLimit: 100}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "Engine.spool.EngineCacheDirMissing")
	})
	t.Run("the cache folder cannot be made", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := New(Config{MemoryLimit: 100, CacheDir: filepath.Join(file, "cache")}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "Engine.spool.EngineCacheWriteFailed")
	})
	t.Run("the cache folder is not writable", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
			_ = f.Close()
			t.Skip("the folder is writable anyway (running as root)")
		}
		_, err := New(Config{MemoryLimit: 100, CacheDir: dir}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "Engine.spool.EngineCacheWriteFailed")
	})
}

func TestBodyMethodsFail(t *testing.T) {
	gone := &Body{path: filepath.Join(t.TempDir(), "gone"), size: 1}
	if _, err := gone.Bytes(); err == nil {
		t.Fatal("Bytes")
	} else {
		wantRoute(t, err, "Body.Bytes.EngineBodyReadFailed")
	}
	_, err := gone.ReadAt(make([]byte, 1), 0)
	wantRoute(t, err, "Body.ReadAt.EngineBodyReadFailed")
	_, err = gone.Open()
	wantRoute(t, err, "Body.Open.EngineBodyReadFailed")
	if err := gone.Remove(); err != nil {
		t.Fatalf("removing a file that is gone: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, (&Body{path: dir}).Remove(), "Body.Remove.EngineBodyRemoveFailed")
	short := &Body{mem: []byte("abc"), size: 3}
	if n, err := short.ReadAt(make([]byte, 5), 1); n != 2 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt %d %v", n, err)
	}
	if _, err := short.ReadAt(make([]byte, 1), 3); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt at the end: %v", err)
	}
	if rc, _ := short.Open(); rc == nil {
		t.Fatal("Open")
	}
}

func TestTimeout(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("start"))
			w.(http.Flusher).Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	})
	tests := []struct {
		name  string
		cfg   Config
		in    Input
		route string
	}{
		{name: "the editor's setting", cfg: Config{TimeoutMs: 50}, in: get(srv.URL), route: "classifySend.EngineTimeout"},
		{name: "the request's setting", in: Input{Request: format.Request{Method: "GET", URL: srv.URL, Settings: &format.Settings{TimeoutMs: intp(50)}}}, route: "classifySend.EngineTimeout"},
		{name: "while the body is read", cfg: Config{TimeoutMs: 100}, in: get(srv.URL + "/body"), route: "bodyFailure.EngineTimeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			begin := time.Now()
			_, err := newEngine(t, tt.cfg).Send(t.Context(), tt.in)
			wantRoute(t, err, tt.route)
			if time.Since(begin) > 5*time.Second {
				t.Fatalf("took %v", time.Since(begin))
			}
		})
	}
	t.Run("a request can have no timeout", func(t *testing.T) {
		quick := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(150 * time.Millisecond)
			_, _ = w.Write([]byte("late"))
		})
		in := Input{Request: format.Request{Method: "GET", URL: quick.URL, Settings: &format.Settings{TimeoutMs: intp(0)}}}
		resp := send(t, newEngine(t, Config{TimeoutMs: 20}), in)
		if bodyString(t, resp) != "late" {
			t.Fatal("body")
		}
	})
}

func TestCancel(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("start"))
			w.(http.Flusher).Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	})
	for _, tt := range []struct{ name, path, route string }{
		{"before the response", "/", "classifySend.EngineCanceled"},
		{"while the body is read", "/body", "bodyFailure.EngineCanceled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			time.AfterFunc(100*time.Millisecond, cancel)
			defer cancel()
			begin := time.Now()
			_, err := newEngine(t, Config{TimeoutMs: -1}).Send(ctx, get(srv.URL+tt.path))
			wantRoute(t, err, tt.route)
			if time.Since(begin) > 5*time.Second {
				t.Fatalf("took %v", time.Since(begin))
			}
		})
	}
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := newEngine(t, Config{}).Send(ctx, get(srv.URL))
		wantRoute(t, err, "classifySend.EngineCanceled")
	})
}

func TestContentEncoding(t *testing.T) {
	const text = "the quick brown fox jumps over the lazy dog, again and again and again and again"
	gz := func(s string) []byte {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		_, _ = w.Write([]byte(s))
		_ = w.Close()
		return b.Bytes()
	}
	zl := func(s string) []byte {
		var b bytes.Buffer
		w := zlib.NewWriter(&b)
		_, _ = w.Write([]byte(s))
		_ = w.Close()
		return b.Bytes()
	}
	raw := func(s string) []byte {
		var b bytes.Buffer
		w, _ := flate.NewWriter(&b, flate.DefaultCompression)
		_, _ = w.Write([]byte(s))
		_ = w.Close()
		return b.Bytes()
	}
	tests := []struct {
		name        string
		encoding    string
		payload     []byte
		wantDecoded []string
		wantBody    string
		wantRoute   string
	}{
		{name: "gzip", encoding: "gzip", payload: gz(text), wantDecoded: []string{"gzip"}, wantBody: text},
		{name: "deflate (zlib)", encoding: "deflate", payload: zl(text), wantDecoded: []string{"deflate"}, wantBody: text},
		{name: "deflate (raw)", encoding: "deflate", payload: raw(text), wantDecoded: []string{"deflate"}, wantBody: text},
		{name: "both", encoding: "deflate, gzip", payload: gz(string(zl(text))), wantDecoded: []string{"deflate", "gzip"}, wantBody: text},
		{name: "identity", encoding: "identity", payload: []byte(text), wantBody: text},
		{name: "unknown (brotli) stays as it is", encoding: "br", payload: []byte("br-bytes"), wantBody: "br-bytes"},
		{name: "not gzip at all", encoding: "gzip", payload: []byte("plain text, not gzip"), wantRoute: "Engine.readBody.EngineDecodeFailed"},
		{name: "cut in the middle", encoding: "gzip", payload: gz(strings.Repeat(text, 50))[:40], wantRoute: "Engine.readBody.EngineDecodeFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acceptEncoding string
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				acceptEncoding = r.Header.Get("Accept-Encoding")
				w.Header().Set("Content-Encoding", tt.encoding)
				_, _ = w.Write(tt.payload)
			})
			resp, err := newEngine(t, Config{}).Send(t.Context(), get(srv.URL))
			if tt.wantRoute != "" {
				wantRoute(t, err, tt.wantRoute)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if acceptEncoding != "gzip, deflate" {
				t.Errorf("Accept-Encoding %q", acceptEncoding)
			}
			if bodyString(t, resp) != tt.wantBody || strings.Join(resp.Decoded, ",") != strings.Join(tt.wantDecoded, ",") {
				t.Fatalf("body %q decoded %v", bodyString(t, resp), resp.Decoded)
			}
			if resp.Transferred != int64(len(tt.payload)) || resp.Header.Get("Content-Encoding") != tt.encoding {
				t.Fatalf("transferred %d (want %d), Content-Encoding %q", resp.Transferred, len(tt.payload), resp.Header.Get("Content-Encoding"))
			}
		})
	}
	t.Run("a HEAD answer with Content-Encoding has no body to decode", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Encoding", "gzip") })
		in := get(srv.URL)
		in.Request.Method = "HEAD"
		resp := send(t, newEngine(t, Config{}), in)
		if resp.Body.Size() != 0 {
			t.Fatal("size")
		}
	})
}

func TestTimings(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("y"))
	})
	e := newEngine(t, Config{})
	resp := send(t, e, get(srv.URL))
	tm := resp.Timings
	if tm.Total <= 0 || tm.Connect <= 0 || tm.FirstByte < 5*time.Millisecond || tm.Download < 5*time.Millisecond || tm.Sent < 0 || tm.Reused {
		t.Fatalf("timings %+v", tm)
	}
	if tm.DNS != 0 || tm.TLS != 0 {
		t.Fatalf("an IP address and plain http have no DNS and no TLS: %+v", tm)
	}
	if tm.Total < tm.FirstByte+tm.Download {
		t.Fatalf("total %v is less than its phases %+v", tm.Total, tm)
	}
	again := send(t, e, get(srv.URL))
	if !again.Timings.Reused || again.Timings.Connect != 0 {
		t.Fatalf("the second request should reuse the connection: %+v", again.Timings)
	}
}

func TestTotalCoversTheChain(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			time.Sleep(30 * time.Millisecond)
			http.Redirect(w, r, "/end", http.StatusFound)
		}
	})
	resp := send(t, newEngine(t, Config{}), get(srv.URL))
	if resp.Timings.Total < 30*time.Millisecond || resp.Chain[0].Timings.Total < 30*time.Millisecond {
		t.Fatalf("total %v, first hop %v", resp.Timings.Total, resp.Chain[0].Timings.Total)
	}
}

func TestTransportFailures(t *testing.T) {
	t.Run("the server hangs up", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		})
		_, err := newEngine(t, Config{}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "classifySend.EngineSendFailed")
	})
	t.Run("nothing listens", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		_, err = newEngine(t, Config{}).Send(t.Context(), get("http://"+addr))
		wantRoute(t, err, "classifySend.EngineConnectFailed")
	})
	t.Run("the body is cut short", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			conn, buf, _ := w.(http.Hijacker).Hijack()
			_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort")
			_ = buf.Flush()
			_ = conn.Close()
		})
		_, err := newEngine(t, Config{}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "Engine.readBody.EngineBodyReadFailed")
	})
	t.Run("a name that does not resolve", func(t *testing.T) {
		// classifySend is given the error the resolver returns, so that no lookup leaves the machine
		dnsErr := &url.Error{Op: "Get", URL: "http://name.invalid/", Err: &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "name.invalid", IsNotFound: true}}}
		u, _ := url.Parse("http://name.invalid/")
		err := classifySend(dnsErr, t.Context(), t.Context(), u)
		wantRoute(t, err, "classifySend.EngineDNSFailed")
	})
}

func TestHostWithPort(t *testing.T) {
	for _, tt := range []struct {
		pattern, url string
		want         bool
	}{
		{"127.0.0.1", "https://127.0.0.1:8443/x", true},
		{"*.example.com", "https://api.example.com/", true},
		{"*.example.com", "https://example.com/", false},
		{"api.example.com:8443", "https://api.example.com:8443/", true},
		{"api.example.com:443", "https://api.example.com/", true},
		{"api.example.com:8443", "https://api.example.com/", false},
		{"", "https://api.example.com/", false},
		{"[", "https://api.example.com/", false},
	} {
		u, _ := url.Parse(tt.url)
		if got := hostMatches(tt.pattern, u); got != tt.want {
			t.Errorf("hostMatches(%q, %q) = %v", tt.pattern, tt.url, got)
		}
	}
}
