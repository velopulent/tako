package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

// testCertificatePEM returns a self-signed pair that expires at notAfter.
func testCertificatePEM(t *testing.T, name string, notAfter time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func leafOf(t *testing.T, certificate *tls.Certificate) *x509.Certificate {
	t.Helper()
	leaf, err := certificateLeaf(certificate)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func TestGeneratedCertificateRenewsBeforeExpiry(t *testing.T) {
	server, _ := newTestServer(t)
	certificatePEM, keyPEM := testCertificatePEM(t, "expiring", time.Now().Add(24*time.Hour))
	certificatePath := filepath.Join(server.config.DataDir, "tako.crt")
	keyPath := filepath.Join(server.config.DataDir, "tako.key")
	if err := os.WriteFile(certificatePath, certificatePEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.ensureCertificate(); err != nil {
		t.Fatal(err)
	}
	renewed, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	leaf := leafOf(t, &renewed)
	if leaf.Subject.CommonName == "expiring" || time.Until(leaf.NotAfter) < certificateRenewWindow {
		t.Fatalf("certificate was not renewed: %s until %s", leaf.Subject.CommonName, leaf.NotAfter)
	}
}

func TestConfiguredCertificateIsReadThroughSessiond(t *testing.T) {
	server, broker := newTestServer(t)
	server.config.Certificate = "/etc/letsencrypt/live/host/fullchain.pem"
	server.config.CertificateKey = "/etc/letsencrypt/live/host/privkey.pem"
	certificatePEM, keyPEM := testCertificatePEM(t, "console.example.com", time.Now().Add(90*24*time.Hour))
	broker.respond = func(operation string, _ json.RawMessage) (any, error) {
		if operation != "tls.certificate" {
			return nil, errors.New("unexpected operation " + operation)
		}
		return auth.CertificateMaterial{Certificate: string(certificatePEM), Key: string(keyPEM)}, nil
	}
	certificate, err := server.loadCertificate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if leaf := leafOf(t, certificate); leaf.Subject.CommonName != "console.example.com" {
		t.Fatalf("loaded %q", leaf.Subject.CommonName)
	}
	if call := broker.last(); call.credentials != (auth.Credentials{}) {
		t.Fatalf("certificate request carried credentials: %+v", call.credentials)
	}
	server.config.CertificateKey = ""
	if _, err := server.loadCertificate(context.Background()); err == nil {
		t.Fatal("a certificate without a key was accepted")
	}
}

func TestCertificateReloadFailureKeepsTheServingCertificate(t *testing.T) {
	certificatePEM, keyPEM := testCertificatePEM(t, "first", time.Now().Add(90*24*time.Hour))
	first, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	fail := false
	store := &certificateStore{load: func(context.Context) (*tls.Certificate, error) {
		if fail {
			return nil, errors.New("sessiond unavailable")
		}
		return &first, nil
	}}
	if _, err := store.get(nil); err == nil {
		t.Fatal("an empty store served a certificate")
	}
	if _, err := store.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err := store.Reload(context.Background()); err == nil {
		t.Fatal("failed reload reported success")
	}
	current, err := store.get(nil)
	if err != nil || leafOf(t, current).Subject.CommonName != "first" {
		t.Fatalf("serving certificate changed after a failed reload: %v", err)
	}
}
