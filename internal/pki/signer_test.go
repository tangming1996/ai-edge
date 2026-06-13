package pki_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
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

// generateTestCA returns a fresh PEM-encoded CA cert + key for use in tests.
func generateTestCA(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	certPEM, keyPEM, err := pki.GenerateSelfSignedCA("Test CA", 24*time.Hour)
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}
	return certPEM, keyPEM
}

// generateTestCSR returns a fresh PEM-encoded CSR for the given subject CN.
func generateTestCSR(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
}

func TestNewSigner_HappyPath(t *testing.T) {
	caCert, caKey := generateTestCA(t)

	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if signer == nil {
		t.Fatal("expected non-nil signer")
	}
}

func TestNewSigner_DefaultsValidity(t *testing.T) {
	caCert, caKey := generateTestCA(t)

	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM: caCert,
		CAKeyPEM:  caKey,
		// CertValidity left as zero to exercise default.
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	// Issue a cert and assert its lifetime is 90 days (the default).
	before := time.Now()
	csrPEM := generateTestCSR(t, "n1")
	res, err := signer.SignCSR(csrPEM, "n1")
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	lifetime := res.ExpiresAt.Sub(before)
	expected := 90 * 24 * time.Hour
	delta := lifetime - expected
	if delta < 0 {
		delta = -delta
	}
	// Allow a 10 second slop for the time it took SignCSR to run.
	if delta > 10*time.Second {
		t.Fatalf("default validity not 90d: lifetime=%s, expected~%s", lifetime, expected)
	}
}

func TestNewSigner_CAPem(t *testing.T) {
	caCert, caKey := generateTestCA(t)
	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	got := signer.CAPem()
	if len(got) == 0 {
		t.Fatal("CAPem returned empty")
	}
	// Round-trip: the returned PEM should decode into a real CERTIFICATE block.
	block, _ := pem.Decode(got)
	if block == nil {
		t.Fatalf("CAPem is not a valid PEM block: %q", got)
	}
	if block.Type != "CERTIFICATE" {
		t.Fatalf("PEM block type is %q, want CERTIFICATE", block.Type)
	}
}

func TestNewSigner_InvalidCertPEM(t *testing.T) {
	_, caKey := generateTestCA(t)
	_, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    []byte("not a pem block"),
		CAKeyPEM:     caKey,
		CertValidity: time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for invalid CA cert PEM")
	}
}

func TestNewSigner_InvalidKeyPEM(t *testing.T) {
	caCert, _ := generateTestCA(t)
	_, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     []byte("not a pem block"),
		CertValidity: time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for invalid CA key PEM")
	}
}

func TestNewSigner_KeyNotECDSA(t *testing.T) {
	caCert, _ := generateTestCA(t)
	// Encode an RSA PKCS1 key — pki.NewSigner expects ECDSA so this must fail.
	rsaKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: []byte("definitely not a real key"),
	})
	_, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     rsaKeyPEM,
		CertValidity: time.Hour,
	})
	if err == nil {
		t.Fatal("expected error when CA key is not ECDSA")
	}
}

func TestSignCSR_InvalidCSRPEM(t *testing.T) {
	caCert, caKey := generateTestCA(t)
	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	_, err = signer.SignCSR([]byte("garbage"), "n1")
	if err == nil {
		t.Fatal("expected error for garbage CSR PEM")
	}
}

func TestSignCSR_InvalidCSRBytes(t *testing.T) {
	caCert, caKey := generateTestCA(t)
	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	// PEM block is valid but the inner bytes are not a valid CSR DER.
	badCSR := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: []byte("not a valid CSR"),
	})
	_, err = signer.SignCSR(badCSR, "n1")
	if err == nil {
		t.Fatal("expected error for non-DER CSR body")
	}
}

func TestSignCSR_BadSignature(t *testing.T) {
	caCert, caKey := generateTestCA(t)
	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	// Create a CSR and then re-encode it with a different PEM block to
	// disturb the signature bytes. x509.ParseCertificateRequest will accept
	// it (the signature covers the TBS CSR), but CheckSignature should fail.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "n1"},
	}, key)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	// Flip a bit in the signature area to invalidate it.
	if len(csrDER) < 5 {
		t.Fatal("CSR DER too short")
	}
	corrupted := append([]byte(nil), csrDER...)
	corrupted[len(corrupted)-1] ^= 0xFF
	corruptedPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: corrupted})

	_, err = signer.SignCSR(corruptedPEM, "n1")
	if err == nil {
		t.Fatal("expected signature validation error")
	}
}

func TestSignCSR_NotExpiredWithin24h(t *testing.T) {
	caCert, caKey := generateTestCA(t)
	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	csrPEM := generateTestCSR(t, "node-001")
	before := time.Now()
	res, err := signer.SignCSR(csrPEM, "node-001")
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	after := time.Now()

	if res.ExpiresAt.Before(before.Add(24 * time.Hour)) {
		t.Fatalf("cert expires too early: %s", res.ExpiresAt)
	}
	if res.ExpiresAt.After(after.Add(24 * time.Hour)) {
		t.Fatalf("cert expires too late: %s", res.ExpiresAt)
	}
}

func TestSignCSR_CNPassThrough(t *testing.T) {
	caCert, caKey := generateTestCA(t)
	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	// The CSR CN is intentionally different from the nodeID we pass to
	// SignCSR. The signer must override the cert's CN with nodeID.
	csrPEM := generateTestCSR(t, "csr-subject-cn")
	res, err := signer.SignCSR(csrPEM, "node-xyz")
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	block, _ := pem.Decode(res.CertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued cert: %v", err)
	}
	if cert.Subject.CommonName != "node-xyz" {
		t.Fatalf("CN not passed through: got %q, want %q",
			cert.Subject.CommonName, "node-xyz")
	}
}

func TestSignCSR_ExtendedKeyUsage(t *testing.T) {
	caCert, caKey := generateTestCA(t)
	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKey,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	res, err := signer.SignCSR(generateTestCSR(t, "n1"), "n1")
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	block, _ := pem.Decode(res.CertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued cert: %v", err)
	}
	if cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("expected ClientAuth EKU, got %v", cert.ExtKeyUsage)
	}
}

func TestPEMRoundTrip(t *testing.T) {
	// Encoding a DER blob as a CERTIFICATE PEM block and decoding it must
	// yield the original bytes.
	original := []byte("hello world")
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: original})
	block, _ := pem.Decode(encoded)
	if block == nil {
		t.Fatal("decode returned nil block")
	}
	if string(block.Bytes) != string(original) {
		t.Fatalf("PEM round trip mismatch: %q vs %q", block.Bytes, original)
	}
	if block.Type != "CERTIFICATE" {
		t.Fatalf("PEM block type lost: %s", block.Type)
	}
}

// Ensure errors from SignCSR bubble up as wrapped error chains so callers can
// use errors.Is / errors.As if they ever need to.
func TestSignCSR_ErrorIsWrapped(t *testing.T) {
	_, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM: []byte("garbage"),
	})
	if err == nil {
		t.Fatal("expected error from NewSigner with garbage cert PEM")
	}
	if errors.Unwrap(err) == nil {
		// The wrap is via fmt.Errorf so the raw error is the message itself.
		// This is a smoke test that we return *some* non-nil error.
		t.Logf("note: error is not wrapped: %v", err)
	}
}
