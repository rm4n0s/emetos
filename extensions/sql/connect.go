package sql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/stdlib"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/rm4n0s/errors"
	"modernc.org/sqlite"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// target is how to reach a database server: the connector of the driver for each database it serves. It holds the password inside
// its closures, in memory, for as long as the connection is open.
type target struct {
	dialect dialect.Dialect
	// defaultDB is the database the connection opened ("" for an engine without databases).
	defaultDB string
	// timeout is how long a connection may take to open.
	timeout time.Duration
	// connector makes the connector of a database; "" is the default one.
	connector func(ctx context.Context, database string) (driver.Connector, error)
}

// where holds the parts of an address as the connection gives them, after the URL is read.
type where struct {
	host     string
	port     int
	user     string
	database string
	query    url.Values
}

// resolve reads the connection's host, port, user and database, from its URL when it has one (the fields that are filled in win over
// the URL's, except host and port, which the URL replaces).
func resolve(c dbconn.Connection, defaultPort int) (where, error) {
	w := where{host: c.Host, port: c.Port, user: c.User, database: c.Database, query: url.Values{}}
	if c.URL != "" {
		u, err := url.Parse(c.URL)
		if err != nil {
			return where{}, errors.NewErr("SQLConnectionURLInvalid", scrubbed{msg: "the connection URL cannot be read", err: err}).SetMetadata("connection", c.Name)
		}
		w.host = u.Hostname()
		w.port = 0
		if p := u.Port(); p != "" {
			n, perr := strconv.Atoi(p)
			if perr != nil {
				return where{}, errors.NewErr("SQLConnectionURLInvalid", scrubbed{msg: "the port of the connection URL is not a number", err: perr}).SetMetadata("connection", c.Name)
			}
			w.port = n
		}
		if w.user == "" && u.User != nil {
			w.user = u.User.Username()
		}
		if w.database == "" {
			w.database = strings.TrimPrefix(u.Path, "/")
		}
		w.query = u.Query()
	}
	if w.port == 0 {
		w.port = defaultPort
	}
	return w, nil
}

// timeoutOf reads the connect timeout of a connection.
func timeoutOf(c dbconn.Connection) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(c.Options[OptionConnectTimeout])); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return defaultConnectTimeout * time.Second
}

// tlsMode reads the TLS mode of a connection; empty means the engine's default.
func tlsMode(c dbconn.Connection) dbconn.TLSMode { return c.TLS }

// newTarget makes the target of a connection. The password is empty when there is none. It does not talk to the server.
func newTarget(ctx context.Context, c dbconn.Connection, password string) (*target, error) {
	d, ok := dialect.ByID(c.Engine)
	if !ok {
		return nil, errors.New("SQLEngineUnknown", "the SQL extension has no such engine", "engine", c.Engine)
	}
	var (
		t   *target
		err error
	)
	switch d.ID() {
	case "postgres":
		t, err = postgresTarget(c, password)
	case "mysql":
		t, err = mysqlTarget(ctx, c, password)
	case "sqlite":
		t, err = sqliteTarget(c)
	default:
		t, err = sqlserverTarget(c, password)
	}
	if err != nil {
		return nil, err
	}
	t.dialect, t.timeout = d, timeoutOf(c)
	if c.ReadOnly {
		inner := t.connector
		stmts := d.ReadOnlySession()
		t.connector = func(ctx context.Context, database string) (driver.Connector, error) {
			base, err := inner(ctx, database)
			if err != nil {
				return nil, err
			}
			return &initConnector{Connector: base, statements: stmts}, nil
		}
	}
	return t, nil
}

// ---- PostgreSQL ----

// postgresTarget makes the target of a PostgreSQL connection.
func postgresTarget(c dbconn.Connection, password string) (*target, error) {
	w, err := resolve(c, 5432)
	if err != nil {
		return nil, err
	}
	defaultDB := w.database
	if defaultDB == "" {
		defaultDB = "postgres"
	}
	return &target{defaultDB: defaultDB, connector: func(_ context.Context, database string) (driver.Connector, error) {
		if database == "" {
			database = defaultDB
		}
		cfg, err := postgresConfig(c, w, password, database)
		if err != nil {
			return nil, err
		}
		return stdlib.GetConnector(*cfg), nil
	}}, nil
}

// postgresConfig builds the configuration of pgx for a database, from the connection string it writes with the password in the user
// information of the URL, so that nothing needs quoting.
func postgresConfig(c dbconn.Connection, w where, password, database string) (*pgx.ConnConfig, error) {
	u := url.URL{Scheme: "postgres", Path: "/" + database}
	q := url.Values{}
	for k, v := range w.query {
		q[k] = v
	}
	if strings.HasPrefix(w.host, "/") { // a directory of unix sockets
		q.Set("host", w.host)
		q.Set("port", strconv.Itoa(w.port))
	} else if w.host != "" {
		u.Host = net.JoinHostPort(w.host, strconv.Itoa(w.port))
	}
	switch {
	case w.user != "" && password != "":
		u.User = url.UserPassword(w.user, password)
	case w.user != "":
		u.User = url.User(w.user)
	}
	if mode := tlsMode(c); mode != "" {
		q.Set("sslmode", string(mode))
	}
	if c.CAFile != "" {
		q.Set("sslrootcert", c.CAFile)
	}
	q.Set("connect_timeout", strconv.Itoa(int(timeoutOf(c)/time.Second)))
	if q.Get("application_name") == "" {
		q.Set("application_name", "Emetos")
	}
	u.RawQuery = q.Encode()
	cfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, errors.NewErr("SQLConnectionSettingsInvalid", scrubbed{msg: "the connection settings are not valid", password: password, err: err}).SetMetadata("connection", c.Name, "engine", c.Engine)
	}
	// pgx closes the connection when a context ends unless it is told to send a cancel request first
	cfg.BuildContextWatcherHandler = func(pc *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: pc, DeadlineDelay: 5 * time.Second}
	}
	return cfg, nil
}

// ---- MySQL and MariaDB ----

// mysqlTarget makes the target of a MySQL or MariaDB connection.
func mysqlTarget(ctx context.Context, c dbconn.Connection, password string) (*target, error) {
	cfg, err := mysqlConfig(ctx, c, password)
	if err != nil {
		return nil, err
	}
	return &target{defaultDB: cfg.DBName, connector: func(_ context.Context, database string) (driver.Connector, error) {
		one := cfg.Clone()
		if database != "" {
			one.DBName = database
		}
		conn, err := mysql.NewConnector(one)
		if err != nil {
			return nil, errors.NewErr("SQLConnectionSettingsInvalid", scrubbed{msg: "the connection settings are not valid", password: password, err: err}).SetMetadata("connection", c.Name, "engine", c.Engine)
		}
		return conn, nil
	}}, nil
}

// mysqlConfig builds the configuration of go-sql-driver/mysql.
func mysqlConfig(ctx context.Context, c dbconn.Connection, password string) (*mysql.Config, error) {
	w, err := resolve(c, 3306)
	if err != nil {
		return nil, err
	}
	tlsConf, fallback, err := tlsConfigFor(ctx, c, w.host, x509.SystemCertPool)
	if err != nil {
		return nil, err
	}
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.DBName = w.user, password, w.database
	if strings.HasPrefix(w.host, "/") {
		cfg.Net, cfg.Addr = "unix", w.host
	} else {
		cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(w.host, strconv.Itoa(w.port))
	}
	cfg.TLS, cfg.AllowFallbackToPlaintext = tlsConf, fallback
	cfg.Timeout = timeoutOf(c)
	cfg.ParseTime = true
	cfg.Params = map[string]string{}
	for k := range w.query {
		cfg.Params[k] = w.query.Get(k)
	}
	return cfg, nil
}

// tlsConfigFor maps the TLS mode of a connection to a configuration for a driver that takes one: nil for no TLS; fallback says that
// the connection may continue without TLS when the server has none (prefer). The CA file is read here.
func tlsConfigFor(ctx context.Context, c dbconn.Connection, host string, roots rootsFunc) (conf *tls.Config, fallback bool, err error) {
	switch tlsMode(c) {
	case "", dbconn.TLSDisable:
		return nil, false, nil
	case dbconn.TLSPrefer:
		return &tls.Config{InsecureSkipVerify: true}, true, nil //nolint:gosec // "prefer" encrypts without checking the server, as libpq's prefer does
	case dbconn.TLSRequire:
		return &tls.Config{InsecureSkipVerify: true}, false, nil //nolint:gosec // "require" encrypts without checking the server, as libpq's require does
	}
	pool, err := rootPool(ctx, c, roots)
	if err != nil {
		return nil, false, err
	}
	if tlsMode(c) == dbconn.TLSVerifyFull {
		return &tls.Config{RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS12}, false, nil
	}
	// verify-ca: the chain is checked and the name is not
	return &tls.Config{
		RootCAs: pool, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12, //nolint:gosec // the chain is verified below; only the name is not checked
		VerifyConnection: func(cs tls.ConnectionState) error {
			opts := x509.VerifyOptions{Roots: pool, Intermediates: x509.NewCertPool()}
			for _, cert := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := cs.PeerCertificates[0].Verify(opts)
			return err
		},
	}, false, nil
}

// rootsFunc reads the certificate authorities of the system (x509.SystemCertPool).
type rootsFunc func() (*x509.CertPool, error)

// rootPool is the certificate authorities to check a server against: the CA file of the connection, or the system's when none.
func rootPool(ctx context.Context, c dbconn.Connection, roots rootsFunc) (*x509.CertPool, error) {
	if c.CAFile == "" {
		pool, err := roots()
		if err != nil {
			return nil, errors.NewErr("SQLTLSRootsUnavailable", err).SetMetadata("connection", c.Name)
		}
		return pool, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("SQLTLSCAFileUnreadable", err).SetMetadata("connection", c.Name)
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, errors.NewErr("SQLTLSCAFileUnreadable", err).SetMetadata("connection", c.Name, "file", c.CAFile)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("SQLTLSCAFileInvalid", "the CA file holds no certificate", "connection", c.Name, "file", c.CAFile)
	}
	return pool, nil
}

// ---- SQLite ----

// sqliteTarget makes the target of a SQLite file.
func sqliteTarget(c dbconn.Connection) (*target, error) {
	dsn, err := sqliteDSN(c)
	if err != nil {
		return nil, err
	}
	return &target{connector: func(context.Context, string) (driver.Connector, error) {
		conn, err := sqlite.NewConnector(dsn)
		if err != nil {
			return nil, errors.NewErr("SQLConnectionSettingsInvalid", err).SetMetadata("connection", c.Name, "engine", c.Engine)
		}
		return conn, nil
	}}, nil
}

// sqliteDSN builds the connection string of the modernc driver: the file as a URI, opened read-only, read-write or created.
func sqliteDSN(c dbconn.Connection) (string, error) {
	path := strings.TrimSpace(c.File)
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.NewErr("SQLFileHomeUnknown", err).SetMetadata("connection", c.Name)
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("SQLFileNotAbsolute", "the file of a SQLite connection is written as an absolute path (or ~/...)", "connection", c.Name, "file", c.File)
	}
	mode := "rw"
	switch {
	case c.ReadOnly:
		mode = "ro"
	case c.Options[OptionCreate] == "true":
		mode = "rwc"
	}
	return "file:" + (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath() + "?mode=" + mode + "&_pragma=busy_timeout(5000)", nil
}

// ---- SQL Server ----

// sqlserverTarget makes the target of a SQL Server connection.
func sqlserverTarget(c dbconn.Connection, password string) (*target, error) {
	w, err := resolve(c, 1433)
	if err != nil {
		return nil, err
	}
	return &target{defaultDB: w.database, connector: func(_ context.Context, database string) (driver.Connector, error) {
		if database == "" {
			database = w.database
		}
		conn, err := mssql.NewConnector(sqlserverDSN(c, w, password, database))
		if err != nil {
			return nil, errors.NewErr("SQLConnectionSettingsInvalid", scrubbed{msg: "the connection settings are not valid", password: password, err: err}).SetMetadata("connection", c.Name, "engine", c.Engine)
		}
		return conn, nil
	}}, nil
}

// sqlserverDSN builds the connection string of go-mssqldb for a database.
func sqlserverDSN(c dbconn.Connection, w where, password, database string) string {
	u := url.URL{Scheme: "sqlserver", Host: net.JoinHostPort(w.host, strconv.Itoa(w.port))}
	if inst := strings.TrimSpace(c.Options[OptionInstance]); inst != "" {
		u.Host = w.host // a named instance is found through the browser service, not by port
		u.Path = "/" + inst
	}
	switch {
	case w.user != "" && password != "":
		u.User = url.UserPassword(w.user, password)
	case w.user != "":
		u.User = url.User(w.user)
	}
	q := url.Values{}
	for k, v := range w.query {
		q[k] = v
	}
	if database != "" {
		q.Set("database", database)
	}
	switch tlsMode(c) {
	case dbconn.TLSDisable:
		q.Set("encrypt", "disable")
	case dbconn.TLSPrefer:
		// the server decides: it encrypts when it is set to (SQL Server 2022 does by default), and the certificate is not checked
		q.Set("encrypt", "false")
		q.Set("TrustServerCertificate", "true")
	case dbconn.TLSRequire:
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", "true")
	case dbconn.TLSVerifyCA, dbconn.TLSVerifyFull:
		// go-mssqldb checks the chain and the name together
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", "false")
		if c.CAFile != "" {
			q.Set("certificate", c.CAFile)
		}
	}
	if c.ReadOnly && database != "" { // the driver refuses the intent without a database
		q.Set("ApplicationIntent", "ReadOnly")
	}
	q.Set("app name", "Emetos")
	q.Set("dial timeout", strconv.Itoa(int(timeoutOf(c)/time.Second)))
	u.RawQuery = q.Encode()
	return u.String()
}

// ---- helpers ----

// initConnector runs statements on every connection the pool opens (the statements that make a session read-only).
type initConnector struct {
	driver.Connector
	statements []string
}

// Connect implements driver.Connector.
func (ic *initConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := ic.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if len(ic.statements) == 0 {
		return conn, nil
	}
	ex, ok := conn.(driver.ExecerContext)
	if !ok {
		if cerr := conn.Close(); cerr != nil {
			return nil, errors.NewErr("SQLSessionInitFailed", cerr)
		}
		return nil, errors.New("SQLSessionInitUnsupported", "the driver cannot run the statements that make the session read-only")
	}
	for _, stmt := range ic.statements {
		if _, err := ex.ExecContext(ctx, stmt, nil); err != nil {
			_ = conn.Close() // the statement's error is the one to report
			return nil, errors.NewErr("SQLSessionInitFailed", err).SetMetadata("statement", stmt)
		}
	}
	return conn, nil
}

// scrubbed is an error whose text is a message of ours, with the cause of the driver below it and the password taken out of
// the cause's text. A driver's error may quote the connection string; the password is never shown, logged or kept in an error.
type scrubbed struct {
	msg      string
	password string
	err      error
}

// Error implements error.
func (s scrubbed) Error() string {
	cause := s.err.Error()
	if s.password != "" {
		cause = strings.ReplaceAll(cause, s.password, "***")
		cause = strings.ReplaceAll(cause, url.QueryEscape(s.password), "***")
		cause = strings.ReplaceAll(cause, url.PathEscape(s.password), "***")
	}
	if s.msg == "" {
		return cause
	}
	return s.msg + ": " + cause
}

// Unwrap implements the unwrapping of errors: the cause is the error of the driver.
func (s scrubbed) Unwrap() error { return s.err }
