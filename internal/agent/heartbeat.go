package agent

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

// RunHeartbeat sends periodic heartbeats until ctx is cancelled.
func RunHeartbeat(ctx context.Context, conn *grpc.ClientConn, nodeID string, cfg *Config) {
	client := pb.NewAgentServiceClient(conn)
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("agent: heartbeat loop stopped")
			return
		case <-ticker.C:
			sendHeartbeat(ctx, client, nodeID, cfg.AgentVersion)
		}
	}
}

func sendHeartbeat(ctx context.Context, client pb.AgentServiceClient, nodeID, version string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := client.ReportHeartbeat(ctx, &pb.ReportHeartbeatRequest{
		NodeId:       nodeID,
		AgentVersion: version,
		Timestamp:    timestamppb.Now(),
	})
	if err != nil {
		log.Println("agent: heartbeat failed:", err)
		return
	}
	log.Println("agent: heartbeat sent")
}
