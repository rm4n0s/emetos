package engine

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rm4n0s/errors"
)

// Timings are the phases of one exchange (SPEC §10.33 "The engine"). A phase that did not happen (the DNS lookup of an IP
// address, the connection of a reused one, TLS of http) is zero.
type Timings struct {
	// DNS is the lookup of the host name.
	DNS time.Duration
	// Connect is the TCP connection.
	Connect time.Duration
	// TLS is the TLS handshake.
	TLS time.Duration
	// Sent is the time to write the request, from the moment the connection was ready.
	Sent time.Duration
	// FirstByte is the wait between the end of the request and the first byte of the response.
	FirstByte time.Duration
	// Download is the time from the first byte to the end of the body (zero for a response that was not read to its end).
	Download time.Duration
	// Total is the whole exchange.
	Total time.Duration
	// Reused says that the connection was already open.
	Reused bool
}

// Hop is a response that came before the final one: a redirect that was followed, or the Digest challenge that was answered.
type Hop struct {
	Method     string
	URL        string
	StatusCode int
	Status     string
	Header     http.Header
	Timings    Timings
	// Reason is HopRedirect or HopDigest: why the request went on.
	Reason string
}

// The reasons a Hop went on.
const (
	HopRedirect = "redirect"
	HopDigest   = "digest"
)

// Sent describes the request as it was sent in the last exchange (with the authorization in its headers, so a caller that
// shows or stores it masks them).
type Sent struct {
	Method string
	URL    string
	Header http.Header
	// BodySize is the length of the body when it is known, and -1 when it was streamed.
	BodySize int64
}

// TLSInfo describes the TLS connection of the final exchange.
type TLSInfo struct {
	Version     string
	CipherSuite string
	ServerName  string
	Subject     string
	Issuer      string
	NotAfter    time.Time
}

// Response is everything about a finished request.
type Response struct {
	// Status is the status line without the protocol ("200 OK"), StatusCode its number.
	Status     string
	StatusCode int
	Proto      string
	Header     http.Header
	// Cookies are those the final response set.
	Cookies []*http.Cookie
	// Body is the body after Content-Encoding was decoded.
	Body *Body
	// Transferred is the number of bytes the body had on the wire (before decoding).
	Transferred int64
	// Decoded lists the encodings that were removed ("gzip", "deflate"), outermost first.
	Decoded []string
	// URL is the URL of the final exchange.
	URL string
	// Chain holds the responses before the final one, in order.
	Chain []Hop
	// Request is the request of the final exchange.
	Request Sent
	// Timings are those of the final exchange; Total is the whole of Send, chain included.
	Timings Timings
	// TLS is nil for a plain http exchange.
	TLS *TLSInfo
	// InsecureTLS says that the certificate was not verified.
	InsecureTLS bool
	// Unresolved and MissingSecrets are the variables the request used that had no value (they were sent as written).
	Unresolved     []string
	MissingSecrets []string
	// Warnings are problems that did not stop the request (the cookies could not be saved).
	Warnings []string
}

// Body is the body of a response: in memory when it fits the limit, otherwise in a file of the cache that is read in pages.
type Body struct {
	mem  []byte
	path string
	size int64
}

// Size is the number of bytes of the body.
func (b *Body) Size() int64 { return b.size }

// InMemory says that the body is in memory, not in a file.
func (b *Body) InMemory() bool { return b.path == "" }

// Path is the cache file of a body that did not fit the memory limit, and empty otherwise.
func (b *Body) Path() string { return b.path }

// Bytes returns the whole body (reading the file if there is one). A body that is in memory is returned without a copy:
// do not change it.
func (b *Body) Bytes() ([]byte, error) {
	if b.path == "" {
		return b.mem, nil
	}
	data, err := os.ReadFile(b.path)
	if err != nil {
		return nil, errors.NewErr("EngineBodyReadFailed", err).SetMetadata("path", b.path)
	}
	return data, nil
}

// ReadAt reads len(p) bytes at the offset; it is io.ReaderAt, for the pages of the response pane.
func (b *Body) ReadAt(p []byte, off int64) (int, error) {
	if b.path == "" {
		if off >= int64(len(b.mem)) {
			return 0, io.EOF
		}
		n := copy(p, b.mem[off:])
		if n < len(p) {
			return n, io.EOF
		}
		return n, nil
	}
	f, err := os.Open(b.path)
	if err != nil {
		return 0, errors.NewErr("EngineBodyReadFailed", err).SetMetadata("path", b.path)
	}
	defer func() { _ = f.Close() }()
	n, err := f.ReadAt(p, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, errors.NewErr("EngineBodyReadFailed", err).SetMetadata("path", b.path)
	}
	return n, err //nolint:wrapcheck // io.EOF is the contract of io.ReaderAt
}

// Open returns a reader of the whole body.
func (b *Body) Open() (io.ReadCloser, error) {
	if b.path == "" {
		return io.NopCloser(bytes.NewReader(b.mem)), nil
	}
	f, err := os.Open(b.path)
	if err != nil {
		return nil, errors.NewErr("EngineBodyReadFailed", err).SetMetadata("path", b.path)
	}
	return f, nil
}

// Remove deletes the cache file of the body, if there is one. The body cannot be read afterwards.
func (b *Body) Remove() error {
	if b.path == "" {
		return nil
	}
	path := b.path
	b.path, b.mem, b.size = "", nil, 0
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.NewErr("EngineBodyRemoveFailed", err).SetMetadata("path", path)
	}
	return nil
}

func tlsVersionName(v uint16) string { return tls.VersionName(v) }

// NewMemoryBody makes the body of a response from bytes that are in memory, for a caller that builds a Response itself (a test of
// the response pane, a stored example).
func NewMemoryBody(data []byte) *Body { return &Body{mem: data, size: int64(len(data))} }
