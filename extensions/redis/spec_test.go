package redis

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// caFile writes a self-signed certificate and returns its path.
func caFile(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test ca"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func conn(mod func(*dbconn.Connection)) dbconn.Connection {
	c := dbconn.Connection{ID: "c1", Name: "cache", Engine: EngineID, Scope: dbconn.ScopeUser, Host: "cache.example.com", Port: 6380, User: "app"}
	if mod != nil {
		mod(&c)
	}
	return c
}

func TestBuildSpec(t *testing.T) {
	tests := []struct {
		name     string
		mod      func(*dbconn.Connection)
		password string
		want     func(Spec) Spec // the fields to compare, from the result
		check    Spec
	}{
		{"host and port", nil, "pw", nil, Spec{Mode: ModeStandalone, Addrs: []string{"cache.example.com:6380"}, Username: "app", Password: "pw"}},
		{"the default port", func(c *dbconn.Connection) { c.Port = 0 }, "", nil, Spec{Mode: ModeStandalone, Addrs: []string{"cache.example.com:6379"}, Username: "app"}},
		{"a database", func(c *dbconn.Connection) { c.Options = map[string]string{OptDB: "3"} }, "", nil,
			Spec{Mode: ModeStandalone, Addrs: []string{"cache.example.com:6380"}, Username: "app", DB: 3}},
		{"a URL", func(c *dbconn.Connection) {
			c.Host, c.Port, c.User, c.URL = "", 0, "", "redis://me@r1.example.com:7000/2"
		}, "", nil,
			Spec{Mode: ModeStandalone, Addrs: []string{"r1.example.com:7000"}, Username: "me", DB: 2}},
		{"the db of a URL query", func(c *dbconn.Connection) { c.Host, c.Port, c.URL = "", 0, "redis://r1.example.com?db=5" }, "", nil,
			Spec{Mode: ModeStandalone, Addrs: []string{"r1.example.com:6379"}, Username: "app", DB: 5}},
		{"the db option beats the URL", func(c *dbconn.Connection) {
			c.Host, c.Port, c.URL = "", 0, "redis://r1.example.com/2"
			c.Options = map[string]string{OptDB: "4"}
		}, "", nil, Spec{Mode: ModeStandalone, Addrs: []string{"r1.example.com:6379"}, Username: "app", DB: 4}},
		{"read-only", func(c *dbconn.Connection) { c.ReadOnly = true }, "", nil,
			Spec{Mode: ModeStandalone, Addrs: []string{"cache.example.com:6380"}, Username: "app", ReadOnly: true}},
		{"sentinel", func(c *dbconn.Connection) {
			c.Options = map[string]string{OptMode: ModeSentinel, OptMaster: "mymaster", OptNodes: "s2.example.com:26379, s3.example.com"}
		}, "", nil, Spec{Mode: ModeSentinel, Addrs: []string{"cache.example.com:6380", "s2.example.com:26379", "s3.example.com:6379"}, Username: "app", MasterName: "mymaster"}},
		{"cluster", func(c *dbconn.Connection) {
			c.Options = map[string]string{OptMode: ModeCluster, OptNodes: "n2:7001;n3:7002"}
		}, "", nil, Spec{Mode: ModeCluster, Addrs: []string{"cache.example.com:6380", "n2:7001", "n3:7002"}, Username: "app"}},
		{"an IPv6 node", func(c *dbconn.Connection) {
			c.Options = map[string]string{OptMode: ModeCluster, OptNodes: "[::1]:7001"}
		}, "", nil,
			Spec{Mode: ModeCluster, Addrs: []string{"cache.example.com:6380", "[::1]:7001"}, Username: "app"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildSpec(context.Background(), conn(tc.mod), tc.password)
			if err != nil {
				t.Fatal(err)
			}
			got.TLS = nil
			if !reflect.DeepEqual(got, tc.check) {
				t.Errorf("spec = %+v\nwant   %+v", got, tc.check)
			}
		})
	}
}

func TestBuildSpecErrors(t *testing.T) {
	tests := []struct {
		name  string
		mod   func(*dbconn.Connection)
		route string
	}{
		{"a bad mode", func(c *dbconn.Connection) { c.Options = map[string]string{OptMode: "primary"} }, "BuildSpec.RedisModeInvalid"},
		{"a URL that is not redis", func(c *dbconn.Connection) { c.URL = "http://x.example.com" }, "BuildSpec.RedisURLInvalid"},
		{"a URL that does not parse", func(c *dbconn.Connection) { c.URL = "redis://u:p@%zz" }, "BuildSpec.RedisURLInvalid"},
		{"a URL with a bad port", func(c *dbconn.Connection) { c.URL = "redis://x.example.com:99999" }, "BuildSpec.RedisURLInvalid"},
		{"a URL with a bad database", func(c *dbconn.Connection) { c.URL = "redis://x.example.com/abc" }, "BuildSpec.RedisURLInvalid"},
		{"a URL with a bad db query", func(c *dbconn.Connection) { c.URL = "redis://x.example.com?db=-1" }, "BuildSpec.RedisURLInvalid"},
		{"no host", func(c *dbconn.Connection) { c.Host = "" }, "BuildSpec.RedisAddressInvalid"},
		{"a bad node", func(c *dbconn.Connection) {
			c.Options = map[string]string{OptMode: ModeCluster, OptNodes: "n2:abc"}
		}, "BuildSpec->normalizeAddr.RedisAddressInvalid"},
		{"a node with a lone colon", func(c *dbconn.Connection) {
			c.Options = map[string]string{OptMode: ModeCluster, OptNodes: "n2:"}
		}, "BuildSpec->normalizeAddr.RedisAddressInvalid"},
		{"a node with no host", func(c *dbconn.Connection) {
			c.Options = map[string]string{OptMode: ModeCluster, OptNodes: ":7000"}
		}, "BuildSpec->normalizeAddr.RedisAddressInvalid"},
		{"a bad database", func(c *dbconn.Connection) { c.Options = map[string]string{OptDB: "x"} }, "BuildSpec.RedisDBInvalid"},
		{"a negative database", func(c *dbconn.Connection) { c.Options = map[string]string{OptDB: "-2"} }, "BuildSpec.RedisDBInvalid"},
		{"a cluster database", func(c *dbconn.Connection) { c.Options = map[string]string{OptMode: ModeCluster, OptDB: "1"} }, "BuildSpec.RedisClusterDB"},
		{"a sentinel with no master", func(c *dbconn.Connection) { c.Options = map[string]string{OptMode: ModeSentinel} }, "BuildSpec.RedisSentinelMasterMissing"},
		{"TLS prefer", func(c *dbconn.Connection) { c.TLS = dbconn.TLSPrefer }, "BuildSpec->tlsConfig.RedisTLSModeUnsupported"},
		{"a CA file that is not there", func(c *dbconn.Connection) { c.TLS, c.CAFile = dbconn.TLSVerifyFull, "/nonexistent/ca.pem" }, "BuildSpec->tlsConfig.RedisCAFileUnreadable"},
		{"a CA file with no certificate", func(c *dbconn.Connection) {
			p := filepath.Join(t.TempDir(), "x.pem")
			if err := os.WriteFile(p, []byte("not a certificate"), 0o600); err != nil {
				t.Fatal(err)
			}
			c.TLS, c.CAFile = dbconn.TLSVerifyCA, p
		}, "BuildSpec->tlsConfig.RedisCAFileInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildSpec(context.Background(), conn(tc.mod), "")
			wantRoute(t, err, tc.route)
		})
	}
}

func TestACancelledContextStopsTheCAFileRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := BuildSpec(ctx, conn(func(c *dbconn.Connection) { c.TLS, c.CAFile = dbconn.TLSVerifyFull, caFile(t) }), "")
	wantRoute(t, err, "BuildSpec->tlsConfig.RedisCAFileUnreadable")
}

func TestTheErrorsOfASpecNeverHoldThePassword(t *testing.T) {
	_, err := BuildSpec(context.Background(), conn(func(c *dbconn.Connection) { c.URL = "redis://u:hunter2@x.example.com:99999" }), "hunter2")
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v", err)
	}
	appErr, _ := asTagged(err)
	for k, v := range appErr.Metadata {
		if s, ok := v.(string); ok && strings.Contains(s, "hunter2") {
			t.Errorf("metadata %q holds the password: %q", k, s)
		}
	}
}

func TestTLSModes(t *testing.T) {
	ca := caFile(t)
	tests := []struct {
		name        string
		mod         func(*dbconn.Connection)
		wantTLS     bool
		skipVerify  bool
		wantRoots   bool
		hasVerifyFn bool
	}{
		{"none", nil, false, false, false, false},
		{"disable", func(c *dbconn.Connection) { c.TLS = dbconn.TLSDisable }, false, false, false, false},
		{"rediss means verify-full", func(c *dbconn.Connection) { c.Host, c.Port, c.URL = "", 0, "rediss://x.example.com" }, true, false, false, false},
		{"disable beats rediss", func(c *dbconn.Connection) {
			c.Host, c.Port, c.URL, c.TLS = "", 0, "rediss://x.example.com", dbconn.TLSDisable
		}, false, false, false, false},
		{"require", func(c *dbconn.Connection) { c.TLS = dbconn.TLSRequire }, true, true, false, false},
		{"verify-ca", func(c *dbconn.Connection) { c.TLS, c.CAFile = dbconn.TLSVerifyCA, ca }, true, true, true, true},
		{"verify-full", func(c *dbconn.Connection) { c.TLS, c.CAFile = dbconn.TLSVerifyFull, ca }, true, false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := BuildSpec(context.Background(), conn(tc.mod), "")
			if err != nil {
				t.Fatal(err)
			}
			if (spec.TLS != nil) != tc.wantTLS {
				t.Fatalf("TLS = %v, want %v", spec.TLS != nil, tc.wantTLS)
			}
			if spec.TLS == nil {
				return
			}
			if spec.TLS.InsecureSkipVerify != tc.skipVerify || (spec.TLS.RootCAs != nil) != tc.wantRoots || (spec.TLS.VerifyConnection != nil) != tc.hasVerifyFn {
				t.Errorf("skip %v roots %v verifyFn %v", spec.TLS.InsecureSkipVerify, spec.TLS.RootCAs != nil, spec.TLS.VerifyConnection != nil)
			}
			if spec.TLS.MinVersion < 0x0303 {
				t.Errorf("MinVersion = %x, want TLS 1.2 or later", spec.TLS.MinVersion)
			}
		})
	}
}

func TestSeparatorOf(t *testing.T) {
	tests := []struct{ opt, want string }{{"", ":"}, {"/", "/"}, {"::", "::"}, {"none", ""}}
	for _, tc := range tests {
		c := conn(func(c *dbconn.Connection) { c.Options = map[string]string{OptSeparator: tc.opt} })
		if got := separatorOf(c); got != tc.want {
			t.Errorf("separator %q = %q, want %q", tc.opt, got, tc.want)
		}
	}
}

func TestTheEngineValidatesItsConnections(t *testing.T) {
	eng := Engine()
	tests := []struct {
		name string
		mod  func(*dbconn.Connection)
		ok   bool
	}{
		{"host", nil, true},
		{"redis URL", func(c *dbconn.Connection) { c.Host, c.Port, c.URL = "", 0, "redis://x.example.com" }, true},
		{"rediss URL", func(c *dbconn.Connection) { c.Host, c.Port, c.URL = "", 0, "rediss://x.example.com" }, true},
		{"a Mongo URL", func(c *dbconn.Connection) { c.Host, c.Port, c.URL = "", 0, "mongodb://x.example.com" }, false},
		{"a bad mode choice", func(c *dbconn.Connection) { c.Options = map[string]string{OptMode: "x"} }, false},
		{"a bad db number", func(c *dbconn.Connection) { c.Options = map[string]string{OptDB: "x"} }, false},
		{"all the options", func(c *dbconn.Connection) {
			c.Options = map[string]string{OptDB: "1", OptSeparator: "/", OptMode: ModeSentinel, OptMaster: "m", OptNodes: "a:1"}
		}, true},
		{"a secret option", func(c *dbconn.Connection) { c.Options = map[string]string{"sentinelPassword": "x"} }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := eng.Validate(conn(tc.mod))
			if (err == nil) != tc.ok {
				t.Errorf("Validate = %v, want ok=%v", err, tc.ok)
			}
		})
	}
	nc := eng.NewConnection()
	if nc.Port != 6379 || nc.Options[OptSeparator] != ":" || nc.Options[OptMode] != ModeStandalone {
		t.Errorf("a new connection = %+v", nc)
	}
}
