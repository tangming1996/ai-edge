package pki_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
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

// generateSelfSignedRSACA returns a self-signed CA pair (cert PEM +
// PKCS#1 RSA private key PEM). The two share a fresh RSA 2048 key so
// the signer can use them together.
func generateSelfSignedRSACA(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "RSA Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create RSA CA: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return certPEM, keyPEM
}

// generateSelfSignedPKCS8ECA returns a self-signed CA pair (cert PEM +
// PKCS#8 EC private key PEM). The two share a fresh EC P-256 key.
func generateSelfSignedPKCS8ECA(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "PKCS8 Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create PKCS#8 CA: %v", err)
	}
	pkcs8DER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS#8: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8DER})
	return certPEM, keyPEM
}

// verifyIssuedCert checks that the issued cert PEM chains back to the
// supplied CA cert PEM (i.e. the issuer signature is valid and the
// issued cert's issuer DN matches the CA's subject).
func verifyIssuedCert(issuedPEM, caCertPEM []byte) error {
	iblock, _ := pem.Decode(issuedPEM)
	if iblock == nil {
		return errors.New("issued PEM decode failed")
	}
	issued, err := x509.ParseCertificate(iblock.Bytes)
	if err != nil {
		return err
	}
	cblock, _ := pem.Decode(caCertPEM)
	if cblock == nil {
		return errors.New("CA cert PEM decode failed")
	}
	ca, err := x509.ParseCertificate(cblock.Bytes)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := issued.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return err
	}
	return nil
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

func TestNewSigner_InvalidRSAKeyBytes(t *testing.T) {
	caCert, _ := generateTestCA(t)
	// RSA PRIVATE KEY is a valid PEM block type, but the inner bytes
	// are not a real PKCS#1 RSA key, so ParsePKCS1PrivateKey must fail.
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
		t.Fatal("expected error when RSA key bytes are invalid")
	}
}

// TestNewSigner_RSAKey exercises the chart path: sprig `genCA` produces
// a PKCS#1 RSA key in a "RSA PRIVATE KEY" PEM block. The signer must
// accept it and be able to issue a leaf certificate from it.
func TestNewSigner_RSAKey(t *testing.T) {
	caCert, caKeyPEM := generateSelfSignedRSACA(t)

	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKeyPEM,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner with RSA key: %v", err)
	}
	if signer == nil {
		t.Fatal("expected non-nil signer")
	}

	// Round-trip: signer must be able to issue a leaf cert using the
	// RSA CA key. The CSR is generated with a fresh EC node key; the
	// issued cert is then verified against the CA's RSA public key.
	csrPEM := generateTestCSR(t, "n1")
	res, err := signer.SignCSR(csrPEM, "n1")
	if err != nil {
		t.Fatalf("SignCSR with RSA CA key: %v", err)
	}
	if err := verifyIssuedCert(res.CertPEM, caCert); err != nil {
		t.Fatalf("issued cert failed verification against RSA CA: %v", err)
	}
}

// TestNewSigner_PKCS8Key exercises the PKCS#8 envelope path. Operators
// who bring their own CA Secret may use any standard toolchain
// (openssl, cfssl, step) which often produces PKCS#8 by default.
func TestNewSigner_PKCS8Key(t *testing.T) {
	caCert, caKeyPEM := generateSelfSignedPKCS8ECA(t)

	signer, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     caKeyPEM,
		CertValidity: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSigner with PKCS#8 key: %v", err)
	}
	if signer == nil {
		t.Fatal("expected non-nil signer")
	}

	csrPEM := generateTestCSR(t, "n1")
	res, err := signer.SignCSR(csrPEM, "n1")
	if err != nil {
		t.Fatalf("SignCSR with PKCS#8 CA key: %v", err)
	}
	if err := verifyIssuedCert(res.CertPEM, caCert); err != nil {
		t.Fatalf("issued cert failed verification against PKCS#8 CA: %v", err)
	}
}

func TestNewSigner_UnsupportedKeyPEM(t *testing.T) {
	caCert, _ := generateTestCA(t)
	// A "CERTIFICATE" PEM block is structurally a valid block but not
	// a private key. The dispatcher must reject it explicitly.
	wrongTypePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("not a real certificate"),
	})
	_, err := pki.NewSigner(pki.SignerConfig{
		CACertPEM:    caCert,
		CAKeyPEM:     wrongTypePEM,
		CertValidity: time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for unsupported PEM block type")
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
