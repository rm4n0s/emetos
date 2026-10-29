package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The options of a Redis connection (dbconn.Connection.Options).
const (
	// OptDB is the database the connection selects ("0" when empty; a URL may carry it too).
	OptDB = "db"
	// OptMode is ModeStandalone (empty), ModeSentinel or ModeCluster.
	OptMode = "mode"
	// OptMaster is the name of the master a Sentinel mode asks for.
	OptMaster = "master"
	// OptNodes are more addresses for the Sentinel and Cluster modes, separated by commas or spaces.
	OptNodes = "nodes"
	// OptSeparator is what splits a key into the folders of the tree (":" when empty).
	OptSeparator = "separator"
)

// DefaultPort is the port of Redis.
const DefaultPort = 6379

// DefaultSeparator is what splits a key into the folders of the explorer's tree.
const DefaultSeparator = ":"

// separatorOf is the separator the connection splits its keys at. An option set to "none" turns the tree off.
func separatorOf(c dbconn.Connection) string {
	switch s := c.Options[OptSeparator]; s {
	case "":
		return DefaultSeparator
	case "none":
		return ""
	default:
		return s
	}
}

// BuildSpec turns a connection (its ${env:} references expanded) and the password found for it into the Spec the client needs. The
// errors have the tags RedisURLInvalid, RedisAddressInvalid, RedisDBInvalid, RedisModeInvalid, RedisClusterDB,
// RedisSentinelMasterMissing, RedisTLSModeUnsupported, RedisCAFileUnreadable and RedisCAFileInvalid; none of them holds the password.
func BuildSpec(ctx context.Context, c dbconn.Connection, password string) (Spec, error) {
	spec := Spec{Mode: c.Options[OptMode], Username: c.User, Password: password, ReadOnly: c.ReadOnly}
	if spec.Mode == "" {
		spec.Mode = ModeStandalone
	}
	switch spec.Mode {
	case ModeStandalone, ModeSentinel, ModeCluster:
	default:
		return Spec{}, errors.New("RedisModeInvalid", "the mode is standalone, sentinel or cluster", "mode", spec.Mode)
	}

	scheme := ""
	host, port := strings.TrimSpace(c.Host), c.Port
	if c.URL != "" {
		u, err := url.Parse(c.URL)
		if err != nil {
			return Spec{}, errors.New("RedisURLInvalid", "the connection URL cannot be read", "url", redactedURL(c.URL))
		}
		scheme = strings.ToLower(u.Scheme)
		if scheme != "redis" && scheme != "rediss" {
			return Spec{}, errors.New("RedisURLInvalid", "the connection URL is redis:// or rediss://", "scheme", u.Scheme)
		}
		host = u.Hostname()
		if p := u.Port(); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n <= 0 || n > 65535 {
				return Spec{}, errors.New("RedisURLInvalid", "the port of the URL is a number from 1 to 65535", "port", p)
			}
			port = n
		}
		if u.User != nil && spec.Username == "" {
			spec.Username = u.User.Username()
		}
		if db := strings.Trim(u.Path, "/"); db != "" {
			n, err := strconv.Atoi(db)
			if err != nil || n < 0 {
				return Spec{}, errors.New("RedisURLInvalid", "the path of the URL is the number of a database", "path", u.Path)
			}
			spec.DB = n
		} else if q := u.Query().Get("db"); q != "" {
			n, err := strconv.Atoi(q)
			if err != nil || n < 0 {
				return Spec{}, errors.New("RedisURLInvalid", "the db of the URL is the number of a database", "db", q)
			}
			spec.DB = n
		}
	}
	if host == "" {
		return Spec{}, errors.New("RedisAddressInvalid", "a connection needs a host")
	}
	if port == 0 {
		port = DefaultPort
	}
	spec.Addrs = []string{net.JoinHostPort(host, strconv.Itoa(port))}
	for _, extra := range strings.FieldsFunc(c.Options[OptNodes], func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		addr, err := normalizeAddr(extra)
		if err != nil {
			return Spec{}, err
		}
		spec.Addrs = append(spec.Addrs, addr)
	}

	if v := strings.TrimSpace(c.Options[OptDB]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return Spec{}, errors.New("RedisDBInvalid", "the database is a number from 0", "db", v)
		}
		spec.DB = n
	}
	switch spec.Mode {
	case ModeCluster:
		if spec.DB != 0 {
			return Spec{}, errors.New("RedisClusterDB", "a cluster has only database 0", "db", spec.DB)
		}
	case ModeSentinel:
		spec.MasterName = strings.TrimSpace(c.Options[OptMaster])
		if spec.MasterName == "" {
			return Spec{}, errors.New("RedisSentinelMasterMissing", "a Sentinel connection needs the name of the master")
		}
	}

	mode := c.TLS
	if mode == "" && scheme == "rediss" {
		mode = dbconn.TLSVerifyFull
	}
	cfg, err := tlsConfig(ctx, mode, c.CAFile)
	if err != nil {
		return Spec{}, err
	}
	spec.TLS = cfg
	return spec, nil
}

// normalizeAddr checks an address of the "nodes" option and gives it the default port when it has none.
func normalizeAddr(s string) (string, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		// no port: a bare host, or a bare IPv6 address
		if strings.Contains(s, ":") && !strings.HasPrefix(s, "[") && strings.Count(s, ":") == 1 {
			return "", errors.New("RedisAddressInvalid", "an address is host or host:port", "address", s)
		}
		host, port = strings.Trim(s, "[]"), strconv.Itoa(DefaultPort)
	}
	if host == "" {
		return "", errors.New("RedisAddressInvalid", "an address is host or host:port", "address", s)
	}
	if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
		return "", errors.New("RedisAddressInvalid", "the port of an address is a number from 1 to 65535", "address", s)
	}
	return net.JoinHostPort(host, port), nil
}

// redactedURL is the URL without its user information, for the metadata of an error.
func redactedURL(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			return raw[:i+3] + "…@" + rest[at+1:]
		}
	}
	return raw
}

// tlsConfig makes the TLS configuration of a mode, or nil for a plain connection.
func tlsConfig(ctx context.Context, mode dbconn.TLSMode, caFile string) (*tls.Config, error) {
	switch mode {
	case "", dbconn.TLSDisable:
		return nil, nil
	case dbconn.TLSPrefer:
		return nil, errors.New("RedisTLSModeUnsupported", "a Redis server cannot negotiate TLS: choose disable, require, verify-ca or verify-full", "mode", string(mode))
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	var pool *x509.CertPool
	if caFile != "" {
		if err := ctx.Err(); err != nil {
			return nil, errors.NewErr("RedisCAFileUnreadable", err).SetMetadata("file", caFile)
		}
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, errors.NewErr("RedisCAFileUnreadable", err).SetMetadata("file", caFile)
		}
		pool = x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("RedisCAFileInvalid", "the CA file holds no certificate", "file", caFile)
		}
		cfg.RootCAs = pool
	}
	switch mode {
	case dbconn.TLSRequire:
		cfg.InsecureSkipVerify = true
	case dbconn.TLSVerifyCA:
		// the chain is checked against the CA, the name of the server is not
		cfg.InsecureSkipVerify = true
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return x509.UnknownAuthorityError{}
			}
			opts := x509.VerifyOptions{Roots: pool, Intermediates: x509.NewCertPool()}
			for _, cert := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := cs.PeerCertificates[0].Verify(opts)
			return err
		}
	}
	return cfg, nil
}
