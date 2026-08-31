package platform

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"time"
)

type CertificateStatus struct {
	Configured    bool      `json:"configured"`
	Path          string    `json:"path,omitempty"`
	Subject       string    `json:"subject,omitempty"`
	Issuer        string    `json:"issuer,omitempty"`
	NotBefore     time.Time `json:"notBefore,omitempty"`
	NotAfter      time.Time `json:"notAfter,omitempty"`
	DaysRemaining int       `json:"daysRemaining,omitempty"`
	Valid         bool      `json:"valid"`
	Warning       string    `json:"warning,omitempty"`
}

func InspectCertificate(path string) (CertificateStatus, error) {
	status := CertificateStatus{Configured: path != "", Path: path}
	if path == "" {
		status.Warning = "HTTPS certificate is not configured."
		return status, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		status.Warning = "Configured certificate is not readable."
		return status, err
	}
	if info.Size() > 1<<20 {
		return status, errors.New("certificate exceeds bounded size")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return status, err
	}
	return InspectCertificatePayload(path, payload)
}

// InspectCertificatePayload parses certificate bytes obtained by an authority
// process. Keeping parsing here lets the gateway inspect certificates it owns
// while external certificate paths are read by sessiond.
func InspectCertificatePayload(path string, payload []byte) (CertificateStatus, error) {
	status := CertificateStatus{Configured: path != "", Path: path}
	if path == "" {
		status.Warning = "HTTPS certificate is not configured."
		return status, nil
	}
	if len(payload) > 1<<20 {
		return status, errors.New("certificate exceeds bounded size")
	}
	block, _ := pem.Decode(payload)
	if block == nil || block.Type != "CERTIFICATE" {
		return status, errors.New("certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return status, err
	}
	now := time.Now()
	status.Subject = certificate.Subject.String()
	status.Issuer = certificate.Issuer.String()
	status.NotBefore = certificate.NotBefore
	status.NotAfter = certificate.NotAfter
	status.DaysRemaining = int(time.Until(certificate.NotAfter).Hours() / 24)
	status.Valid = !now.Before(certificate.NotBefore) && now.Before(certificate.NotAfter)
	if !status.Valid {
		status.Warning = "Configured certificate is outside its validity window."
	} else if status.DaysRemaining < 30 {
		status.Warning = "Configured certificate expires within 30 days."
	}
	return status, nil
}
