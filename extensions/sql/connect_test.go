package sql

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql/driver"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

func TestPostgresConnectionSettings(t *testing.T) {
	tests := []struct {
		name     string
		c        dbconn.Connection
		password string
		db       string
		check    func(t *testing.T, host string, port uint16, user, password, database string, tls bool, params map[string]string)
	}{
		{
			name: "fields", c: dbconn.Connection{Name: "p", Engine: "postgres", Host: "db.example.com", Port: 5433, User: "me", Database: "shop"}, password: "p@ss/w:rd?#", db: "shop",
			check: func(t *testing.T, host string, port uint16, user, password, database string, _ bool, params map[string]string) {
				if host != "db.example.com" || port != 5433 || user != "me" || password != "p@ss/w:rd?#" || database != "shop" {
					t.Errorf("host %q port %d user %q password %q database %q", host, port, user, password, database)
				}
				if params["application_name"] != "Emetos" {
					t.Errorf("params = %v", params)
				}
			},
		},
		{
			name: "url", c: dbconn.Connection{Name: "p", Engine: "postgres", URL: "postgresql://alice@pg.example.org:6000/orders?application_name=mine"}, db: "orders",
			check: func(t *testing.T, host string, port uint16, user, _, database string, _ bool, params map[string]string) {
				if host != "pg.example.org" || port != 6000 || user != "alice" || database != "orders" || params["application_name"] != "mine" {
					t.Errorf("host %q port %d user %q database %q params %v", host, port, user, database, params)
				}
			},
		},
		{
			name: "fields beat the url's user", c: dbconn.Connection{Name: "p", Engine: "postgres", User: "bob", URL: "postgres://alice@h/db"}, db: "db",
			check: func(t *testing.T, host string, port uint16, user, _, _ string, _ bool, _ map[string]string) {
				if host != "h" || port != 5432 || user != "bob" {
					t.Errorf("host %q port %d user %q", host, port, user)
				}
			},
		},
		{
			name: "tls", c: dbconn.Connection{Name: "p", Engine: "postgres", Host: "h", User: "u", TLS: dbconn.TLSRequire}, db: "x",
			check: func(t *testing.T, _ string, _ uint16, _, _, _ string, tlsOn bool, _ map[string]string) {
				if !tlsOn {
					t.Error("require has no TLS configuration")
				}
			},
		},
		{
			name: "unix socket", c: dbconn.Connection{Name: "p", Engine: "postgres", Host: "/var/run/postgresql", User: "u"}, db: "x",
			check: func(t *testing.T, host string, port uint16, _, _, _ string, _ bool, _ map[string]string) {
				if host != "/var/run/postgresql" || port != 5432 {
					t.Errorf("host %q port %d", host, port)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := resolve(tt.c, 5432)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := postgresConfig(tt.c, w, tt.password, tt.db)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.TLSConfig != nil, cfg.RuntimeParams)
		})
	}
}

func TestMySQLConnectionSettings(t *testing.T) {
	tests := []struct {
		name  string
		c     dbconn.Connection
		check func(t *testing.T, addr, net, user, db string, tlsOn, fallback bool)
	}{
		{"fields", dbconn.Connection{Name: "m", Engine: "mysql", Host: "db.example.com", Port: 3307, User: "me", Database: "shop"},
			func(t *testing.T, addr, nw, user, db string, tlsOn, fallback bool) {
				if addr != "db.example.com:3307" || nw != "tcp" || user != "me" || db != "shop" || tlsOn || fallback {
					t.Errorf("addr %q net %q user %q db %q tls %v fallback %v", addr, nw, user, db, tlsOn, fallback)
				}
			}},
		{"url with the default port", dbconn.Connection{Name: "m", Engine: "mysql", URL: "mariadb://root@maria.example.net/app"},
			func(t *testing.T, addr, _, user, db string, _, _ bool) {
				if addr != "maria.example.net:3306" || user != "root" || db != "app" {
					t.Errorf("addr %q user %q db %q", addr, user, db)
				}
			}},
		{"a socket", dbconn.Connection{Name: "m", Engine: "mysql", Host: "/run/mysqld/mysqld.sock", User: "u"},
			func(t *testing.T, addr, nw, _, _ string, _, _ bool) {
				if addr != "/run/mysqld/mysqld.sock" || nw != "unix" {
					t.Errorf("addr %q net %q", addr, nw)
				}
			}},
		{"prefer falls back", dbconn.Connection{Name: "m", Engine: "mysql", Host: "h", TLS: dbconn.TLSPrefer},
			func(t *testing.T, _, _, _, _ string, tlsOn, fallback bool) {
				if !tlsOn || !fallback {
					t.Errorf("tls %v fallback %v", tlsOn, fallback)
				}
			}},
		{"require does not fall back", dbconn.Connection{Name: "m", Engine: "mysql", Host: "h", TLS: dbconn.TLSRequire},
			func(t *testing.T, _, _, _, _ string, tlsOn, fallback bool) {
				if !tlsOn || fallback {
					t.Errorf("tls %v fallback %v", tlsOn, fallback)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := mysqlConfig(ctxFor(t), tt.c, "secret")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Passwd != "secret" || !cfg.ParseTime {
				t.Errorf("password %q parseTime %v", cfg.Passwd, cfg.ParseTime)
			}
			tt.check(t, cfg.Addr, cfg.Net, cfg.User, cfg.DBName, cfg.TLS != nil, cfg.AllowFallbackToPlaintext)
		})
	}
}

func TestSQLServerConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		c        dbconn.Connection
		password string
		db       string
		want     map[string]string
		host     string
		path     string
	}{
		{"fields", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "sql.example.com", Port: 1444, User: "sa", Database: "shop"}, "p@ss word", "shop",
			map[string]string{"database": "shop", "app name": "Emetos"}, "sql.example.com:1444", ""},
		{"an instance", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "sql.example.com", User: "sa", Options: map[string]string{OptionInstance: "SQLEXPRESS"}}, "x", "",
			map[string]string{}, "sql.example.com", "/SQLEXPRESS"},
		{"disable", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "h", User: "sa", TLS: dbconn.TLSDisable}, "x", "", map[string]string{"encrypt": "disable"}, "h:1433", ""},
		{"prefer", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "h", User: "sa", TLS: dbconn.TLSPrefer}, "x", "",
			map[string]string{"encrypt": "false", "TrustServerCertificate": "true"}, "h:1433", ""},
		{"require", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "h", User: "sa", TLS: dbconn.TLSRequire}, "x", "",
			map[string]string{"encrypt": "true", "TrustServerCertificate": "true"}, "h:1433", ""},
		{"verify", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "h", User: "sa", TLS: dbconn.TLSVerifyFull, CAFile: "/etc/ca.pem"}, "x", "",
			map[string]string{"encrypt": "true", "TrustServerCertificate": "false", "certificate": "/etc/ca.pem"}, "h:1433", ""},
		{"read-only is a hint", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "h", User: "sa", ReadOnly: true}, "x", "shop",
			map[string]string{"ApplicationIntent": "ReadOnly"}, "h:1433", ""},
		{"no hint without a database", dbconn.Connection{Name: "s", Engine: "sqlserver", Host: "h", User: "sa", ReadOnly: true}, "x", "",
			map[string]string{"ApplicationIntent": ""}, "h:1433", ""},
		{"url", dbconn.Connection{Name: "s", Engine: "sqlserver", URL: "sqlserver://sa@sql.example.org:1500?encrypt=strict"}, "x", "", map[string]string{"encrypt": "strict"}, "sql.example.org:1500", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := resolve(tt.c, 1433)
			if err != nil {
				t.Fatal(err)
			}
			dsn := sqlserverDSN(tt.c, w, tt.password, tt.db)
			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatal(err)
			}
			if u.Host != tt.host || u.Path != tt.path {
				t.Errorf("host %q path %q, want %q %q", u.Host, u.Path, tt.host, tt.path)
			}
			if pw, _ := u.User.Password(); pw != tt.password {
				t.Errorf("password %q, want %q", pw, tt.password)
			}
			for k, v := range tt.want {
				if got := u.Query().Get(k); got != v {
					t.Errorf("%s = %q, want %q (%s)", k, got, v, strings.ReplaceAll(dsn, tt.password, "***"))
				}
			}
		})
	}
}

func TestSQLiteConnectionString(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tests := []struct {
		name    string
		c       dbconn.Connection
		want    string
		wantErr string
	}{
		{"read-write", dbconn.Connection{Name: "l", Engine: "sqlite", File: "/data/app.db"}, "file:/data/app.db?mode=rw&_pragma=busy_timeout(5000)", ""},
		{"read-only", dbconn.Connection{Name: "l", Engine: "sqlite", File: "/data/app.db", ReadOnly: true}, "file:/data/app.db?mode=ro&_pragma=busy_timeout(5000)", ""},
		{"created", dbconn.Connection{Name: "l", Engine: "sqlite", File: "/data/app.db", Options: map[string]string{OptionCreate: "true"}}, "file:/data/app.db?mode=rwc&_pragma=busy_timeout(5000)", ""},
		{"read-only beats create", dbconn.Connection{Name: "l", Engine: "sqlite", File: "/data/app.db", ReadOnly: true, Options: map[string]string{OptionCreate: "true"}}, "file:/data/app.db?mode=ro&_pragma=busy_timeout(5000)", ""},
		{"a name that needs escaping", dbconn.Connection{Name: "l", Engine: "sqlite", File: "/data/a b?c#d%e.db"}, "file:/data/a%20b%3Fc%23d%25e.db?mode=rw&_pragma=busy_timeout(5000)", ""},
		{"the home folder", dbconn.Connection{Name: "l", Engine: "sqlite", File: "~/notes.db"}, "file:" + home + "/notes.db?mode=rw&_pragma=busy_timeout(5000)", ""},
		{"relative", dbconn.Connection{Name: "l", Engine: "sqlite", File: "notes.db"}, "", "SQLFileNotAbsolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqliteDSN(tt.c)
			if tt.wantErr != "" {
				wantRoute(t, err, "sqliteDSN."+tt.wantErr)
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("dsn = %q, %v\nwant %q", got, err, tt.want)
			}
		})
	}
}

func TestTheConnectTimeoutComesFromTheConnection(t *testing.T) {
	tests := []struct {
		opts map[string]string
		want time.Duration
	}{
		{nil, 15 * time.Second},
		{map[string]string{OptionConnectTimeout: "3"}, 3 * time.Second},
		{map[string]string{OptionConnectTimeout: "0"}, 15 * time.Second},
		{map[string]string{OptionConnectTimeout: "soon"}, 15 * time.Second},
	}
	for _, tt := range tests {
		if got := timeoutOf(dbconn.Connection{Options: tt.opts}); got != tt.want {
			t.Errorf("%v: %v, want %v", tt.opts, got, tt.want)
		}
	}
}

func TestEnginesAgreeWithTheDialects(t *testing.T) {
	engines := Engines()
	if len(engines) != 4 {
		t.Fatalf("engines = %d", len(engines))
	}
	for _, e := range engines {
		if e.Name == "" || e.DisplayIcon() == "" {
			t.Errorf("engine %q has no name or icon", e.ID)
		}
		c := e.NewConnection()
		c.Name = "x"
		switch {
		case e.File:
			c.File = "/a.db"
		default:
			c.Host = "h"
		}
		if err := e.Validate(c); err != nil {
			t.Errorf("%s: a new connection with a host or a file does not validate: %v", e.ID, err)
		}
	}
	postgres := engines[0]
	if postgres.DefaultPort != 5432 || postgres.NewConnection().Options[OptionConnectTimeout] != "15" {
		t.Errorf("postgres = %+v", postgres)
	}
	if err := postgres.Validate(dbconn.Connection{Name: "x", Engine: "postgres", Scope: dbconn.ScopeUser, URL: "mysql://h/db"}); err == nil {
		t.Error("a postgres connection takes a mysql URL")
	}
}

// ---- passwords never show ----

func TestNoErrorOfAFailedConnectionHoldsThePassword(t *testing.T) {
	const marker = "Zq9!marker/Pw:2@x"
	tests := []dbconn.Connection{
		{ID: "p", Name: "pg", Engine: "postgres", Host: "127.0.0.1", Port: 1, User: "me", Database: "d"},
		{ID: "p2", Name: "pg tls", Engine: "postgres", Host: "127.0.0.1", Port: 1, User: "me", TLS: dbconn.TLSRequire},
		{ID: "m", Name: "my", Engine: "mysql", Host: "127.0.0.1", Port: 1, User: "me", Database: "d"},
		{ID: "s", Name: "ms", Engine: "sqlserver", Host: "127.0.0.1", Port: 1, User: "me", Database: "d"},
		{ID: "u", Name: "pg url", Engine: "postgres", URL: "postgres://me@127.0.0.1:1/d"},
		{ID: "bad", Name: "bad", Engine: "postgres", URL: "postgres://me@[::1/d"},
	}
	for _, c := range tests {
		t.Run(c.Name, func(t *testing.T) {
			c.Options = map[string]string{OptionConnectTimeout: "2"}
			err := NewService().Open(ctxFor(t), c, marker)
			if err == nil {
				t.Fatal("connected to a port nobody listens on")
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			encoded, jerr := json.Marshal(appErr.ToJson())
			if jerr != nil {
				t.Fatal(jerr)
			}
			for what, text := range map[string]string{"message": err.Error(), "route": appErr.Route(), "json": string(encoded), "original": appErr.OriginalErr.Error()} {
				for _, form := range []string{marker, url.QueryEscape(marker), url.PathEscape(marker)} {
					if strings.Contains(text, form) {
						t.Errorf("the %s of the error holds the password: %s", what, text)
					}
				}
			}
		})
	}
}

func TestAScrubbedErrorHidesThePasswordAndKeepsItsCause(t *testing.T) {
	cause := errors.New("DriverFake", "dial tcp: user=me password=s3cret%2Fx host=h; also s3cret/x")
	got := scrubbed{msg: "cannot connect", password: "s3cret/x", err: cause}
	if text := got.Error(); strings.Contains(text, "s3cret") || !strings.HasPrefix(text, "cannot connect: dial tcp") {
		t.Errorf("text = %q", text)
	}
	if !errors.Is(got, cause) {
		t.Error("the cause cannot be found under the scrubbed error")
	}
	if text := (scrubbed{err: cause}).Error(); text != cause.Error() {
		t.Errorf("no message, no password: %q", text)
	}
}

// ---- TLS ----

// pki is a CA and a server certificate it signed.
type pki struct {
	caPEM    []byte
	caFile   string
	otherCA  string
	serverTL tls.Certificate
}

// newPKI makes a CA, a server certificate for "db.test" signed by it, and the file of a second CA that signed nothing.
func newPKI(t *testing.T) pki {
	t.Helper()
	makeCA := func(name string) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	ca, caKey, caPEM := makeCA("test CA")
	_, _, otherPEM := makeCA("another CA")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "db.test"}, DNSNames: []string{"db.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := pki{caPEM: caPEM, caFile: filepath.Join(dir, "ca.pem"), otherCA: filepath.Join(dir, "other.pem"), serverTL: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}}
	if err := os.WriteFile(p.caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.otherCA, otherPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// serveTLS starts a TLS server that completes the handshake and closes; it returns its address.
func serveTLS(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	})
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }() // the client's side is the one under test
				if tc, ok := conn.(*tls.Conn); ok {
					_ = tc.Handshake() // the client's side is the one under test
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(conn, 1))
			}()
		}
	}()
	return l.Addr().String()
}

func TestTLSModesCheckTheServerAsTheirNamesSay(t *testing.T) {
	p := newPKI(t)
	addr := serveTLS(t, p.serverTL)
	tests := []struct {
		name     string
		mode     dbconn.TLSMode
		caFile   string
		host     string // the name the config expects the server to have
		wantNil  bool
		wantFail bool
	}{
		{"disable", dbconn.TLSDisable, "", "db.test", true, false},
		{"unset", "", "", "db.test", true, false},
		{"prefer does not check", dbconn.TLSPrefer, "", "db.test", false, false},
		{"require does not check", dbconn.TLSRequire, "", "other.name", false, false},
		{"verify-ca with the right CA", dbconn.TLSVerifyCA, p.caFile, "other.name", false, false},
		{"verify-ca with another CA", dbconn.TLSVerifyCA, p.otherCA, "db.test", false, true},
		{"verify-full with the right CA and name", dbconn.TLSVerifyFull, p.caFile, "db.test", false, false},
		{"verify-full with the wrong name", dbconn.TLSVerifyFull, p.caFile, "other.name", false, true},
		{"verify-full with another CA", dbconn.TLSVerifyFull, p.otherCA, "db.test", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := dbconn.Connection{Name: "t", TLS: tt.mode, CAFile: tt.caFile}
			conf, _, err := tlsConfigFor(ctxFor(t), c, tt.host, x509.SystemCertPool)
			if err != nil {
				t.Fatal(err)
			}
			if (conf == nil) != tt.wantNil {
				t.Fatalf("config nil = %v, want %v", conf == nil, tt.wantNil)
			}
			if conf == nil {
				return
			}
			conn, err := tls.Dial("tcp", addr, conf)
			if err == nil {
				if cerr := conn.Close(); cerr != nil {
					t.Log(cerr)
				}
			}
			if (err != nil) != tt.wantFail {
				t.Errorf("handshake error = %v, want a failure: %v", err, tt.wantFail)
			}
		})
	}
}

func TestACAFileThatCannotBeUsedIsReported(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(bad, bytes.Repeat([]byte("not a certificate\n"), 3), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := tlsConfigFor(ctxFor(t), dbconn.Connection{Name: "t", TLS: dbconn.TLSVerifyFull, CAFile: bad}, "h", x509.SystemCertPool)
	wantRoute(t, err, "tlsConfigFor->rootPool.SQLTLSCAFileInvalid")
	_, _, err = tlsConfigFor(ctxFor(t), dbconn.Connection{Name: "t", TLS: dbconn.TLSVerifyFull, CAFile: filepath.Join(dir, "none.pem")}, "h", x509.SystemCertPool)
	wantRoute(t, err, "tlsConfigFor->rootPool.SQLTLSCAFileUnreadable")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = tlsConfigFor(cancelled, dbconn.Connection{Name: "t", TLS: dbconn.TLSVerifyCA, CAFile: bad}, "h", x509.SystemCertPool)
	wantRoute(t, err, "tlsConfigFor->rootPool.SQLTLSCAFileUnreadable")
	if conf, _, err := tlsConfigFor(ctxFor(t), dbconn.Connection{Name: "t", TLS: dbconn.TLSVerifyFull}, "h", x509.SystemCertPool); err != nil || conf == nil || conf.RootCAs == nil {
		t.Errorf("no CA file means the system's roots: %v %v", conf, err)
	}
	_, _, err = tlsConfigFor(ctxFor(t), dbconn.Connection{Name: "t", TLS: dbconn.TLSVerifyFull}, "h", func() (*x509.CertPool, error) { return nil, errors.New("RootsFake", "no roots") })
	wantRoute(t, err, "tlsConfigFor->rootPool.SQLTLSRootsUnavailable")
}

// ---- the statements that make a session read-only ----

// fakeConn is a driver connection that records the statements it is given.
type fakeConn struct {
	driver.Conn
	ran    *[]string
	failOn string
	closed *bool
}

// ExecContext implements driver.ExecerContext.
func (f fakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	*f.ran = append(*f.ran, query)
	if query == f.failOn {
		return nil, errors.New("DriverFake", "the server refused")
	}
	return driver.RowsAffected(0), nil
}

// Close implements driver.Conn.
func (f fakeConn) Close() error { *f.closed = true; return nil }

// plainConn is a connection that cannot run a statement by itself.
type plainConn struct {
	driver.Conn
	closed *bool
}

// Close implements driver.Conn.
func (p plainConn) Close() error { *p.closed = true; return nil }

// fakeConnector makes one connection.
type fakeConnector struct {
	driver.Connector
	conn func() driver.Conn
}

// Connect implements driver.Connector.
func (f fakeConnector) Connect(context.Context) (driver.Conn, error) { return f.conn(), nil }

func TestSessionStatementsRunOnEveryNewConnection(t *testing.T) {
	var ran []string
	var closed bool
	statements := []string{"SET one", "SET two"}
	ok := &initConnector{Connector: fakeConnector{conn: func() driver.Conn { return fakeConn{ran: &ran, closed: &closed} }}, statements: statements}
	for range 2 {
		if _, err := ok.Connect(ctxFor(t)); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"SET one", "SET two", "SET one", "SET two"}; !reflect.DeepEqual(ran, want) {
		t.Errorf("ran %v, want %v", ran, want)
	}

	ran, closed = nil, false
	failing := &initConnector{Connector: fakeConnector{conn: func() driver.Conn { return fakeConn{ran: &ran, closed: &closed, failOn: "SET two"} }}, statements: statements}
	_, err := failing.Connect(ctxFor(t))
	wantRoute(t, err, "initConnector.Connect.SQLSessionInitFailed")
	if !closed || !reflect.DeepEqual(ran, statements) {
		t.Errorf("a connection whose session could not be made is kept: closed %v, ran %v", closed, ran)
	}

	closed = false
	unsupported := &initConnector{Connector: fakeConnector{conn: func() driver.Conn { return plainConn{closed: &closed} }}, statements: statements}
	_, err = unsupported.Connect(ctxFor(t))
	wantRoute(t, err, "initConnector.Connect.SQLSessionInitUnsupported")
	if !closed {
		t.Error("a connection that cannot run the statements is kept")
	}

	// no statements: the connection is given back as it is
	closed = false
	none := &initConnector{Connector: fakeConnector{conn: func() driver.Conn { return plainConn{closed: &closed} }}}
	if conn, err := none.Connect(ctxFor(t)); err != nil || conn == nil || closed {
		t.Errorf("no statements: %v %v closed %v", conn, err, closed)
	}
}

func TestOnlyAReadOnlyConnectionGetsTheSessionStatements(t *testing.T) {
	tests := []struct {
		engine   string
		readOnly bool
		wrapped  bool
		want     []string
	}{
		{"postgres", true, true, []string{"SET default_transaction_read_only = on"}},
		{"postgres", false, false, nil},
		{"mysql", true, true, []string{"SET SESSION TRANSACTION READ ONLY"}},
		{"sqlite", true, true, []string{"PRAGMA query_only = ON"}},
		{"sqlserver", true, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			c := dbconn.Connection{ID: "x", Name: "x", Engine: tt.engine, Host: "h", File: "/a.db", ReadOnly: tt.readOnly}
			tgt, err := newTarget(ctxFor(t), c, "")
			if err != nil {
				t.Fatal(err)
			}
			conn, err := tgt.connector(ctxFor(t), "")
			if err != nil {
				t.Fatal(err)
			}
			ic, wrapped := conn.(*initConnector)
			if wrapped != tt.wrapped || (wrapped && !reflect.DeepEqual(ic.statements, tt.want)) {
				t.Errorf("wrapped %v statements %v, want %v %v", wrapped, ic, tt.wrapped, tt.want)
			}
		})
	}
}

func TestTheHomeFolderMustBeKnownForAFileWrittenWithATilde(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := sqliteDSN(dbconn.Connection{Name: "l", Engine: "sqlite", File: "~/notes.db"})
	wantRoute(t, err, "sqliteDSN.SQLFileHomeUnknown")
}

func TestSettingsADriverRefusesAreReportedWithoutThePassword(t *testing.T) {
	const marker = "Zq9!marker"
	tests := []struct {
		name string
		c    dbconn.Connection
		want string
	}{
		{"postgres with a port out of range", dbconn.Connection{Name: "p", Engine: "postgres", URL: "postgres://me@h:99999/db"}, "postgresTarget.func1->postgresConfig.SQLConnectionSettingsInvalid"},
		{"sql server with a setting it does not know", dbconn.Connection{Name: "s", Engine: "sqlserver", URL: "sqlserver://me@h:1433?encrypt=bogus"}, "sqlserverTarget.func1.SQLConnectionSettingsInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tgt, err := newTarget(ctxFor(t), tt.c, marker)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tgt.connector(ctxFor(t), "")
			wantRoute(t, err, tt.want)
			if strings.Contains(err.Error(), marker) {
				t.Errorf("the password is in the error: %v", err)
			}
		})
	}
}
