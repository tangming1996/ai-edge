package pki_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/pki"
)

func TestSignCSR(t *testing.T) {
	caCert, caKey, err := pki.GenerateSelfSignedCA("Test Root CA", 10*365*24*time.Hour)
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}

	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate node key: %v", err)
	}

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "test-node"},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, nodeKey)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	result, err := signer.SignCSR(csrPEM, "node-001")
	if err != nil {
		t.Fatalf("sign CSR: %v", err)
	}

	if len(result.CertPEM) == 0 {
		t.Fatal("empty cert PEM")
	}
	if result.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
	if result.ExpiresAt.Before(time.Now()) {
		t.Fatal("cert already expired")
	}

	block, _ := pem.Decode(result.CertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued cert: %v", err)
	}
	if cert.Subject.CommonName != "node-001" {
		t.Fatalf("unexpected CN: %s", cert.Subject.CommonName)
	}
}
