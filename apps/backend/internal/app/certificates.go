package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"go.uber.org/zap"
)

const (
	// certificateRenewWindow replaces a generated certificate this long before
	// it expires, so a long-running gateway never serves an expired one.
	certificateRenewWindow = 30 * 24 * time.Hour
	// certificateReloadPeriod picks up renewed external certificates (for
	// example from an ACME client) without a restart.
	certificateReloadPeriod = 6 * time.Hour
)

// certificateStore holds the serving certificate and swaps it in place, so
// renewal and reloads never interrupt the listener.
type certificateStore struct {
	mu      sync.RWMutex
	current *tls.Certificate
	load    func(context.Context) (*tls.Certificate, error)
}

func (store *certificateStore) Reload(ctx context.Context) (*tls.Certificate, error) {
	certificate, err := store.load(ctx)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	store.current = certificate
	store.mu.Unlock()
	return certificate, nil
}

func (store *certificateStore) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.current == nil {
		return nil, errors.New("no certificate loaded")
	}
	return store.current, nil
}

// loadCertificate returns the configured certificate, or the generated
// self-signed one. Configured paths are read by sessiond as root, so keys can
// stay in root-only locations such as /etc/letsencrypt.
func (server *Server) loadCertificate(ctx context.Context) (*tls.Certificate, error) {
	if server.config.Certificate != "" || server.config.CertificateKey != "" {
		if server.config.Certificate == "" || server.config.CertificateKey == "" {
			return nil, errors.New("certificate and certificate_key must be configured together")
		}
		var material auth.CertificateMaterial
		if err := server.broker.Call(ctx, "tls.certificate", auth.Credentials{}, nil, &material); err != nil {
			return nil, fmt.Errorf("read configured certificate through sessiond: %w", err)
		}
		certificate, err := tls.X509KeyPair([]byte(material.Certificate), []byte(material.Key))
		material = auth.CertificateMaterial{}
		if err != nil {
			return nil, fmt.Errorf("parse configured certificate: %w", err)
		}
		return &certificate, nil
	}
	certificatePath, keyPath, err := server.ensureCertificate()
	if err != nil {
		return nil, err
	}
	certificate, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		return nil, err
	}
	return &certificate, nil
}

// ReloadCertificate reloads the serving certificate and keeps the current one
// when the reload fails. `systemctl reload tako` sends SIGHUP to trigger it.
func (server *Server) ReloadCertificate() {
	if server.config.Development {
		return
	}
	ctx, cancel := context.WithTimeout(server.lifetime, 30*time.Second)
	defer cancel()
	certificate, err := server.certificates.Reload(ctx)
	if err != nil {
		server.logger.Error("certificate reload failed; keeping the current certificate", zap.Error(err))
		return
	}
	server.logCertificate("certificate loaded", certificate)
}

func (server *Server) renewCertificates() {
	ticker := time.NewTicker(certificateReloadPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-server.lifetime.Done():
			return
		case <-ticker.C:
			server.ReloadCertificate()
		}
	}
}

func (server *Server) logCertificate(message string, certificate *tls.Certificate) {
	leaf, err := certificateLeaf(certificate)
	if err != nil {
		server.logger.Info(message)
		return
	}
	server.logger.Info(message, zap.String("subject", leaf.Subject.CommonName), zap.Strings("names", leaf.DNSNames), zap.Time("expires", leaf.NotAfter))
}

func certificateLeaf(certificate *tls.Certificate) (*x509.Certificate, error) {
	if certificate == nil || len(certificate.Certificate) == 0 {
		return nil, errors.New("empty certificate")
	}
	if certificate.Leaf != nil {
		return certificate.Leaf, nil
	}
	return x509.ParseCertificate(certificate.Certificate[0])
}

// generatedCertificateUsable reports whether a generated pair parses and stays
// valid past the renewal window.
func generatedCertificateUsable(certificatePath, keyPath string, now time.Time) bool {
	pair, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		return false
	}
	leaf, err := certificateLeaf(&pair)
	return err == nil && now.Add(certificateRenewWindow).Before(leaf.NotAfter)
}
