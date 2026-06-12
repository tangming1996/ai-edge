package pki

import (
	"crypto/ecdsa"
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
type Signer struct {
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
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

	keyBlock, _ := pem.Decode(cfg.CAKeyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("pki: failed to decode CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
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
