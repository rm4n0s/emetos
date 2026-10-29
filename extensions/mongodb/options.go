package mongodb

import (
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The options of the MongoDB engine's form (dbconn.Engine.Fields): the keys of Connection.Options.
const (
	optAuthSource  = "authSource"
	optMechanism   = "authMechanism"
	optReplicaSet  = "replicaSet"
	optDirect      = "directConnection"
	optReadPref    = "readPreference"
	optClientCert  = "tlsCertificateKeyFile"
	defaultTimeout = 10 * time.Second
)

// engine describes MongoDB to the connection form (SPEC §10.22 "Connections").
func engine() dbconn.Engine {
	return dbconn.Engine{
		ID: "mongodb", Name: "MongoDB", Icon: "database", DefaultPort: 27017,
		URLSchemes: []string{"mongodb", "mongodb+srv"},
		Fields: []dbconn.Field{
			{Key: optAuthSource, Label: "Auth source", Kind: dbconn.FieldText, Placeholder: "admin"},
			{Key: optMechanism, Label: "Auth mechanism", Kind: dbconn.FieldChoice, Choices: []dbconn.Choice{
				{Value: "", Label: "Default"},
				{Value: "SCRAM-SHA-256", Label: "SCRAM-SHA-256"},
				{Value: "SCRAM-SHA-1", Label: "SCRAM-SHA-1"},
				{Value: "MONGODB-X509", Label: "X.509 certificate"},
				{Value: "PLAIN", Label: "PLAIN (LDAP)"},
			}},
			{Key: optClientCert, Label: "Client certificate (X.509)", Kind: dbconn.FieldFile, Placeholder: "a PEM file with the certificate and its key"},
			{Key: optReplicaSet, Label: "Replica set", Kind: dbconn.FieldText},
			{Key: optDirect, Label: "Direct connection", Kind: dbconn.FieldBool},
			{Key: optReadPref, Label: "Read preference", Kind: dbconn.FieldChoice, Choices: []dbconn.Choice{
				{Value: "", Label: "Default (primary)"},
				{Value: "primary", Label: "primary"},
				{Value: "primaryPreferred", Label: "primaryPreferred"},
				{Value: "secondary", Label: "secondary"},
				{Value: "secondaryPreferred", Label: "secondaryPreferred"},
				{Value: "nearest", Label: "nearest"},
			}},
		},
	}
}

// connectionURI is the connection string of a connection: its URL, or one made from the host and the port. The host may be a list
// of members of a replica set (host1:27017,host2:27017).
func connectionURI(c dbconn.Connection) string {
	if c.URL != "" {
		return c.URL
	}
	host := strings.TrimSpace(c.Host)
	bracketed := strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") // an IPv6 address: [::1]
	if c.Port != 0 && !strings.Contains(host, ",") && (bracketed || !strings.Contains(host, ":")) {
		host = host + ":" + strconv.Itoa(c.Port)
	}
	path := "/"
	if db := strings.TrimSpace(c.Database); db != "" {
		path += url.PathEscape(db) // the database of the connection string: it is the default of the auth source, as everywhere in MongoDB
	}
	return "mongodb://" + host + path
}

// clientOptions makes the options of the driver for a connection and its password. The password is set on the credential and never
// written into the connection string, so it is in no string that could be logged. timeout is how long the driver looks for a server
// and waits for a connection. It reads the files of the TLS settings, so it is
// called off the UI thread. The errors have the tags MongoOptionsInvalid, MongoTLSFileFailed and MongoTLSFileInvalid.
func clientOptions(c dbconn.Connection, password string, timeout time.Duration) (*options.ClientOptions, error) {
	opts := options.Client().ApplyURI(connectionURI(c))
	if err := opts.Validate(); err != nil {
		return nil, errors.NewErr("MongoOptionsInvalid", err).SetMetadata("connection", c.Name)
	}
	opts.SetAppName("Emetos")
	opts.SetServerSelectionTimeout(timeout).SetConnectTimeout(timeout)

	cred := options.Credential{}
	if opts.Auth != nil {
		cred = *opts.Auth
	}
	if c.User != "" {
		cred.Username = c.User
	}
	if password != "" {
		cred.Password, cred.PasswordSet = password, true
	}
	if v := c.Options[optAuthSource]; v != "" {
		cred.AuthSource = v
	}
	if v := c.Options[optMechanism]; v != "" {
		cred.AuthMechanism = v
	}
	if cred.AuthSource == "" && c.Database != "" {
		cred.AuthSource = strings.TrimSpace(c.Database) // as in a connection string: the database is where the user is defined, unless the form says otherwise
	}
	if cred.AuthMechanism == "MONGODB-X509" {
		cred.AuthSource = "$external" // the only source X.509 has
	}
	if cred.Username != "" || cred.Password != "" || cred.AuthMechanism != "" {
		opts.SetAuth(cred)
	}
	if v := c.Options[optReplicaSet]; v != "" {
		opts.SetReplicaSet(v)
	}
	if v := c.Options[optDirect]; v != "" {
		opts.SetDirect(v == "true")
	}
	if v := c.Options[optReadPref]; v != "" {
		mode, err := readpref.ModeFromString(v)
		if err != nil {
			return nil, errors.NewErr("MongoOptionsInvalid", err).SetMetadata("connection", c.Name, "readPreference", v)
		}
		rp, err := readpref.New(mode)
		if err != nil {
			return nil, errors.NewErr("MongoOptionsInvalid", err).SetMetadata("connection", c.Name, "readPreference", v)
		}
		opts.SetReadPreference(rp)
	}
	cfg, err := tlsConfig(c)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		opts.SetTLSConfig(cfg)
	}
	return opts, nil
}

// tlsConfig makes the TLS configuration the TLS mode, the CA file and the client certificate ask for; nil when the connection leaves
// TLS to the connection string (the mode is empty or "prefer").
func tlsConfig(c dbconn.Connection) (*tls.Config, error) {
	mode := c.TLS
	certFile := c.Options[optClientCert]
	if mode == dbconn.TLSDisable {
		return nil, nil
	}
	if mode == "" || mode == dbconn.TLSPrefer {
		// MongoDB cannot upgrade a plain connection: the connection string says whether TLS is used (tls=true, mongodb+srv).
		if c.CAFile == "" && certFile == "" {
			return nil, nil
		}
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, errors.NewErr("MongoTLSFileFailed", err).SetMetadata("connection", c.Name, "file", c.CAFile)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("MongoTLSFileInvalid", "the CA file has no certificate in PEM form", "connection", c.Name, "file", c.CAFile)
		}
		cfg.RootCAs = pool
	}
	if certFile != "" {
		pem, err := os.ReadFile(certFile)
		if err != nil {
			return nil, errors.NewErr("MongoTLSFileFailed", err).SetMetadata("connection", c.Name, "file", certFile)
		}
		cert, err := tls.X509KeyPair(pem, pem)
		if err != nil {
			return nil, errors.NewErr("MongoTLSFileInvalid", err).SetMetadata("connection", c.Name, "file", certFile)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	switch mode {
	case dbconn.TLSRequire:
		cfg.InsecureSkipVerify = true //nolint:gosec // "require" is the mode that does not check the certificate (SPEC §10.21 form)
	case dbconn.TLSVerifyCA:
		// check the chain, not the name of the server
		cfg.InsecureSkipVerify = true //nolint:gosec // the chain is checked below in VerifyConnection
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("MongoTLSNoCertificate", "the server sent no certificate")
			}
			inter := x509.NewCertPool()
			for _, cert := range cs.PeerCertificates[1:] {
				inter.AddCert(cert)
			}
			_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: cfg.RootCAs, Intermediates: inter})
			if err != nil {
				return errors.NewErr("MongoTLSVerifyFailed", err)
			}
			return nil
		}
	}
	return cfg, nil
}
