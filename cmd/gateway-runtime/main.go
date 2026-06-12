package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/gateway"
	"github.com/edgeai-platform/ai-edge/internal/observability"
	"github.com/edgeai-platform/ai-edge/internal/store"
	buildversion "github.com/edgeai-platform/ai-edge/internal/version"
)

func main() {
	if buildversion.ShouldPrint(os.Args[1:]) {
		fmt.Println(buildversion.Info("gateway-runtime"))
		return
	}

	gatewayID := envOrDefault("GATEWAY_ID", "")
	if gatewayID == "" {
		log.Fatal("gateway-runtime: GATEWAY_ID is required")
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
		log.Fatalf("gateway-runtime: connect database: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("gateway-runtime: close database: %v", closeErr)
		}
	}()

	controlPlaneAddr := envOrDefault("CONTROL_PLANE_ADDR", "localhost:9090")
	upstreamConn, err := grpc.NewClient(controlPlaneAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("gateway-runtime: dial control plane: %v", err)
	}
	defer func() {
		if closeErr := upstreamConn.Close(); closeErr != nil {
			log.Printf("gateway-runtime: close control plane connection: %v", closeErr)
		}
	}()

	identityCache := gateway.NewIdentityCache(gateway.IdentityCacheConfig{
		DB:  db,
		TTL: envOrDefaultDuration("IDENTITY_CACHE_TTL", 30*time.Second),
	})
	reporter := observability.NewReporter(db)
	dispatcher := gateway.NewDispatcher(gateway.DispatcherConfig{
		GatewayID:     gatewayID,
		DB:            db,
		IdentityCache: identityCache,
		ClaimDuration: envOrDefaultDuration("TASK_CLAIM_DURATION", 5*time.Minute),
	})
	onboardingProxy := gateway.NewOnboardingProxy(upstreamConn, gatewayID)
	agentService := gateway.NewAgentService(db, gatewayID, reporter)

	serverOpts, err := grpcServerOptions(
		gatewayID,
		identityCache,
		envOrDefault("GATEWAY_TLS_CERT_PATH", ""),
		envOrDefault("GATEWAY_TLS_KEY_PATH", ""),
		envOrDefault("GATEWAY_CA_CERT_PATH", ""),
	)
	if err != nil {
		log.Fatalf("gateway-runtime: TLS config: %v", err)
	}
	grpcServer := grpc.NewServer(serverOpts...)
	pb.RegisterNodeOnboardingServiceServer(grpcServer, onboardingProxy)
	pb.RegisterGatewaySyncServiceServer(grpcServer, dispatcher)
	pb.RegisterAgentServiceServer(grpcServer, agentService)

	grpcAddr := envOrDefault("GRPC_ADDR", ":9443")
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("gateway-runtime: listen gRPC %s: %v", grpcAddr, err)
	}

	httpMux := http.NewServeMux()
	artifactHandler := gateway.NewArtifactHandler(gateway.ArtifactHandlerConfig{
		DB:              db,
		GatewayID:       gatewayID,
		CacheDir:        envOrDefault("CACHE_DIR", "./var/lib/gateway-runtime/cache"),
		UpstreamBaseURL: envOrDefault("UPSTREAM_BASE_URL", "http://localhost:9000"),
	})
	artifactHandler.RegisterRoutes(httpMux)
	httpMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	httpMux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		for name, value := range reporter.Registry().Snapshot() {
			safeName := strings.NewReplacer(".", "_", "-", "_", ":", "_").Replace(name)
			_, _ = w.Write([]byte(safeName + " " + strconv.FormatFloat(value, 'f', -1, 64) + "\n"))
		}
	})

	httpAddr := envOrDefault("HTTP_ADDR", ":8081")
	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: httpMux,
	}

	connectivityMonitor := gateway.NewConnectivityMonitor(gateway.ConnectivityMonitorConfig{
		CloudHealthURL: envOrDefault("CLOUD_HEALTH_URL", ""),
		CheckInterval:  envOrDefaultDuration("CONNECTIVITY_CHECK_INTERVAL", 10*time.Second),
		Timeout:        envOrDefaultDuration("CONNECTIVITY_TIMEOUT", 5*time.Second),
		OnRecover:      gateway.NewIncrementalSyncer(gatewayID, identityCache).Sync,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go connectivityMonitor.Run(ctx)
	go func() {
		log.Printf("gateway-runtime: gRPC listening on %s", grpcAddr)
		if err := grpcServer.Serve(grpcLis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("gateway-runtime: gRPC serve: %v", err)
		}
	}()
	go func() {
		log.Printf("gateway-runtime: HTTP listening on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("gateway-runtime: HTTP serve: %v", err)
		}
	}()

	log.Printf("gateway-runtime: started gateway_id=%s control_plane=%s %s", gatewayID, controlPlaneAddr, buildversion.String())
	<-ctx.Done()
	log.Println("gateway-runtime: shutting down...")

	grpcServer.GracefulStop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("gateway-runtime: HTTP shutdown: %v", err)
	}
	log.Println("gateway-runtime: stopped")
}

func grpcServerOptions(gatewayID string, cache *gateway.IdentityCache, certPath, keyPath, caPath string) ([]grpc.ServerOption, error) {
	var opts []grpc.ServerOption
	if certPath == "" || keyPath == "" || caPath == "" {
		log.Println("gateway-runtime: TLS paths not fully configured, starting gRPC server without mTLS")
		return opts, nil
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("failed to parse gateway CA cert")
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS12,
	}
	opts = append(opts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	opts = append(opts, grpc.UnaryInterceptor(gateway.NewAuthInterceptor(gateway.AuthInterceptorConfig{
		Cache:     cache,
		GatewayID: gatewayID,
		SkipMethods: []string{
			"/edge.ai.api.v1.NodeOnboardingService/Bootstrap",
			"/edge.ai.api.v1.GatewaySyncService/PushRegionalTask",
			"/edge.ai.api.v1.GatewaySyncService/SyncGatewayStatus",
			"/edge.ai.api.v1.GatewaySyncService/NotifyIdentityEvent",
		},
	})))
	return opts, nil
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

func envOrDefaultDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
