package pki

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// Signer issues certificates using a CA key pair. In production the CA private
// key MUST only reside in the Control Plane process.
//
// The CA key is stored as a crypto.PrivateKey so the signer accepts both RSA
// material (e.g. produced by the chart's sprig `genCA` helper, which emits
// PKCS#1 RSA) and ECDSA material (e.g. produced in-process by
// GenerateSelfSignedCA, which emits PKCS#1 EC). x509.CreateCertificate only
// requires a crypto.Signer, so any algorithm that implements that interface
// (RSA, ECDSA, Ed25519) works without further changes here.
type Signer struct {
	caCert *x509.Certificate
	caKey  crypto.PrivateKey
	caPEM  []byte

	certValidity time.Duration
}

// SignerConfig configures the Signer.
type SignerConfig struct {
	CACertPEM    []byte
	CAKeyPEM     []byte
	CertValidity time.Duration // default 90 days
}

// NewSigner creates a Signer from PEM-encoded CA cert and key.
func NewSigner(cfg SignerConfig) (*Signer, error) {
	block, _ := pem.Decode(cfg.CACertPEM)
	if block == nil {
		return nil, fmt.Errorf("pki: failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse CA cert: %w", err)
	}

	caKey, err := parsePrivateKeyPEM(cfg.CAKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("pki: parse CA key: %w", err)
	}

	validity := cfg.CertValidity
	if validity == 0 {
		validity = 90 * 24 * time.Hour
	}

	return &Signer{
		caCert:       caCert,
		caKey:        caKey,
		caPEM:        cfg.CACertPEM,
		certValidity: validity,
	}, nil
}

// parsePrivateKeyPEM decodes a PEM-encoded private key, accepting the three
// on-the-wire formats we expect to see for the apiserver CA:
//
//   - "RSA PRIVATE KEY"  (PKCS#1)   — produced by sprig's `genCA` chart helper
//   - "EC PRIVATE KEY"   (PKCS#1)   — produced in-process by GenerateSelfSignedCA
//   - "PRIVATE KEY"      (PKCS#8)   — generic envelope; algorithm is auto-detected
//
// Returns the raw crypto.PrivateKey so callers can pass it straight to
// x509.CreateCertificate, which only requires a crypto.Signer.
func parsePrivateKeyPEM(pemBytes []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("pki: failed to decode CA key PEM")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("pki: unsupported CA key PEM block type %q", block.Type)
	}
}

// CAPem returns the PEM-encoded CA certificate.
func (s *Signer) CAPem() []byte {
	return s.caPEM
}

// SignResult holds the output of a successful signing.
type SignResult struct {
	CertPEM     []byte
	Fingerprint string
	ExpiresAt   time.Time
}

// SignCSR parses a PEM-encoded CSR and issues a certificate.
func (s *Signer) SignCSR(csrPEM []byte, nodeID string) (*SignResult, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, fmt.Errorf("pki: failed to decode CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("pki: CSR signature invalid: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("pki: generate serial: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(s.certValidity)

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   nodeID,
			Organization: []string{"EdgeAI Platform"},
		},
		NotBefore:             now,
		NotAfter:              expiresAt,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, s.caCert, csr.PublicKey, s.caKey)
	if err != nil {
		return nil, fmt.Errorf("pki: create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	hash := sha256.Sum256(certDER)
	fingerprint := hex.EncodeToString(hash[:])

	return &SignResult{
		CertPEM:     certPEM,
		Fingerprint: fingerprint,
		ExpiresAt:   expiresAt,
	}, nil
}
