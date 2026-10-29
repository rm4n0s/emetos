package containerapi

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// The default ports of a tcp:// host without a port: Docker's, without and with TLS.
const (
	defaultTCPPort    = "2375"
	defaultTLSTCPPort = "2376"
)

// TLSOptions say how a tcp:// host is secured (SPEC §10.20): the variables of the Docker CLI, DOCKER_TLS_VERIFY and
// DOCKER_CERT_PATH, in a struct.
type TLSOptions struct {
	// Verify turns TLS on and checks the server's certificate (DOCKER_TLS_VERIFY).
	Verify bool
	// CertDir holds ca.pem, cert.pem and key.pem (DOCKER_CERT_PATH). A missing ca.pem means the system's roots, and a
	// missing cert.pem or key.pem means no client certificate.
	CertDir string
	// SkipVerify turns TLS on without checking the server's certificate (a Docker context made with skip-tls-verify).
	SkipVerify bool
}

// Endpoint is where an engine listens.
type Endpoint struct {
	// Network is "unix" or "tcp".
	Network string
	// Address is the path of the socket, or host:port.
	Address string
	// TLS is the configuration of the connection, nil for plain HTTP.
	TLS *tls.Config
}

// String returns the endpoint as a host the engines' tools take: unix:///path or tcp://host:port.
func (e Endpoint) String() string {
	if e.Network == "unix" {
		return "unix://" + e.Address
	}
	return "tcp://" + e.Address
}

// Secure reports whether the connection uses TLS.
func (e Endpoint) Secure() bool { return e.TLS != nil }

// UnixEndpoint returns the endpoint of a Unix socket.
func UnixEndpoint(path string) Endpoint { return Endpoint{Network: "unix", Address: path} }

// ParseHost reads a host as DOCKER_HOST, CONTAINER_HOST and the settings write it: unix:///path, a bare absolute path,
// tcp://host[:port], http://host[:port] and https://host[:port]. A tcp host uses TLS when tlsOpts say so or the scheme is
// https. ssh://, npipe:// and fd:// are not built (SPEC §10.20 "Not built").
//
// The errors have the tags ContainerAPIHostInvalid, ContainerAPIHostUnsupported and ContainerAPITLSFailed.
func ParseHost(host string, tlsOpts TLSOptions) (Endpoint, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return Endpoint{}, errors.New("ContainerAPIHostInvalid", "the engine host is empty", "host", host)
	}
	if strings.HasPrefix(host, "/") {
		return UnixEndpoint(host), nil
	}
	scheme, rest, found := strings.Cut(host, "://")
	if !found {
		return Endpoint{}, errors.New("ContainerAPIHostInvalid", "the engine host \""+host+"\" is not unix:///path, tcp://host:port or an absolute path", "host", host)
	}
	switch strings.ToLower(scheme) {
	case "unix":
		path := rest
		if !strings.HasPrefix(path, "/") {
			return Endpoint{}, errors.New("ContainerAPIHostInvalid", "the socket path of \""+host+"\" is not absolute", "host", host)
		}
		return UnixEndpoint(path), nil
	case "tcp", "http", "https":
		useTLS := tlsOpts.Verify || tlsOpts.SkipVerify || strings.EqualFold(scheme, "https")
		address, ok := tcpAddress(rest, useTLS)
		if !ok {
			return Endpoint{}, errors.New("ContainerAPIHostInvalid", "the address of the engine host \""+host+"\" is not host[:port]", "host", host)
		}
		ep := Endpoint{Network: "tcp", Address: address}
		if useTLS {
			conf, err := loadTLS(address, tlsOpts)
			if err != nil {
				return Endpoint{}, err
			}
			ep.TLS = conf
		}
		return ep, nil
	case "ssh", "npipe", "fd":
		return Endpoint{}, errors.New("ContainerAPIHostUnsupported", "engine hosts of the kind "+scheme+":// are not supported; use a unix socket or tcp:// with TLS", "host", host)
	}
	return Endpoint{}, errors.New("ContainerAPIHostInvalid", "the engine host \""+host+"\" has an unknown scheme "+scheme+"://", "host", host)
}

// tcpAddress turns the part of a host after "://" into host:port, with the default port when it has none; false when it
// is not an address.
func tcpAddress(rest string, useTLS bool) (string, bool) {
	u, err := url.Parse("//" + strings.TrimSuffix(rest, "/"))
	if err != nil || u.Hostname() == "" || u.Path != "" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = defaultTCPPort
		if useTLS {
			port = defaultTLSTCPPort
		}
	}
	return net.JoinHostPort(u.Hostname(), port), true
}

// loadTLS reads the certificates of tlsOpts and makes the client configuration for the server at address.
func loadTLS(address string, tlsOpts TLSOptions) (*tls.Config, error) {
	host, _, splitErr := net.SplitHostPort(address)
	if splitErr != nil {
		host = address
	}
	conf := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: tlsOpts.SkipVerify} //nolint:gosec // the user asked for it
	if tlsOpts.CertDir == "" {
		return conf, nil
	}
	if ca, err := os.ReadFile(filepath.Join(tlsOpts.CertDir, "ca.pem")); err == nil {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, errors.New("ContainerAPITLSFailed", "ca.pem in "+tlsOpts.CertDir+" holds no certificate", "dir", tlsOpts.CertDir, "file", "ca.pem")
		}
		conf.RootCAs = pool
	} else if !os.IsNotExist(err) {
		return nil, errors.NewErr("ContainerAPITLSFailed", err).SetMetadata("dir", tlsOpts.CertDir, "file", "ca.pem")
	}
	certFile, keyFile := filepath.Join(tlsOpts.CertDir, "cert.pem"), filepath.Join(tlsOpts.CertDir, "key.pem")
	if _, err := os.Stat(certFile); err != nil {
		if os.IsNotExist(err) {
			return conf, nil // no client certificate
		}
		return nil, errors.NewErr("ContainerAPITLSFailed", err).SetMetadata("dir", tlsOpts.CertDir, "file", "cert.pem")
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, errors.NewErr("ContainerAPITLSFailed", err).SetMetadata("dir", tlsOpts.CertDir, "file", "cert.pem and key.pem")
	}
	conf.Certificates = []tls.Certificate{pair}
	return conf, nil
}
