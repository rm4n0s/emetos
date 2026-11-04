package engine

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// Send builds the request (Prepare) and sends it. The context cancels it, even while the body is being read.
//
// When the redirects exceed the limit, the response of the last one is returned with its chain together with the error
// EngineTooManyRedirects; for every other failure the response is nil.
func (e *Engine) Send(ctx context.Context, in Input) (*Response, error) {
	p, err := e.Prepare(in)
	if err != nil {
		return nil, err
	}
	return e.Do(ctx, p)
}

// tracer collects the times of the httptrace hooks of one exchange. The hooks run on the transport's goroutines.
type tracer struct {
	mu                                           sync.Mutex
	start                                        time.Time
	dnsStart, dnsDone, connStart, connDone       time.Time
	tlsStart, tlsDone, gotConn, wrote, firstByte time.Time
	reused                                       bool
}

func (t *tracer) hooks() *httptrace.ClientTrace {
	set := func(p *time.Time) { t.mu.Lock(); *p = time.Now(); t.mu.Unlock() }
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { set(&t.dnsStart) },
		DNSDone:  func(httptrace.DNSDoneInfo) { set(&t.dnsDone) },
		ConnectStart: func(_, _ string) {
			t.mu.Lock()
			if t.connStart.IsZero() {
				t.connStart = time.Now()
			}
			t.mu.Unlock()
		},
		ConnectDone:       func(_, _ string, _ error) { set(&t.connDone) },
		TLSHandshakeStart: func() { set(&t.tlsStart) },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { set(&t.tlsDone) },
		GotConn: func(i httptrace.GotConnInfo) {
			t.mu.Lock()
			t.gotConn, t.reused = time.Now(), i.Reused
			t.mu.Unlock()
		},
		WroteRequest:         func(httptrace.WroteRequestInfo) { set(&t.wrote) },
		GotFirstResponseByte: func() { set(&t.firstByte) },
	}
}

// timings computes the phases; end is when the body was read (zero: not read).
func (t *tracer) timings(end time.Time) Timings {
	t.mu.Lock()
	defer t.mu.Unlock()
	span := func(a, b time.Time) time.Duration {
		if a.IsZero() || b.IsZero() || b.Before(a) {
			return 0
		}
		return b.Sub(a)
	}
	tm := Timings{
		DNS: span(t.dnsStart, t.dnsDone), Connect: span(t.connStart, t.connDone), TLS: span(t.tlsStart, t.tlsDone),
		Sent: span(t.gotConn, t.wrote), FirstByte: span(t.wrote, t.firstByte), Reused: t.reused,
	}
	if tm.FirstByte == 0 {
		tm.FirstByte = span(t.gotConn, t.firstByte)
	}
	if !end.IsZero() {
		tm.Download = span(t.firstByte, end)
		tm.Total = span(t.start, end)
	} else {
		tm.Total = span(t.start, time.Now())
	}
	return tm
}

// Do sends a prepared request.
func (e *Engine) Do(ctx context.Context, p *Prepared) (*Response, error) {
	begin := time.Now()
	parent := ctx
	timeout := e.cfg.timeout()
	switch {
	case p.timeout > 0:
		timeout = time.Duration(p.timeout) * time.Millisecond
	case p.timeout < 0:
		timeout = 0
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	follow := boolOr(p.follow, boolOr(e.cfg.FollowRedirects, true))
	maxRedirects := intOr(p.maxRedirect, e.cfg.MaxRedirects)
	if maxRedirects <= 0 && p.maxRedirect == nil {
		maxRedirects = DefaultMaxRedirects
	}
	verify := boolOr(p.verify, boolOr(e.cfg.VerifyTLS, true))
	jar := e.jars.Jar(p.env)

	r := &Response{Unresolved: p.Unresolved, MissingSecrets: p.MissingSecrets}
	u := p.URL
	method := p.Method
	header := p.Header.Clone()
	body := p.body
	redirects := 0
	digestTried := false

	for {
		resp, tr, sent, err := e.exchange(ctx, parent, jar, verify, method, u, header, p.Host, body)
		if err != nil {
			return nil, err
		}
		hop := func(reason string) Hop {
			return Hop{Method: method, URL: u.String(), StatusCode: resp.StatusCode, Status: statusText(resp), Header: resp.Header, Timings: tr.timings(time.Time{}), Reason: reason}
		}

		if resp.StatusCode == http.StatusUnauthorized && p.digest != nil && !digestTried && u.Host == p.URL.Host {
			ch, found, derr := parseDigest(resp.Header.Values("Www-Authenticate"))
			if derr != nil {
				drainClose(resp)
				return nil, derr
			}
			if found {
				digestTried = true
				r.Chain = append(r.Chain, hop(HopDigest))
				drainClose(resp)
				cnonce := newCnonce()
				header.Set("Authorization", digestAuthorization(ch, p.digest.user, p.digest.pass, method, u.RequestURI(), cnonce))
				continue
			}
		}

		if loc := resp.Header.Get("Location"); follow && isRedirect(resp.StatusCode) && loc != "" {
			if redirects >= maxRedirects {
				e.fill(r, resp, tr, sent, verify, u)
				r.Body = &Body{}
				drainClose(resp)
				r.Timings.Total = time.Since(begin)
				e.saveCookies(r, p.env)
				return r, errors.New("EngineTooManyRedirects", "the redirects exceed the limit", "limit", maxRedirects, "url", u.String())
			}
			next, perr := u.Parse(loc)
			if perr != nil || (next.Scheme != "http" && next.Scheme != "https") {
				drainClose(resp)
				return nil, errors.New("EngineRedirectInvalid", "the Location of the redirect is not a valid URL", "location", loc)
			}
			r.Chain = append(r.Chain, hop(HopRedirect))
			drainClose(resp)
			redirects++
			digestTried = false
			if (resp.StatusCode == 301 || resp.StatusCode == 302) && method == http.MethodPost || resp.StatusCode == 303 && method != http.MethodGet && method != http.MethodHead {
				method, body = http.MethodGet, nil
				for _, h := range []string{"Content-Type", "Content-Encoding", "Content-Language", "Content-Location"} {
					header.Del(h)
				}
			}
			if next.Host != u.Host {
				header.Del("Authorization")
				header.Del("Cookie")
				for _, h := range p.authHeaders {
					header.Del(h)
				}
			}
			u = next
			continue
		}

		e.fill(r, resp, tr, sent, verify, u)
		bd, transferred, decoded, rerr := e.readBody(ctx, parent, resp)
		_ = resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		r.Body, r.Transferred, r.Decoded = bd, transferred, decoded
		end := time.Now()
		r.Timings = tr.timings(end)
		r.Timings.Total = end.Sub(begin)
		e.saveCookies(r, p.env)
		return r, nil
	}
}

func (e *Engine) saveCookies(r *Response, env string) {
	if err := e.jars.Save(env); err != nil {
		r.Warnings = append(r.Warnings, "the cookies could not be kept: "+err.Error())
	}
}

func (e *Engine) fill(r *Response, resp *http.Response, tr *tracer, sent Sent, verify bool, u *url.URL) {
	r.Status, r.StatusCode, r.Proto = statusText(resp), resp.StatusCode, resp.Proto
	r.Header, r.Cookies, r.URL, r.Request = resp.Header, resp.Cookies(), u.String(), sent
	r.Timings = tr.timings(time.Time{})
	r.InsecureTLS = !verify && u.Scheme == "https"
	if cs := resp.TLS; cs != nil {
		info := &TLSInfo{Version: tlsVersionName(cs.Version), CipherSuite: tls.CipherSuiteName(cs.CipherSuite), ServerName: cs.ServerName}
		if len(cs.PeerCertificates) > 0 {
			c := cs.PeerCertificates[0]
			info.Subject, info.Issuer, info.NotAfter = c.Subject.String(), c.Issuer.String(), c.NotAfter
		}
		r.TLS = info
	}
}

func statusText(resp *http.Response) string {
	s := strings.TrimSpace(strings.TrimPrefix(resp.Status, resp.Proto))
	if s == "" {
		s = http.StatusText(resp.StatusCode)
	}
	return s
}

func isRedirect(code int) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

func drainClose(resp *http.Response) {
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
	_ = resp.Body.Close()
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// exchange sends one request and returns the response with its headers read, not its body.
func (e *Engine) exchange(ctx, parent context.Context, jar *Jar, verify bool, method string, u *url.URL, header http.Header, host string, body *bodySource) (*http.Response, *tracer, Sent, error) {
	tp, err := e.transport(u, verify)
	if err != nil {
		return nil, nil, Sent{}, err
	}
	tr := &tracer{start: time.Now()}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, tr.hooks()), method, u.String(), nil)
	if err != nil {
		return nil, nil, Sent{}, errors.NewErr("EngineURLInvalid", err).SetMetadata("url", u.String())
	}
	sent := Sent{Method: method, URL: u.String(), BodySize: 0}
	if body != nil {
		rc, size, oerr := body.open()
		if oerr != nil {
			return nil, nil, Sent{}, oerr
		}
		req.Body, req.ContentLength, sent.BodySize = rc, size, size
		if size == 0 {
			_ = rc.Close()
			req.Body = http.NoBody
		}
		req.GetBody = func() (io.ReadCloser, error) {
			rc, _, gerr := body.open()
			return rc, gerr
		}
	}
	req.Header = header.Clone()
	if req.Header.Get("User-Agent") == "" {
		ua := e.cfg.UserAgent
		if ua == "" {
			ua = "Emetos/dev"
		}
		req.Header.Set("User-Agent", ua)
	}
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "gzip, deflate")
	}
	if host != "" {
		req.Host = host
	}
	sent.Header = req.Header.Clone()
	if cs := jar.Cookies(u); len(cs) > 0 && sent.Header.Get("Cookie") == "" {
		parts := make([]string, len(cs))
		for i, c := range cs {
			parts[i] = c.Name + "=" + c.Value
		}
		sent.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	client := &http.Client{
		Transport:     tp,
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, Sent{}, classifySend(err, ctx, parent, u)
	}
	return resp, tr, sent, nil
}

// classifySend gives a failure of the transport its tag.
func classifySend(err error, ctx, parent context.Context, u *url.URL) error {
	if perr := parent.Err(); perr != nil && !errors.Is(perr, context.DeadlineExceeded) {
		return errors.NewErr("EngineCanceled", err).SetMetadata("url", u.String())
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.NewErr("EngineTimeout", err).SetMetadata("url", u.String())
	}
	var verr *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var dns *net.DNSError
	var op *net.OpError
	switch {
	case errors.As(err, &verr) || errors.As(err, &unknown):
		return errors.NewErr("EngineTLSVerifyFailed", err).SetMetadata("url", u.String())
	case errors.As(err, &dns):
		return errors.NewErr("EngineDNSFailed", err).SetMetadata("url", u.String())
	case errors.As(err, &op) && op.Op == "dial":
		return errors.NewErr("EngineConnectFailed", err).SetMetadata("url", u.String())
	}
	return errors.NewErr("EngineSendFailed", err).SetMetadata("url", u.String())
}

// countReader counts the bytes read and keeps the first error that is not the end.
type countReader struct {
	r   io.Reader
	n   int64
	err error
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if err != nil && err != io.EOF && c.err == nil { //nolint:errorlint // io.EOF is returned as is by the readers
		c.err = err
	}
	return n, err //nolint:wrapcheck // a reader passes its error on unchanged
}

// readBody reads the body of the final response, decodes it, and keeps it in memory or in the cache.
func (e *Engine) readBody(ctx, parent context.Context, resp *http.Response) (*Body, int64, []string, error) {
	counter := &countReader{r: resp.Body}
	var r io.Reader = counter
	var decoded []string
	encs := strings.Split(resp.Header.Get("Content-Encoding"), ",")
	for i := len(encs) - 1; i >= 0; i-- {
		enc := strings.ToLower(strings.TrimSpace(encs[i]))
		var next io.Reader
		var derr error
		switch enc {
		case "gzip", "x-gzip":
			br := bufio.NewReader(r)
			if _, perr := br.Peek(1); perr != nil {
				next = br // an empty body has nothing to decode
			} else {
				next, derr = gzip.NewReader(br)
			}
		case "deflate":
			br := bufio.NewReader(r)
			head, perr := br.Peek(2)
			switch {
			case perr != nil:
				next = br
			case head[0]&0x0f == 8 && (int(head[0])<<8|int(head[1]))%31 == 0:
				next, derr = zlib.NewReader(br)
			default:
				next = flate.NewReader(br) // some servers send the raw stream
			}
		default:
			continue
		}
		if derr != nil {
			if ferr := bodyFailure(ctx, parent); ferr != nil {
				return nil, 0, nil, ferr
			}
			return nil, 0, nil, errors.NewErr("EngineDecodeFailed", derr).SetMetadata("encoding", enc)
		}
		r = next
		decoded = append([]string{enc}, decoded...)
	}
	// the announced length is of the bytes that came, so it is the length of the body only when nothing was decoded
	var hint int64
	if len(decoded) == 0 && resp.ContentLength > 0 {
		hint = resp.ContentLength
	}
	bd, err := e.spool(r, hint)
	if err != nil {
		if _, ok := errors.FromError(err); ok {
			return nil, 0, nil, err
		}
		if ferr := bodyFailure(ctx, parent); ferr != nil {
			return nil, 0, nil, ferr
		}
		if counter.err == nil && len(decoded) > 0 {
			return nil, 0, nil, errors.NewErr("EngineDecodeFailed", err).SetMetadata("encoding", strings.Join(decoded, ","))
		}
		return nil, 0, nil, errors.NewErr("EngineBodyReadFailed", err)
	}
	return bd, counter.n, decoded, nil
}

// bodyFailure is the error of a body that was cut by a cancel or the timeout; nil when neither happened.
func bodyFailure(ctx, parent context.Context) error {
	if perr := parent.Err(); perr != nil && !errors.Is(perr, context.DeadlineExceeded) {
		return errors.New("EngineCanceled", "the request was canceled while the body was read")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("EngineTimeout", "the timeout passed while the body was read")
	}
	return nil
}

// errWriter remembers the first error of the writer, to tell a full disk from a broken connection.
type errWriter struct {
	w   io.Writer
	err error
}

func (w *errWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err //nolint:wrapcheck // the writer's error is read from err
}

// memBody is a body being read into memory: the chunks are exact-size slices that are never copied while the body grows, so no old
// copy of a bigger body is left behind for the garbage collector (a bytes.Buffer doubles and keeps up to twice the body).
type memBody struct {
	chunks [][]byte
	n      int64
}

// maxChunk is the size of the biggest chunk of a body being read.
const maxChunk = 1 << 20

// fill reads up to limit bytes from r (stopping at its end) into chunks. first is the size of the first chunk (a body of known length
// is read into one chunk of its size), the next chunks double up to maxChunk.
func (m *memBody) fill(r io.Reader, first, limit int64) error {
	size := min(first, limit)
	for m.n < limit {
		size = max(min(size, limit-m.n), 1)
		chunk := make([]byte, size)
		got := 0
		var err error
		for got < len(chunk) && err == nil {
			var k int
			k, err = r.Read(chunk[got:])
			got += k
		}
		if got > 0 {
			m.chunks = append(m.chunks, chunk[:got])
			m.n += int64(got)
		}
		if err != nil {
			if err == io.EOF { //nolint:errorlint // a reader's end is io.EOF itself
				return nil
			}
			return err //nolint:wrapcheck // readBody tells a read error from a decoding error
		}
		size = min(size*2, maxChunk)
	}
	return nil
}

// bytes joins the chunks: a body that came in one chunk is returned as it is, otherwise it is copied once into a slice of its
// exact size and the chunks go back.
func (m *memBody) bytes() []byte {
	if len(m.chunks) == 1 {
		return m.chunks[0]
	}
	out := make([]byte, 0, m.n)
	for i, c := range m.chunks {
		out = append(out, c...)
		m.chunks[i] = nil
	}
	m.chunks = nil
	return out
}

// spool keeps up to the memory limit in memory; a longer body goes to a file of the cache, written as it is read, so that the
// memory never holds more than the limit. hint is the length the server announced for a body that is not decoded, 0 when unknown.
func (e *Engine) spool(r io.Reader, hint int64) (*Body, error) {
	limit := e.cfg.memoryLimit()
	var mem memBody
	first := int64(4 << 10)
	if hint > 0 && hint <= limit {
		first = hint + 1 // one byte more than announced: a body of the announced length ends in the first chunk
	}
	// a body that is announced as longer than the limit goes to the file from its first byte: nothing of it is held in memory
	if hint <= limit {
		if err := mem.fill(r, first, limit+1); err != nil {
			return nil, err
		}
		if mem.n <= limit {
			return &Body{mem: mem.bytes(), size: mem.n}, nil
		}
	}
	n := mem.n
	dir := e.cfg.CacheDir
	if dir == "" {
		return nil, errors.New("EngineCacheDirMissing", "the body does not fit the memory limit and there is no cache folder", "limit", limit)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errors.NewErr("EngineCacheWriteFailed", err).SetMetadata("dir", dir)
	}
	f, err := os.CreateTemp(dir, "response-*.body")
	if err != nil {
		return nil, errors.NewErr("EngineCacheWriteFailed", err).SetMetadata("dir", dir)
	}
	fail := func(cause error) (*Body, error) {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, cause
	}
	w := &errWriter{w: f}
	for i, c := range mem.chunks {
		if _, err := w.Write(c); err != nil {
			return fail(errors.NewErr("EngineCacheWriteFailed", err).SetMetadata("path", f.Name()))
		}
		mem.chunks[i] = nil // the memory goes back before the rest is read
	}
	mem.chunks = nil
	rest, err := io.Copy(w, r)
	if w.err != nil {
		return fail(errors.NewErr("EngineCacheWriteFailed", w.err).SetMetadata("path", f.Name()))
	}
	if err != nil {
		return fail(err) //nolint:wrapcheck // readBody tells a read error from a decoding error
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return nil, errors.NewErr("EngineCacheWriteFailed", err).SetMetadata("path", f.Name())
	}
	return &Body{path: f.Name(), size: n + rest}, nil
}
