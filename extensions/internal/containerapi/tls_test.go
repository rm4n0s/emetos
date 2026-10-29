package containerapi_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

func TestLoadTLSReadsTheCertificates(t *testing.T) {
	dir := t.TempDir()
	caPEM, certPEM, keyPEM := containerapitest.Certificates(t)
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		files map[string][]byte
		route string
		certs int
		roots bool
	}{
		{name: "the whole set", files: map[string][]byte{"ca.pem": caPEM, "cert.pem": certPEM, "key.pem": keyPEM}, certs: 1, roots: true},
		{name: "only a CA", files: map[string][]byte{"ca.pem": caPEM}, roots: true},
		{name: "no files: the system's roots", files: map[string][]byte{}},
		{name: "a CA that is not a certificate", files: map[string][]byte{"ca.pem": []byte("nonsense")}, route: "ParseHost->loadTLS.ContainerAPITLSFailed"},
		{name: "a certificate without its key", files: map[string][]byte{"cert.pem": certPEM}, route: "ParseHost->loadTLS.ContainerAPITLSFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range []string{"ca.pem", "cert.pem", "key.pem"} {
				if err := os.Remove(filepath.Join(dir, n)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			for n, d := range tc.files {
				write(n, d)
			}
			ep, err := containerapi.ParseHost("tcp://127.0.0.1:2376", containerapi.TLSOptions{Verify: true, CertDir: dir})
			if tc.route != "" {
				wantRoute(t, err, tc.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(ep.TLS.Certificates) != tc.certs || (ep.TLS.RootCAs != nil) != tc.roots || ep.TLS.ServerName != "127.0.0.1" {
				t.Errorf("TLS = %d certificates, roots %v, server name %q", len(ep.TLS.Certificates), ep.TLS.RootCAs != nil, ep.TLS.ServerName)
			}
		})
	}
}

func TestLoadTLSReportsAnUnreadableCA(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "ca.pem"), 0o700); err != nil { // a folder where the file should be
		t.Fatal(err)
	}
	_, err := containerapi.ParseHost("tcp://127.0.0.1:2376", containerapi.TLSOptions{Verify: true, CertDir: dir})
	wantRoute(t, err, "ParseHost->loadTLS.ContainerAPITLSFailed")
}
