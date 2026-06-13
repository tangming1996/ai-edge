package pki_test

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/pki"
)

func TestGenerateSelfSignedCA_HappyPath(t *testing.T) {
	certPEM, keyPEM, err := pki.GenerateSelfSignedCA("Root CA", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: %v", err)
	}
	if len(certPEM) == 0 {
		t.Fatal("empty cert PEM")
	}
	if len(keyPEM) == 0 {
		t.Fatal("empty key PEM")
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if !cert.IsCA {
		t.Fatal("CA cert does not have IsCA=true")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatal("CA cert missing CertSign key usage")
	}
}

func TestGenerateSelfSignedCA_SubjectPassThrough(t *testing.T) {
	cn := "My Custom CN " + strings.Repeat("x", 16)
	certPEM, _, err := pki.GenerateSelfSignedCA(cn, time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: %v", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if cert.Subject.CommonName != cn {
		t.Fatalf("CN mismatch: got %q, want %q", cert.Subject.CommonName, cn)
	}
}

func TestGenerateSelfSignedCA_Validity(t *testing.T) {
	validity := 7 * 24 * time.Hour
	certPEM, _, err := pki.GenerateSelfSignedCA("ca", validity)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: %v", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	now := time.Now()
	if cert.NotBefore.After(now) {
		t.Fatalf("NotBefore in the future: %s", cert.NotBefore)
	}
	expected := now.Add(validity)
	// Allow slop for time it took to generate.
	delta := cert.NotAfter.Sub(expected)
	if delta < 0 {
		delta = -delta
	}
	if delta > 5*time.Second {
		t.Fatalf("NotAfter off: got %s, expected ~%s", cert.NotAfter, expected)
	}
}

func TestGenerateSelfSignedCA_KeyUsableBySigner(t *testing.T) {
	// Round-trip: a self-signed CA must be loadable by pki.NewSigner and be
	// able to sign a CSR end-to-end. This exercises the implicit contract
	// between ca.go and signer.go.
	certPEM, keyPEM, err := pki.GenerateSelfSignedCA("Round Trip CA", time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: %v", err)
	}

	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    certPEM,
		CAKeyPEM:     keyPEM,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner from generated CA: %v", err)
	}

	res, err := signer.SignCSR(generateTestCSR(t, "edge-node-1"), "edge-node-1")
	if err != nil {
		t.Fatalf("SignCSR using generated CA: %v", err)
	}
	if res.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
}

func TestGenerateSelfSignedCA_SelfSigned(t *testing.T) {
	// A self-signed CA must verify its own signature.
	certPEM, _, err := pki.GenerateSelfSignedCA("SelfSigned", time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCA: %v", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Fatalf("self-signed signature invalid: %v", err)
	}
}
