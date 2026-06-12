package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"time"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"google.golang.org/grpc"
)

const defaultRenewThreshold = 30 * 24 * time.Hour

// CertRenewer periodically checks certificate expiry and triggers renewal.
type CertRenewer struct {
	cfg            *Config
	identity       *Identity
	conn           *grpc.ClientConn
	checkInterval  time.Duration
	renewThreshold time.Duration
}

// CertRenewerConfig configures the CertRenewer.
type CertRenewerConfig struct {
	Config         *Config
	Identity       *Identity
	Conn           *grpc.ClientConn
	CheckInterval  time.Duration
	RenewThreshold time.Duration
}

// NewCertRenewer creates a CertRenewer.
func NewCertRenewer(cfg CertRenewerConfig) *CertRenewer {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = 12 * time.Hour
	}
	threshold := cfg.RenewThreshold
	if threshold == 0 {
		threshold = defaultRenewThreshold
	}
	return &CertRenewer{
		cfg:            cfg.Config,
		identity:       cfg.Identity,
		conn:           cfg.Conn,
		checkInterval:  interval,
		renewThreshold: threshold,
	}
}

// Run starts the periodic certificate check loop.
func (r *CertRenewer) Run(ctx context.Context) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("agent: cert renewer stopped")
			return
		case <-ticker.C:
			r.checkAndRenew(ctx)
		}
	}
}

func (r *CertRenewer) checkAndRenew(ctx context.Context) {
	leaf := r.leafCert()
	if leaf == nil {
		log.Println("agent: cert renewer: no leaf certificate found")
		return
	}

	remaining := time.Until(leaf.NotAfter)
	if remaining > r.renewThreshold {
		log.Printf("agent: cert renewer: certificate valid for %s, no renewal needed", remaining.Truncate(time.Hour))
		return
	}

	log.Printf("agent: cert renewer: certificate expires in %s (threshold %s), renewing...",
		remaining.Truncate(time.Hour), r.renewThreshold)

	if err := r.renew(ctx); err != nil {
		log.Printf("agent: cert renewer: RENEWAL FAILED: %v", err)
		return
	}

	log.Println("agent: cert renewer: renewal successful")
}

func (r *CertRenewer) renew(ctx context.Context) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   r.identity.NodeID,
			Organization: []string{"EdgeAI Agent"},
		},
	}, key)
	if err != nil {
		return fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	client := pb.NewNodeOnboardingServiceClient(r.conn)
	renewCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := client.Renew(renewCtx, &pb.RenewRequest{
		CsrPem: csrPEM,
	})
	if err != nil {
		return fmt.Errorf("renew RPC: %w", err)
	}

	keyPEM := pemEncodeECKey(key)
	if err := os.WriteFile(r.cfg.KeyPath(), keyPEM, 0600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	if err := os.WriteFile(r.cfg.CertPath(), resp.CertificatePem, 0644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if len(resp.CaPem) > 0 {
		if err := os.WriteFile(r.cfg.CAPath(), resp.CaPem, 0644); err != nil {
			return fmt.Errorf("write CA: %w", err)
		}
	}

	cert, err := tls.X509KeyPair(resp.CertificatePem, keyPEM)
	if err != nil {
		return fmt.Errorf("parse renewed cert: %w", err)
	}
	r.identity.Cert = cert

	return nil
}

func (r *CertRenewer) leafCert() *x509.Certificate {
	if len(r.identity.Cert.Certificate) == 0 {
		return nil
	}
	leaf, err := x509.ParseCertificate(r.identity.Cert.Certificate[0])
	if err != nil {
		log.Printf("agent: cert renewer: parse leaf cert: %v", err)
		return nil
	}
	return leaf
}
