package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/edgeai-platform/ai-edge/internal/agent"
	edgeruntime "github.com/edgeai-platform/ai-edge/internal/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	configPath := flag.String("config", "/etc/edge-agent/config.json", "Path to edge-agent config file")
	flag.Parse()

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("edge-agent: load config: %v", err)
	}
	if mkdirErr := os.MkdirAll(cfg.DataDir, 0755); mkdirErr != nil {
		log.Fatalf("edge-agent: prepare data dir: %v", mkdirErr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	identity, err := agent.LoadOrBootstrap(ctx, cfg)
	if err != nil {
		log.Fatalf("edge-agent: load/bootstrap identity: %v", err)
	}

	useMTLS := envOrDefaultBool("EDGE_USE_MTLS", false)
	dialOpts := []grpc.DialOption{}
	if useMTLS {
		dialOpts = append(dialOpts, identity.MTLSDialOption())
	} else {
		log.Println("edge-agent: EDGE_USE_MTLS is disabled, using insecure gRPC connection")
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(cfg.GatewayAddr, dialOpts...)
	if err != nil {
		log.Fatalf("edge-agent: dial gateway: %v", err)
	}
	defer conn.Close()

	runtimeManager := edgeruntime.NewManager()
	runtimeManager.Register(edgeruntime.NewLlamaCppAdapter(cfg.DataDir))

	downloader := agent.NewDownloader(agent.DownloaderConfig{
		GatewayBaseURL: deriveGatewayHTTPAddr(cfg, useMTLS),
		DataDir:        cfg.DataDir,
		TLSConfig:      downloaderTLSConfig(identity, useMTLS),
	})

	executorMux := agent.NewExecutorMux()
	executorMux.Register(agent.NewModelExecutor(downloader, cfg.DataDir), "InstallModel", "DeleteModel")
	executorMux.Register(agent.NewRuntimeExecutor(runtimeManager), "StartRuntime", "StopRuntime", "RestartRuntime", "UpgradeRuntime")
	executorMux.Register(agent.NewUpdater(agent.UpdaterConfig{
		CurrentVersion: cfg.AgentVersion,
		BinaryPath:     envOrDefault("EDGE_AGENT_BINARY_PATH", "/usr/local/bin/edge-agent"),
		DataDir:        cfg.DataDir,
		Downloader:     downloader,
	}), agent.TaskTypeUpgradeAgent)
	executorMux.Register(agent.NewLogCollector(identity.NodeID, deriveGatewayHTTPAddr(cfg, useMTLS)), agent.TaskTypeCollectLogs)

	taskRunner := agent.NewTaskRunner(conn, identity.NodeID, executorMux, agent.TaskRunnerConfig{
		DataDir: cfg.DataDir,
	})
	renewer := agent.NewCertRenewer(agent.CertRenewerConfig{
		Config:   cfg,
		Identity: identity,
		Conn:     conn,
	})

	go agent.RunHeartbeat(ctx, conn, identity.NodeID, cfg)
	go taskRunner.Run(ctx)
	go renewer.Run(ctx)

	log.Printf("edge-agent: started node_id=%s gateway=%s", identity.NodeID, cfg.GatewayAddr)
	<-ctx.Done()
	log.Println("edge-agent: stopping...")
	log.Println("edge-agent: stopped")
}

func downloaderTLSConfig(identity *agent.Identity, useMTLS bool) *tls.Config {
	if !useMTLS {
		return nil
	}
	return &tls.Config{
		Certificates: []tls.Certificate{identity.Cert},
		RootCAs:      identity.CAPool,
		MinVersion:   tls.VersionTLS12,
	}
}

func deriveGatewayHTTPAddr(cfg *agent.Config, useMTLS bool) string {
	if cfg.GatewayHTTPAddr != "" {
		return strings.TrimRight(cfg.GatewayHTTPAddr, "/")
	}

	host, _, err := net.SplitHostPort(cfg.GatewayAddr)
	if err != nil {
		host = cfg.GatewayAddr
	}
	scheme := "http"
	if useMTLS {
		scheme = "https"
	}
	return scheme + "://" + host + ":8081"
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	case "":
		return def
	default:
		return def
	}
}
