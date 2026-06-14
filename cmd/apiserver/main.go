package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/deployment"
	"github.com/edgeai-platform/ai-edge/internal/gateway"
	"github.com/edgeai-platform/ai-edge/internal/model"
	"github.com/edgeai-platform/ai-edge/internal/onboarding"
	"github.com/edgeai-platform/ai-edge/internal/pki"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"github.com/edgeai-platform/ai-edge/internal/task"
	buildversion "github.com/edgeai-platform/ai-edge/internal/version"
)

func main() {
	if buildversion.ShouldPrint(os.Args[1:]) {
		fmt.Println(buildversion.Info("apiserver"))
		return
	}

	cfg := store.Config{
		Host:     envOrDefault("DB_HOST", "localhost"),
		Port:     envOrDefaultInt("DB_PORT", 5432),
		User:     envOrDefault("DB_USER", "postgres"),
		Password: envOrDefault("DB_PASSWORD", "postgres"),
		DBName:   envOrDefault("DB_NAME", "edgeai"),
		SSLMode:  envOrDefault("DB_SSLMODE", "disable"),
	}

	db, err := store.New(cfg)
	if err != nil {
		log.Fatalf("apiserver: connect database: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("apiserver: close database: %v", closeErr)
		}
	}()

	signer, err := initSigner()
	if err != nil {
		log.Fatalf("apiserver: init signer: %v", err)
	}

	grpcAddr := envOrDefault("GRPC_ADDR", ":9091")
	httpAddr := envOrDefault("HTTP_ADDR", ":8081")

	// --- gRPC server ---
	grpcServer := grpc.NewServer()
	registerServices(grpcServer, db, signer)
	// Register the standard gRPC health service so kubelet's gRPC
	// liveness/readiness probes (manifests/helm/ai-edge/templates/
	// apiserver.yaml: grpc: { port: <grpcPort> }) can call
	// grpc.health.v1.Health.Check and get SERVING back. Without
	// this, the probe fails with "unknown service grpc.health.v1.
	// Health" because the server only has business services
	// registered.
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(grpcServer)

	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("apiserver: listen gRPC %s: %v", grpcAddr, err)
	}

	go func() {
		log.Printf("apiserver: gRPC listening on %s", grpcAddr)
		if serveErr := grpcServer.Serve(grpcLis); serveErr != nil {
			log.Fatalf("apiserver: gRPC serve: %v", serveErr)
		}
	}()

	// --- grpc-gateway HTTP reverse proxy ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gwMux := runtime.NewServeMux()
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	gwConn, err := grpc.NewClient(grpcAddr, dialOpts...)
	if err != nil {
		log.Fatalf("apiserver: dial gRPC for gateway: %v", err)
	}
	defer func() {
		if err := gwConn.Close(); err != nil {
			log.Printf("apiserver: close gateway connection: %v", err)
		}
	}()

	registerGatewayHandlers(ctx, gwMux, gwConn)

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: gwMux,
	}

	go func() {
		log.Printf("apiserver: HTTP/JSON listening on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("apiserver: HTTP serve: %v", err)
		}
	}()

	log.Printf("apiserver: starting %s", buildversion.String())

	// --- graceful shutdown ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("apiserver: received %s, shutting down...", sig)

	grpcServer.GracefulStop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("apiserver: HTTP shutdown: %v", err)
	}

	log.Println("apiserver: stopped")
}

func registerServices(s *grpc.Server, db *store.DB, signer *pki.Signer) {
	tokenStore := onboarding.NewTokenStore(db)
	bootstrapSvc := onboarding.NewBootstrapService(db, tokenStore, signer)

	pb.RegisterGatewayServiceServer(s, gateway.NewGatewayManagementService(db))
	pb.RegisterNodeServiceServer(s, onboarding.NewNodeGRPC(db))
	pb.RegisterIdentityServiceServer(s, onboarding.NewIdentityGRPC(db, bootstrapSvc))
	pb.RegisterNodeOnboardingServiceServer(s, onboarding.NewOnboardingGRPC(db, signer))
	pb.RegisterBootstrapTokenServiceServer(s, onboarding.NewTokenGRPC(db))
	pb.RegisterTaskServiceServer(s, task.NewService(db))
	pb.RegisterModelServiceServer(s, model.NewService(db))
	pb.RegisterDeploymentServiceServer(s, deployment.NewService(db))
}

func registerGatewayHandlers(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) {
	handlers := []func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error{
		pb.RegisterGatewayServiceHandler,
		pb.RegisterNodeServiceHandler,
		pb.RegisterIdentityServiceHandler,
		pb.RegisterNodeOnboardingServiceHandler,
		pb.RegisterBootstrapTokenServiceHandler,
		pb.RegisterTaskServiceHandler,
		pb.RegisterModelServiceHandler,
		pb.RegisterDeploymentServiceHandler,
	}
	for _, h := range handlers {
		if err := h(ctx, mux, conn); err != nil {
			log.Fatalf("apiserver: register gateway handler: %v", err)
		}
	}
}

func initSigner() (*pki.Signer, error) {
	certPath := envOrDefault("CA_CERT_PATH", "")
	keyPath := envOrDefault("CA_KEY_PATH", "")

	if certPath == "" || keyPath == "" {
		log.Println("apiserver: CA_CERT_PATH / CA_KEY_PATH not set, signer will not be available for production use")
		return newSelfSignedSigner()
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	return pki.NewSigner(pki.SignerConfig{
		CACertPEM: certPEM,
		CAKeyPEM:  keyPEM,
	})
}

func newSelfSignedSigner() (*pki.Signer, error) {
	certPEM, keyPEM, err := pki.GenerateSelfSignedCA("EdgeAI Dev CA", 10*365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	return pki.NewSigner(pki.SignerConfig{
		CACertPEM: certPEM,
		CAKeyPEM:  keyPEM,
	})
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
