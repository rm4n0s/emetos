package engine

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// The defaults of the settings (SPEC §10.33 "The engine").
const (
	// DefaultTimeoutMs is the timeout of a request (httpClient.timeoutMs).
	DefaultTimeoutMs = 30000
	// DefaultMaxRedirects is the number of redirects that are followed.
	DefaultMaxRedirects = 10
	// DefaultMemoryLimit is the size of a response body that is kept in memory; a larger one goes to a file of the cache.
	DefaultMemoryLimit = 50 << 20
)

// ClientCert is a client certificate for the hosts that match Host (httpClient.clientCertificates).
type ClientCert struct {
	// Host is a pattern for the host name ("api.example.com", "*.example.com"); a pattern with a colon matches host:port.
	Host     string
	CertFile string
	KeyFile  string
	// Passphrase decrypts a key in the legacy PEM encryption (the caller reads it from the keyring).
	Passphrase string
}

// Config is what the engine takes from the settings of the editor. The zero value is the default of every setting.
type Config struct {
	// TimeoutMs limits a request, its body included; 0 is DefaultTimeoutMs and a negative number is no limit.
	TimeoutMs int
	// FollowRedirects is whether redirects are followed (nil: yes). MaxRedirects limits them (0: DefaultMaxRedirects).
	FollowRedirects *bool
	MaxRedirects    int
	// VerifyTLS is whether certificates are verified (nil: yes).
	VerifyTLS *bool
	// CAFile is a PEM file of certificate authorities that are trusted in addition to the system's.
	CAFile string
	// ClientCerts are the client certificates, the first match wins.
	ClientCerts []ClientCert
	// Proxy is the URL of the proxy (httpClient.proxy); empty: HTTP_PROXY, HTTPS_PROXY and NO_PROXY.
	Proxy string
	// UserAgent is sent unless the request has the header; empty: "Emetos/dev".
	UserAgent string
	// CacheDir holds the bodies that do not fit the memory; it is created when needed.
	CacheDir string
	// MemoryLimit is the largest body kept in memory (0: DefaultMemoryLimit).
	MemoryLimit int64
	// Jars keeps the cookies (nil: they live as long as the engine).
	Jars *Jars
}

func (c Config) timeout() time.Duration {
	switch {
	case c.TimeoutMs == 0:
		return DefaultTimeoutMs * time.Millisecond
	case c.TimeoutMs < 0:
		return 0
	}
	return time.Duration(c.TimeoutMs) * time.Millisecond
}

func (c Config) memoryLimit() int64 {
	if c.MemoryLimit <= 0 {
		return DefaultMemoryLimit
	}
	return c.MemoryLimit
}

// Engine sends requests. It is safe for concurrent use; Close it when it is no longer needed.
type Engine struct {
	cfg  Config
	jars *Jars

	mu         sync.Mutex
	transports map[string]*http.Transport
	pool       *x509.CertPool
}

// New makes an engine. The files of the configuration (the CA file, the certificates) are read when a request first needs them.
func New(cfg Config) *Engine {
	jars := cfg.Jars
	if jars == nil {
		jars = NewJars(nil)
	}
	return &Engine{cfg: cfg, jars: jars, transports: map[string]*http.Transport{}}
}

// Jars returns the cookie jars of the engine.
func (e *Engine) Jars() *Jars { return e.jars }

// Close closes the idle connections.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.transports {
		t.CloseIdleConnections()
	}
}

// transport returns the transport for an origin: one per host and verification, because the client certificate depends on the
// host.
func (e *Engine) transport(u *url.URL, verify bool) (*http.Transport, error) {
	key := "verify|"
	if !verify {
		key = "insecure|"
	}
	key += u.Host
	e.mu.Lock()
	defer e.mu.Unlock()
	if t, ok := e.transports[key]; ok {
		return t, nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !verify} //nolint:gosec // the user turned verification off for this request (a warning badge shows it)
	if verify {
		pool, err := e.rootPool()
		if err != nil {
			return nil, err
		}
		tc.RootCAs = pool
	}
	for _, cc := range e.cfg.ClientCerts {
		if !hostMatches(cc.Host, u) {
			continue
		}
		cert, err := loadClientCert(cc)
		if err != nil {
			return nil, err
		}
		tc.Certificates = []tls.Certificate{cert}
		break
	}
	proxy := http.ProxyFromEnvironment
	if e.cfg.Proxy != "" {
		pu, err := url.Parse(e.cfg.Proxy)
		if err != nil || pu.Host == "" {
			return nil, errors.New("EngineProxyInvalid", "the proxy is not a URL with a host", "proxy", e.cfg.Proxy)
		}
		proxy = http.ProxyURL(pu)
	}
	t := &http.Transport{
		Proxy:                 proxy,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       tc,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true, // the engine decodes gzip and deflate itself and keeps Content-Encoding
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	e.transports[key] = t
	return t, nil
}

// rootPool is the system pool with the CA file added; nil when no CA file is configured (the system pool applies).
func (e *Engine) rootPool() (*x509.CertPool, error) {
	if e.cfg.CAFile == "" {
		return nil, nil
	}
	if e.pool != nil {
		return e.pool, nil
	}
	data, err := os.ReadFile(e.cfg.CAFile)
	if err != nil {
		return nil, errors.NewErr("EngineCAFileReadFailed", err).SetMetadata("path", e.cfg.CAFile)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("EngineCAFileInvalid", "the CA file has no certificate", "path", e.cfg.CAFile)
	}
	e.pool = pool
	return pool, nil
}

// hostMatches reports whether the pattern of a client certificate fits the URL.
func hostMatches(pattern string, u *url.URL) bool {
	if pattern == "" {
		return false
	}
	subject := strings.ToLower(u.Hostname())
	if strings.Contains(pattern, ":") {
		subject = strings.ToLower(u.Host)
		if u.Port() == "" {
			subject = net.JoinHostPort(subject, map[string]string{"https": "443", "http": "80"}[u.Scheme])
		}
	}
	ok, err := path.Match(strings.ToLower(pattern), subject)
	return err == nil && ok
}

func loadClientCert(cc ClientCert) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(cc.CertFile)
	if err != nil {
		return tls.Certificate{}, errors.NewErr("EngineClientCertReadFailed", err).SetMetadata("path", cc.CertFile)
	}
	keyPEM, err := os.ReadFile(cc.KeyFile)
	if err != nil {
		return tls.Certificate{}, errors.NewErr("EngineClientCertReadFailed", err).SetMetadata("path", cc.KeyFile)
	}
	if block, _ := pem.Decode(keyPEM); block != nil && x509.IsEncryptedPEMBlock(block) { //nolint:staticcheck // legacy PEM encryption is the only encrypted key the standard library can read
		if cc.Passphrase == "" {
			return tls.Certificate{}, errors.New("EngineClientKeyEncrypted", "the client key is encrypted and there is no passphrase", "path", cc.KeyFile)
		}
		der, derr := x509.DecryptPEMBlock(block, []byte(cc.Passphrase)) //nolint:staticcheck // see above
		if derr != nil {
			return tls.Certificate{}, errors.New("EngineClientKeyPassphrase", "the passphrase does not decrypt the client key", "path", cc.KeyFile)
		}
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der})
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, errors.NewErr("EngineClientCertInvalid", err).SetMetadata("cert", cc.CertFile, "key", cc.KeyFile)
	}
	return cert, nil
}
