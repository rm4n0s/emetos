package engine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// testCA is a certificate authority made for a test.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Emetos test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
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
	return &testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue makes a certificate for 127.0.0.1 signed by the CA, and returns it as PEM files in dir and as a tls.Certificate.
func (ca *testCA) issue(t *testing.T, dir, name string, client bool) (certFile, keyFile string, cert tls.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.ExtKeyUsageServerAuth
	if client {
		usage = x509.ExtKeyUsageClientAuth
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certFile, keyFile = filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	cert, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile, cert
}

func (ca *testCA) pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.cert)
	return p
}

// tlsServer starts a TLS server whose certificate the CA signed; clientCA, when set, makes it ask for a client certificate.
func tlsServer(t *testing.T, ca *testCA, clientCA *testCA, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	_, _, cert := ca.issue(t, t.TempDir(), "server", false)
	s := httptest.NewUnstartedServer(h)
	s.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	if clientCA != nil {
		s.TLS.ClientAuth = tls.RequireAndVerifyClientCert
		s.TLS.ClientCAs = clientCA.pool()
	}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	name := "none"
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		name = r.TLS.PeerCertificates[0].Subject.CommonName
	}
	_, _ = w.Write([]byte("client:" + name))
}

func TestTLSVerification(t *testing.T) {
	ca := newCA(t)
	srv := tlsServer(t, ca, nil, okHandler)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("an unknown authority fails", func(t *testing.T) {
		_, err := newEngine(t, Config{}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "classifySend.EngineTLSVerifyFailed")
	})
	t.Run("verification off in the editor's settings", func(t *testing.T) {
		resp := send(t, newEngine(t, Config{VerifyTLS: boolp(false)}), get(srv.URL))
		if bodyString(t, resp) != "client:none" || !resp.InsecureTLS || resp.TLS == nil {
			t.Fatalf("response %+v", resp)
		}
	})
	t.Run("verification off for the request", func(t *testing.T) {
		in := get(srv.URL)
		in.Request.Settings = &format.Settings{VerifyTLS: boolp(false)}
		resp := send(t, newEngine(t, Config{}), in)
		if !resp.InsecureTLS {
			t.Fatal("InsecureTLS")
		}
	})
	t.Run("verification on for the request, off in the settings", func(t *testing.T) {
		in := get(srv.URL)
		in.Request.Settings = &format.Settings{VerifyTLS: boolp(true)}
		_, err := newEngine(t, Config{VerifyTLS: boolp(false)}).Send(t.Context(), in)
		wantRoute(t, err, "classifySend.EngineTLSVerifyFailed")
	})
	t.Run("a CA file is trusted", func(t *testing.T) {
		resp := send(t, newEngine(t, Config{CAFile: caFile}), get(srv.URL))
		if resp.InsecureTLS || resp.TLS == nil || resp.TLS.Subject != "CN=server" || resp.TLS.Issuer != "CN=Emetos test CA" ||
			resp.TLS.Version == "" || resp.TLS.CipherSuite == "" || resp.TLS.NotAfter.IsZero() {
			t.Fatalf("TLS %+v insecure %v", resp.TLS, resp.InsecureTLS)
		}
		if resp.Timings.TLS <= 0 {
			t.Fatalf("timings %+v", resp.Timings)
		}
	})
	t.Run("a CA file that is missing", func(t *testing.T) {
		_, err := newEngine(t, Config{CAFile: filepath.Join(t.TempDir(), "none.pem")}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "Engine.rootPool.EngineCAFileReadFailed")
	})
	t.Run("a CA file without a certificate", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.pem")
		if err := os.WriteFile(bad, []byte("not a certificate"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := newEngine(t, Config{CAFile: bad}).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "Engine.rootPool.EngineCAFileInvalid")
	})
}

func TestClientCertificates(t *testing.T) {
	serverCA, clientCA, otherCA := newCA(t), newCA(t), newCA(t)
	srv := tlsServer(t, serverCA, clientCA, okHandler)
	dir := t.TempDir()
	good, goodKey, _ := clientCA.issue(t, dir, "ada", true)
	untrusted, untrustedKey, _ := otherCA.issue(t, dir, "mallory", true)
	open := Config{VerifyTLS: boolp(false)}

	t.Run("the certificate of the host is sent", func(t *testing.T) {
		cfg := open
		cfg.ClientCerts = []ClientCert{{Host: "other.example", CertFile: untrusted, KeyFile: untrustedKey}, {Host: "127.0.0.1", CertFile: good, KeyFile: goodKey}}
		resp := send(t, newEngine(t, cfg), get(srv.URL))
		if bodyString(t, resp) != "client:ada" {
			t.Fatalf("body %q", bodyString(t, resp))
		}
	})
	t.Run("a pattern that does not match sends none", func(t *testing.T) {
		cfg := open
		cfg.ClientCerts = []ClientCert{{Host: "*.example.com", CertFile: good, KeyFile: goodKey}}
		_, err := newEngine(t, cfg).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "classifySend.EngineSendFailed")
	})
	t.Run("a certificate the server does not trust", func(t *testing.T) {
		cfg := open
		cfg.ClientCerts = []ClientCert{{Host: "127.0.0.1", CertFile: untrusted, KeyFile: untrustedKey}}
		_, err := newEngine(t, cfg).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "classifySend.EngineSendFailed")
	})
	t.Run("files that are missing", func(t *testing.T) {
		for _, cc := range []ClientCert{
			{Host: "127.0.0.1", CertFile: filepath.Join(dir, "none.crt"), KeyFile: goodKey},
			{Host: "127.0.0.1", CertFile: good, KeyFile: filepath.Join(dir, "none.key")},
		} {
			cfg := open
			cfg.ClientCerts = []ClientCert{cc}
			_, err := newEngine(t, cfg).Send(t.Context(), get(srv.URL))
			wantRoute(t, err, "loadClientCert.EngineClientCertReadFailed")
		}
	})
	t.Run("a key that is not the certificate's", func(t *testing.T) {
		cfg := open
		cfg.ClientCerts = []ClientCert{{Host: "127.0.0.1", CertFile: good, KeyFile: untrustedKey}}
		_, err := newEngine(t, cfg).Send(t.Context(), get(srv.URL))
		wantRoute(t, err, "loadClientCert.EngineClientCertInvalid")
	})
}

func TestEncryptedClientKey(t *testing.T) {
	ca := newCA(t)
	srv := tlsServer(t, ca, ca, okHandler)
	dir := t.TempDir()
	certFile, keyFile, _ := ca.issue(t, dir, "ada", true)
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(keyPEM)
	enc, err := x509.EncryptPEMBlock(rand.Reader, block.Type, block.Bytes, []byte("hunter2"), x509.PEMCipherAES256) //nolint:staticcheck // the engine reads this legacy encryption
	if err != nil {
		t.Fatal(err)
	}
	encFile := filepath.Join(dir, "enc.key")
	if err := os.WriteFile(encFile, pem.EncodeToMemory(enc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgWith := func(pass string) Config {
		return Config{VerifyTLS: boolp(false), ClientCerts: []ClientCert{{Host: "127.0.0.1", CertFile: certFile, KeyFile: encFile, Passphrase: pass}}}
	}
	resp := send(t, newEngine(t, cfgWith("hunter2")), get(srv.URL))
	if bodyString(t, resp) != "client:ada" {
		t.Fatalf("body %q", bodyString(t, resp))
	}
	_, err = newEngine(t, cfgWith("")).Send(t.Context(), get(srv.URL))
	wantRoute(t, err, "loadClientCert.EngineClientKeyEncrypted")
	_, err = newEngine(t, cfgWith("wrong")).Send(t.Context(), get(srv.URL))
	wantRoute(t, err, "loadClientCert.EngineClientKeyPassphrase")
}

func TestProxy(t *testing.T) {
	var gotURI, gotHost string
	proxy := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotURI, gotHost = r.RequestURI, r.Host
		_, _ = w.Write([]byte("via proxy"))
	})
	t.Run("an explicit proxy", func(t *testing.T) {
		// 192.0.2.1 is a documentation address (RFC 5737): the proxy answers, nothing is dialed
		resp := send(t, newEngine(t, Config{Proxy: proxy.URL}), get("http://192.0.2.1:8080/some/path?q=1"))
		if bodyString(t, resp) != "via proxy" || gotURI != "http://192.0.2.1:8080/some/path?q=1" || gotHost != "192.0.2.1:8080" {
			t.Fatalf("proxy got %q host %q, body %q", gotURI, gotHost, bodyString(t, resp))
		}
	})
	t.Run("a proxy that is not a URL", func(t *testing.T) {
		_, err := newEngine(t, Config{Proxy: "not a url"}).Send(t.Context(), get("http://192.0.2.1/"))
		wantRoute(t, err, "Engine.transport.EngineProxyInvalid")
	})
}
