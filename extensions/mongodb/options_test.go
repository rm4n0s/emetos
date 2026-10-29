package mongodb

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

func TestConnectionURI(t *testing.T) {
	cases := []struct {
		name string
		c    dbconn.Connection
		want string
	}{
		{"a host and a port", dbconn.Connection{Host: "db.example.com", Port: 27017}, "mongodb://db.example.com:27017/"},
		{"a host with no port", dbconn.Connection{Host: "db.example.com"}, "mongodb://db.example.com/"},
		{"spaces around the host", dbconn.Connection{Host: "  db  ", Port: 1}, "mongodb://db:1/"},
		{"a database", dbconn.Connection{Host: "h", Port: 27017, Database: "my shop"}, "mongodb://h:27017/my%20shop"},
		{"a replica set", dbconn.Connection{Host: "a:27017,b:27018", Port: 27017}, "mongodb://a:27017,b:27018/"},
		{"a host that has its port", dbconn.Connection{Host: "a:27018", Port: 27017}, "mongodb://a:27018/"},
		{"an IPv6 address", dbconn.Connection{Host: "[::1]", Port: 27018}, "mongodb://[::1]:27018/"},
		{"an IPv6 address with its port", dbconn.Connection{Host: "[::1]:27019", Port: 27018}, "mongodb://[::1]:27019/"},
		{"a URL wins", dbconn.Connection{Host: "ignored", URL: "mongodb+srv://c.example.net/shop"}, "mongodb+srv://c.example.net/shop"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := connectionURI(tt.c); got != tt.want {
				t.Errorf("connectionURI = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientOptions(t *testing.T) {
	cases := []struct {
		name  string
		c     dbconn.Connection
		pw    string
		check func(t *testing.T, o *options.ClientOptions)
	}{
		{"hosts and a timeout", dbconn.Connection{Host: "a:27017,b:27018"}, "", func(t *testing.T, o *options.ClientOptions) {
			if strings.Join(o.Hosts, ",") != "a:27017,b:27018" || *o.ServerSelectionTimeout != time.Second || *o.AppName != "Emetos" || o.Auth != nil {
				t.Errorf("options = %+v", o)
			}
		}},
		{"a user and a password", dbconn.Connection{Host: "h", User: "app"}, "pw", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth == nil || o.Auth.Username != "app" || o.Auth.Password != "pw" || !o.Auth.PasswordSet || o.Auth.AuthSource != "" {
				t.Errorf("auth = %+v", o.Auth)
			}
			if strings.Contains(o.GetURI(), "pw") {
				t.Errorf("the password is in the connection string: %s", o.GetURI())
			}
		}},
		{"a user with no password", dbconn.Connection{Host: "h", User: "app"}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth == nil || o.Auth.Username != "app" || o.Auth.PasswordSet {
				t.Errorf("auth = %+v", o.Auth)
			}
		}},
		{"no user and no password", dbconn.Connection{Host: "h"}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth != nil {
				t.Errorf("auth = %+v", o.Auth)
			}
		}},
		{"the user of the URL and a password", dbconn.Connection{URL: "mongodb://app@h/shop"}, "pw", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth == nil || o.Auth.Username != "app" || o.Auth.Password != "pw" || o.Auth.AuthSource != "shop" {
				t.Errorf("auth = %+v", o.Auth)
			}
		}},
		{"the user of the form wins over the URL's", dbconn.Connection{URL: "mongodb://old@h/shop", User: "new"}, "pw", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth.Username != "new" {
				t.Errorf("auth = %+v", o.Auth)
			}
		}},
		{"the database is the default auth source", dbconn.Connection{Host: "h", User: "u", Database: "shop"}, "p", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth.AuthSource != "shop" {
				t.Errorf("auth = %+v", o.Auth)
			}
		}},
		{"the auth source and the mechanism", dbconn.Connection{Host: "h", User: "u", Options: map[string]string{"authSource": "users", "authMechanism": "SCRAM-SHA-256"}}, "p", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth.AuthSource != "users" || o.Auth.AuthMechanism != "SCRAM-SHA-256" {
				t.Errorf("auth = %+v", o.Auth)
			}
		}},
		{"X.509 authenticates in $external", dbconn.Connection{Host: "h", Database: "shop", Options: map[string]string{"authMechanism": "MONGODB-X509"}}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.Auth == nil || o.Auth.AuthMechanism != "MONGODB-X509" || o.Auth.AuthSource != "$external" {
				t.Errorf("auth = %+v", o.Auth)
			}
		}},
		{"a replica set, a direct connection and a read preference", dbconn.Connection{Host: "h", Options: map[string]string{
			"replicaSet": "rs0", "directConnection": "true", "readPreference": "secondaryPreferred"}}, "", func(t *testing.T, o *options.ClientOptions) {
			if *o.ReplicaSet != "rs0" || !*o.Direct || o.ReadPreference.Mode() != readpref.SecondaryPreferredMode {
				t.Errorf("options = %+v", o)
			}
		}},
		{"direct connection off", dbconn.Connection{Host: "h", Options: map[string]string{"directConnection": "false"}}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.Direct == nil || *o.Direct {
				t.Errorf("direct = %v", o.Direct)
			}
		}},
		{"no TLS settings leave TLS to the string", dbconn.Connection{Host: "h"}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.TLSConfig != nil {
				t.Error("TLS was set")
			}
		}},
		{"TLS disabled", dbconn.Connection{Host: "h", TLS: dbconn.TLSDisable}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.TLSConfig != nil {
				t.Error("TLS was set")
			}
		}},
		{"TLS required", dbconn.Connection{Host: "h", TLS: dbconn.TLSRequire}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.TLSConfig == nil || !o.TLSConfig.InsecureSkipVerify || o.TLSConfig.MinVersion != tls.VersionTLS12 {
				t.Errorf("tls = %+v", o.TLSConfig)
			}
		}},
		{"TLS with the full check", dbconn.Connection{Host: "h", TLS: dbconn.TLSVerifyFull}, "", func(t *testing.T, o *options.ClientOptions) {
			if o.TLSConfig == nil || o.TLSConfig.InsecureSkipVerify || o.TLSConfig.VerifyConnection != nil {
				t.Errorf("tls = %+v", o.TLSConfig)
			}
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o, err := clientOptions(tt.c, tt.pw, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, o)
		})
	}
}

func TestClientOptionsThatAreWrong(t *testing.T) {
	cases := []struct {
		name string
		c    dbconn.Connection
	}{
		{"a connection string that is not one", dbconn.Connection{URL: "mongodb://"}},
		{"a scheme that is not MongoDB's", dbconn.Connection{URL: "http://h/"}},
		{"a read preference that does not exist", dbconn.Connection{Host: "h", Options: map[string]string{"readPreference": "closest"}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := clientOptions(tt.c, "", time.Second)
			wantRoute(t, err, "clientOptions.MongoOptionsInvalid")
		})
	}
}

// pki makes a CA and a certificate it signs, as PEM, for the TLS tests.
type pki struct {
	caPEM, certPEM, keyPEM []byte
	ca, leaf               *x509.Certificate
}

func newPKI(t *testing.T, name string) pki {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name + " CA"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "leaf"}, DNSNames: []string{"mongo.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(leafDER)
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	return pki{
		caPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		ca:      ca, leaf: leaf,
	}
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTLSWithACAFileAndAClientCertificate(t *testing.T) {
	p := newPKI(t, "good")
	caFile := writeFile(t, "ca.pem", p.caPEM)
	certFile := writeFile(t, "client.pem", append(append([]byte{}, p.certPEM...), p.keyPEM...))
	c := dbconn.Connection{Name: "n", Host: "h", TLS: dbconn.TLSVerifyFull, CAFile: caFile, Options: map[string]string{"tlsCertificateKeyFile": certFile}}
	cfg, err := tlsConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs == nil || len(cfg.Certificates) != 1 || cfg.InsecureSkipVerify {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.RootCAs.Equal(x509.NewCertPool()) {
		t.Error("the CA file was not read")
	}
	// a CA file or a certificate alone makes the TLS configuration also when the mode is the default
	c.TLS = ""
	if cfg, err := tlsConfig(c); err != nil || cfg == nil {
		t.Errorf("tlsConfig with no mode = %v, %v", cfg, err)
	}
	c.TLS = dbconn.TLSDisable
	if cfg, err := tlsConfig(c); err != nil || cfg != nil {
		t.Errorf("tlsConfig disabled = %v, %v", cfg, err)
	}
}

func TestVerifyCAChecksTheChainNotTheName(t *testing.T) {
	good, other := newPKI(t, "good"), newPKI(t, "other")
	caFile := writeFile(t, "ca.pem", good.caPEM)
	cfg, err := tlsConfig(dbconn.Connection{Name: "n", TLS: dbconn.TLSVerifyCA, CAFile: caFile})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{good.leaf}}); err != nil {
		t.Errorf("a certificate of the CA was refused: %v", err)
	}
	// the name of the server is not checked: this certificate is for mongo.test and the server may be called anything
	wantRoute(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{other.leaf}}), "MongoTLSVerifyFailed")
	wantRoute(t, cfg.VerifyConnection(tls.ConnectionState{}), "MongoTLSNoCertificate")
}

func TestTLSFilesThatAreWrong(t *testing.T) {
	p := newPKI(t, "good")
	notPEM := writeFile(t, "ca.pem", []byte("not a certificate"))
	onlyCert := writeFile(t, "cert.pem", p.certPEM)
	cases := []struct {
		name  string
		c     dbconn.Connection
		route string
	}{
		{"a CA file that is not there", dbconn.Connection{Name: "n", TLS: dbconn.TLSVerifyCA, CAFile: filepath.Join(t.TempDir(), "none.pem")}, "tlsConfig.MongoTLSFileFailed"},
		{"a CA file that is not PEM", dbconn.Connection{Name: "n", TLS: dbconn.TLSVerifyCA, CAFile: notPEM}, "tlsConfig.MongoTLSFileInvalid"},
		{"a client certificate that is not there", dbconn.Connection{Name: "n", TLS: dbconn.TLSRequire, Options: map[string]string{"tlsCertificateKeyFile": filepath.Join(t.TempDir(), "none.pem")}}, "tlsConfig.MongoTLSFileFailed"},
		{"a client certificate with no key", dbconn.Connection{Name: "n", TLS: dbconn.TLSRequire, Options: map[string]string{"tlsCertificateKeyFile": onlyCert}}, "tlsConfig.MongoTLSFileInvalid"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tlsConfig(tt.c)
			wantRoute(t, err, tt.route)
			// and through the options of the connection
			tt.c.Host = "h"
			_, err = clientOptions(tt.c, "", time.Second)
			wantRoute(t, err, "clientOptions->tlsConfig")
		})
	}
}

// deadBackend is a backend whose server is not there: every call fails once the driver has looked for a server.
func deadBackend(t *testing.T) *driverBackend {
	t.Helper()
	opts := options.Client().ApplyURI("mongodb://127.0.0.1:1/").SetServerSelectionTimeout(150 * time.Millisecond)
	client, err := mongo.Connect(opts)
	if err != nil {
		t.Fatal(err)
	}
	return &driverBackend{client: client}
}

func TestTheDriverBackendReportsEachFailureWithItsTag(t *testing.T) {
	b := deadBackend(t)
	ctx := context.Background()
	_, err := b.Databases(ctx)
	wantRoute(t, err, "driverBackend.Databases.MongoListDatabasesFailed")
	_, err = b.Collections(ctx, "shop")
	wantRoute(t, err, "driverBackend.Collections.MongoListCollectionsFailed")
	_, err = b.EstimatedCount(ctx, "shop", "orders")
	wantRoute(t, err, "driverBackend.EstimatedCount.MongoCountFailed")
	_, err = b.Indexes(ctx, "shop", "orders")
	wantRoute(t, err, "driverBackend.Indexes.MongoListIndexesFailed")
	_, err = b.Sample(ctx, "shop", "orders", 10)
	wantRoute(t, err, "driverBackend.Sample.MongoSampleFailed")
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// a client that was closed cannot be closed again
	wantRoute(t, b.Close(ctx), "driverBackend.Close.MongoDisconnectFailed")
}

func TestDialingAServerThatIsNotThereFails(t *testing.T) {
	d := driverDialer{timeout: 200 * time.Millisecond}
	c := dbconn.Connection{Name: "Nowhere", Host: "127.0.0.1", Port: 1, User: "u"}
	start := time.Now()
	_, err := d.Dial(context.Background(), c, "pw")
	wantRoute(t, err, "driverDialer.Dial.MongoConnectFailed")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the dial took %v", elapsed)
	}
	if strings.Contains(err.Error(), "pw") {
		t.Errorf("the error shows the password: %v", err)
	}
	// options that are wrong are refused before a server is looked for
	c.Options = map[string]string{"readPreference": "closest"}
	_, err = d.Dial(context.Background(), c, "")
	wantRoute(t, err, "driverDialer.Dial->clientOptions.MongoOptionsInvalid")
}

func TestDialingStopsWhenTheContextEnds(t *testing.T) {
	d := driverDialer{timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := d.Dial(ctx, dbconn.Connection{Name: "Nowhere", Host: "127.0.0.1", Port: 1}, "")
	wantRoute(t, err, "driverDialer.Dial.MongoConnectFailed")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the dial went on for %v after its context ended", elapsed)
	}
}
