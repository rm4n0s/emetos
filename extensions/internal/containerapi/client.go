package containerapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// The API versions the Client speaks (SPEC §10.20).
const (
	// MinDockerAPI is the oldest Docker API version the Client works with, MaxDockerAPI the newest one it uses: with a newer
	// engine it still asks for this version, which every newer engine understands.
	MinDockerAPI = "1.41"
	MaxDockerAPI = "1.47"
	// LibpodAPI is the version in the paths of Podman's libpod API, and MinPodmanMajor the oldest Podman (major version).
	LibpodAPI      = "4.0.0"
	MinPodmanMajor = 4
)

const (
	// DefaultMaxBody is the most bytes of a JSON response the Client reads.
	DefaultMaxBody = 32 << 20
	// maxErrorBody is the most bytes of an error response that are read.
	maxErrorBody = 64 << 10
	// maxStreamLine is the longest line of a JSON-lines stream (events, pull progress).
	maxStreamLine = 4 << 20
	// defaultDialTimeout bounds the connect to the engine.
	defaultDialTimeout = 10 * time.Second
)

// Options tune a Client.
type Options struct {
	// MaxBody is the most bytes of a JSON response the Client reads; 0 is DefaultMaxBody. A longer response is an error
	// (ContainerAPIBodyTooLarge), not a truncated read.
	MaxBody int64
	// DialTimeout bounds the connect to the engine; 0 is 10 seconds.
	DialTimeout time.Duration
}

// Client talks to one engine. It is safe for use from several goroutines.
type Client struct {
	kind    Kind
	ep      Endpoint
	hc      *http.Client
	base    string
	maxBody int64

	mu   sync.Mutex
	info *ServerInfo
}

// New makes a Client for the engine of the given kind at ep. It does no I/O: the first call negotiates the API version
// (Negotiate does it at once, to find out whether the engine answers).
func New(kind Kind, ep Endpoint, opts Options) *Client {
	dial := opts.DialTimeout
	if dial <= 0 {
		dial = defaultDialTimeout
	}
	maxBody := opts.MaxBody
	if maxBody <= 0 {
		maxBody = DefaultMaxBody
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: dial}
			return d.DialContext(ctx, ep.Network, ep.Address)
		},
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
	}
	base := "http://engine"
	if ep.TLS != nil {
		tr.TLSClientConfig = ep.TLS.Clone()
		base = "https://" + ep.Address
	} else if ep.Network == "tcp" {
		base = "http://" + ep.Address
	}
	return &Client{kind: kind, ep: ep, hc: &http.Client{Transport: tr}, base: base, maxBody: maxBody}
}

// Kind returns the engine's kind.
func (c *Client) Kind() Kind { return c.kind }

// Endpoint returns where the engine listens.
func (c *Client) Endpoint() Endpoint { return c.ep }

// Close closes the idle connections of the Client. The Client can still be used afterwards.
func (c *Client) Close() { c.hc.CloseIdleConnections() }

// request is one call of the engine's API.
type request struct {
	method, path string
	// unversioned is a path that is sent as it is, without the version prefix (/_ping, /version).
	unversioned bool
	query       url.Values
	// body is encoded as JSON when it is not nil.
	body any
}

// do sends a request. An answer with a status of 400 or more is read, bounded, and returned as the error that names the
// status (ContainerAPINotFound, ContainerAPIConflict, ContainerAPIBadRequest and ContainerAPIEngineError, all with the
// engine's own message). On success the caller closes resp.Body.
func (c *Client) do(ctx context.Context, r request) (*http.Response, error) {
	path := r.path
	if !r.unversioned {
		prefix, err := c.prefix(ctx)
		if err != nil {
			return nil, err
		}
		path = prefix + r.path
	}
	var body io.Reader
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return nil, errors.NewErr("ContainerAPIEncodeFailed", err).SetMetadata("method", r.method, "path", path)
		}
		body = bytes.NewReader(b)
	}
	target := c.base + path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.method, target, body)
	if err != nil {
		return nil, errors.NewErr("ContainerAPIRequestFailed", err).SetMetadata("method", r.method, "path", path)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "emetos")
	resp, err := c.hc.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return nil, errors.NewErr("ContainerAPIConnectFailed", err).SetMetadata("method", r.method, "path", path, "endpoint", c.ep.String())
		}
		return nil, errors.NewErr("ContainerAPIRequestFailed", err).SetMetadata("method", r.method, "path", path, "endpoint", c.ep.String())
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()                      // a body that was read has nothing to report on closing
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody)) // a failed read leaves a shorter message, the status still says what happened
	msg := engineMessage(raw, resp.StatusCode)
	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, errors.New("ContainerAPINotFound", msg, "status", resp.StatusCode, "method", r.method, "path", path)
	case http.StatusConflict:
		return nil, errors.New("ContainerAPIConflict", msg, "status", resp.StatusCode, "method", r.method, "path", path)
	case http.StatusBadRequest:
		return nil, errors.New("ContainerAPIBadRequest", msg, "status", resp.StatusCode, "method", r.method, "path", path)
	}
	return nil, errors.New("ContainerAPIEngineError", msg, "status", resp.StatusCode, "method", r.method, "path", path)
}

// engineMessage returns the text of an engine's error answer: the "message" of its JSON, else its body, else the status
// text.
func engineMessage(raw []byte, status int) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Message != "" {
		return e.Message
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		text = http.StatusText(status)
	}
	if len(text) > 1024 {
		text = text[:1024]
	}
	return text
}

// call sends a request and decodes the JSON answer into out; with a nil out the answer is dropped.
func (c *Client) call(ctx context.Context, r request, out any) error {
	resp, err := c.do(ctx, r)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // a body that was read has nothing to report on closing
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody)) // the answer is not wanted; the connection is reused when it was short
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return errors.NewErr("ContainerAPIReadFailed", err).SetMetadata("method", r.method, "path", r.path)
	}
	if int64(len(raw)) > c.maxBody {
		return errors.New("ContainerAPIBodyTooLarge", "the engine's answer is longer than the limit of "+strconv.FormatInt(c.maxBody, 10)+" bytes", "method", r.method, "path", r.path, "limit", c.maxBody)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", r.method, "path", r.path)
	}
	return nil
}

// prefix returns the version prefix of the paths, negotiating the API version first when that has not happened yet.
func (c *Client) prefix(ctx context.Context) (string, error) {
	info, err := c.Negotiate(ctx)
	if err != nil {
		return "", err
	}
	if c.kind == KindPodman {
		return "/v" + info.APIVersion + "/libpod", nil
	}
	return "/v" + info.APIVersion, nil
}

// Negotiate finds out which API version to speak with /_ping and /version, and returns what the engine says about itself.
// The first call does the work, a later one returns the same answer; a failed call is tried again by the next one.
//
// The errors have the tags ContainerAPIConnectFailed (nothing answers on the socket), ContainerAPIVersionTooOld (Docker
// below 1.41, Podman below 4), ContainerAPIVersionTooNew (an engine whose oldest version is newer than MaxDockerAPI),
// ContainerAPIEngineMismatch (a Podman Client on an engine that is not Podman) and the tags of every call.
func (c *Client) Negotiate(ctx context.Context) (ServerInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.info != nil {
		return *c.info, nil
	}
	pingPath, versionPath := "/_ping", "/version"
	if c.kind == KindPodman {
		pingPath, versionPath = "/libpod/_ping", "/v"+LibpodAPI+"/libpod/version"
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: pingPath, unversioned: true})
	if err != nil {
		return ServerInfo{}, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10)) // "OK"
	_ = resp.Body.Close()
	serverAPI := resp.Header.Get("Api-Version")
	libpodAPI := resp.Header.Get("Libpod-Api-Version")

	var ver struct {
		Version       string
		APIVersion    string `json:"ApiVersion"`
		MinAPIVersion string `json:"MinAPIVersion"`
		OS            string `json:"Os"`
		Arch          string
		Platform      struct{ Name string }
		Components    []struct{ Name string }
	}
	info := ServerInfo{Kind: c.kind}
	switch c.kind {
	case KindPodman:
		if libpodAPI == "" {
			return ServerInfo{}, errors.New("ContainerAPIEngineMismatch", "the engine at "+c.ep.String()+" does not speak Podman's libpod API", "endpoint", c.ep.String(), "server", resp.Header.Get("Server"))
		}
		if major, _, _ := strings.Cut(libpodAPI, "."); atoi(major) < MinPodmanMajor {
			return ServerInfo{}, errors.New("ContainerAPIVersionTooOld", "Podman "+libpodAPI+" is older than Podman "+strconv.Itoa(MinPodmanMajor), "version", libpodAPI, "need", MinPodmanMajor)
		}
		info.APIVersion = LibpodAPI
	default:
		if serverAPI == "" || compareVersions(serverAPI, MinDockerAPI) < 0 {
			return ServerInfo{}, errors.New("ContainerAPIVersionTooOld", "the engine's API version "+serverAPI+" is older than "+MinDockerAPI, "version", serverAPI, "need", MinDockerAPI)
		}
		info.APIVersion = serverAPI
		if compareVersions(serverAPI, MaxDockerAPI) > 0 {
			info.APIVersion = MaxDockerAPI
		}
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: versionPath, unversioned: true}, &ver); err != nil {
		return ServerInfo{}, err
	}
	if c.kind == KindDocker && ver.MinAPIVersion != "" && compareVersions(ver.MinAPIVersion, MaxDockerAPI) > 0 {
		return ServerInfo{}, errors.New("ContainerAPIVersionTooNew", "the engine's oldest API version "+ver.MinAPIVersion+" is newer than "+MaxDockerAPI, "version", ver.MinAPIVersion, "max", MaxDockerAPI)
	}
	info.Version, info.ServerAPIVersion, info.MinAPIVersion, info.OS, info.Arch = ver.Version, ver.APIVersion, ver.MinAPIVersion, ver.OS, ver.Arch
	if c.kind == KindPodman {
		info.ServerAPIVersion = libpodAPI
		info.Engine = "Podman Engine"
	} else {
		info.Engine = ver.Platform.Name
		if info.Engine == "" && len(ver.Components) > 0 {
			info.Engine = ver.Components[0].Name
		}
	}
	c.info = &info
	return info, nil
}

// compareVersions compares two dotted versions ("1.41", "4.0.0") number by number: negative when a is older than b.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = atoi(as[i])
		}
		if i < len(bs) {
			y = atoi(bs[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// atoi reads the leading digits of s; 0 when there are none.
func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// scanLines calls fn for each line of r (a JSON-lines stream) until the end of the stream, a failure, or fn's error. A
// line longer than maxStreamLine is an error (ContainerAPIBodyTooLarge). When ctx is cancelled the read fails and
// scanLines returns nil, because the caller stopped the stream.
func scanLines(ctx context.Context, r io.Reader, fn func(line []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxStreamLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, bufio.ErrTooLong) {
			return errors.NewErr("ContainerAPIBodyTooLarge", err).SetMetadata("limit", maxStreamLine)
		}
		return errors.NewErr("ContainerAPIStreamFailed", err)
	}
	return nil
}
