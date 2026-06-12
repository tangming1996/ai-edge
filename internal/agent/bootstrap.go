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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

// Identity holds the result of a successful bootstrap.
type Identity struct {
	NodeID string
	Cert   tls.Certificate
	CACert *x509.Certificate
	CAPool *x509.CertPool
}

// MTLSDialOption returns gRPC dial credentials for mTLS.
func (id *Identity) MTLSDialOption() grpc.DialOption {
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{id.Cert},
		RootCAs:      id.CAPool,
	}
	return grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))
}

// LoadOrBootstrap tries to load existing identity from disk; if not found it
// performs a fresh bootstrap against the gateway.
func LoadOrBootstrap(ctx context.Context, cfg *Config) (*Identity, error) {
	id, err := loadIdentity(cfg)
	if err == nil {
		log.Println("agent: loaded existing identity, node_id =", id.NodeID)
		return id, nil
	}

	log.Println("agent: no existing identity, performing bootstrap...")
	return bootstrap(ctx, cfg)
}

// loadIdentity reads previously persisted key/cert/ca from DataDir.
func loadIdentity(cfg *Config) (*Identity, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertPath(), cfg.KeyPath())
	if err != nil {
		return nil, err
	}

	caPEM, err := os.ReadFile(cfg.CAPath())
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("agent: failed to parse CA cert")
	}

	block, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("agent: parse CA cert: %w", err)
	}

	nodeID, err := os.ReadFile(cfg.NodeIDPath())
	if err != nil {
		return nil, err
	}

	return &Identity{
		NodeID: string(nodeID),
		Cert:   cert,
		CACert: caCert,
		CAPool: pool,
	}, nil
}

// bootstrap generates a key pair, sends a CSR, and persists the resulting
// identity to disk.
func bootstrap(ctx context.Context, cfg *Config) (*Identity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("agent: generate key: %w", err)
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{Organization: []string{"EdgeAI Agent"}},
	}, key)
	if err != nil {
		return nil, fmt.Errorf("agent: create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	conn, err := grpc.NewClient(cfg.GatewayAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("agent: dial gateway: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("agent: close bootstrap connection: %v", err)
		}
	}()

	client := pb.NewNodeOnboardingServiceClient(conn)
	resp, err := client.Bootstrap(ctx, &pb.BootstrapRequest{
		Token:     cfg.Token,
		GatewayId: cfg.GatewayID,
		CsrPem:    csrPEM,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: bootstrap RPC: %w", err)
	}

	if err := persistIdentity(cfg, key, resp); err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair(resp.CertificatePem, pemEncodeECKey(key))
	if err != nil {
		return nil, fmt.Errorf("agent: parse issued cert: %w", err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(resp.CaPem)
	block, _ := pem.Decode(resp.CaPem)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("agent: parse CA cert: %w", err)
	}

	log.Println("agent: bootstrap complete, node_id =", resp.NodeId)
	return &Identity{
		NodeID: resp.NodeId,
		Cert:   cert,
		CACert: caCert,
		CAPool: pool,
	}, nil
}

func persistIdentity(cfg *Config, key *ecdsa.PrivateKey, resp *pb.BootstrapResponse) error {
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return fmt.Errorf("agent: mkdir %s: %w", cfg.DataDir, err)
	}

	keyPEM := pemEncodeECKey(key)
	if err := os.WriteFile(cfg.KeyPath(), keyPEM, 0600); err != nil {
		return fmt.Errorf("agent: write key: %w", err)
	}
	if err := os.WriteFile(cfg.CertPath(), resp.CertificatePem, 0644); err != nil {
		return fmt.Errorf("agent: write cert: %w", err)
	}
	if err := os.WriteFile(cfg.CAPath(), resp.CaPem, 0644); err != nil {
		return fmt.Errorf("agent: write CA: %w", err)
	}
	if err := os.WriteFile(cfg.NodeIDPath(), []byte(resp.NodeId), 0644); err != nil {
		return fmt.Errorf("agent: write node-id: %w", err)
	}
	return nil
}

func pemEncodeECKey(key *ecdsa.PrivateKey) []byte {
	der, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}
