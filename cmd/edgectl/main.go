package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

var (
	serverAddr string
	authToken  string
)

func main() {
	root := &cobra.Command{
		Use:   "edgectl",
		Short: "EdgeAI Runtime Platform CLI",
	}

	root.PersistentFlags().StringVar(&serverAddr, "server", envOrDefault("EDGECTL_SERVER", "localhost:9090"), "gRPC server address")
	root.PersistentFlags().StringVar(&authToken, "token", os.Getenv("EDGECTL_TOKEN"), "Bearer token for admin auth")

	root.AddCommand(tokenCmd(), nodeCmd(), deploymentCmd(), taskCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func dial() (*grpc.ClientConn, error) {
	return grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func authCtx() context.Context {
	ctx := context.Background()
	if authToken != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+authToken)
	}
	return ctx
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// --- token commands ---

func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage bootstrap tokens",
	}
	cmd.AddCommand(tokenCreateCmd(), tokenListCmd())
	return cmd
}

func tokenCreateCmd() *cobra.Command {
	var (
		gatewayID   string
		maxUses     int32
		expiresIn   string
		description string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a bootstrap token",
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close bootstrap token connection: %w", closeErr)
						return
					}
					log.Printf("edgectl: close bootstrap token connection: %v", closeErr)
				}
			}()

			dur, err := time.ParseDuration(expiresIn)
			if err != nil {
				return fmt.Errorf("invalid --expires-in: %w", err)
			}

			client := pb.NewBootstrapTokenServiceClient(conn)
			resp, err := client.CreateBootstrapToken(authCtx(), &pb.CreateBootstrapTokenRequest{
				GatewayId:        gatewayID,
				MaxUses:          maxUses,
				ExpiresInSeconds: int32(dur.Seconds()),
				Description:      description,
			})
			if err != nil {
				return err
			}

			tok := resp.GetTokenMetadata()
			fmt.Printf("Token ID:    %s\n", tok.GetId())
			fmt.Printf("Plaintext:   %s\n", resp.GetTokenPlain())
			fmt.Printf("Gateway:     %s\n", tok.GetGatewayId())
			fmt.Printf("Max Uses:    %d\n", tok.GetMaxUses())
			fmt.Printf("Expires At:  %s\n", tok.GetExpiresAt().AsTime().Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().StringVar(&gatewayID, "gateway", "", "Gateway ID")
	cmd.Flags().Int32Var(&maxUses, "max-uses", 10, "Maximum number of uses")
	cmd.Flags().StringVar(&expiresIn, "expires-in", "24h", "Token validity duration")
	cmd.Flags().StringVar(&description, "description", "", "Token description")
	_ = cmd.MarkFlagRequired("gateway")
	return cmd
}

func tokenListCmd() *cobra.Command {
	var gatewayID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bootstrap tokens",
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close bootstrap token connection: %w", closeErr)
						return
					}
					log.Printf("edgectl: close bootstrap token connection: %v", closeErr)
				}
			}()

			client := pb.NewBootstrapTokenServiceClient(conn)
			resp, err := client.ListBootstrapTokens(authCtx(), &pb.ListBootstrapTokensRequest{
				GatewayId: gatewayID,
			})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(w, "ID\tGATEWAY\tSTATUS\tUSED/MAX\tEXPIRES"); err != nil {
				return err
			}
			for _, t := range resp.GetTokens() {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\n",
					t.GetId(), t.GetGatewayId(), t.GetStatus(),
					t.GetUsedCount(), t.GetMaxUses(),
					t.GetExpiresAt().AsTime().Format(time.RFC3339)); err != nil {
					return err
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&gatewayID, "gateway", "", "Gateway ID")
	_ = cmd.MarkFlagRequired("gateway")
	return cmd
}

// --- node commands ---

func nodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage edge nodes",
	}
	cmd.AddCommand(nodeListCmd(), nodeRevokeCmd())
	return cmd
}

func nodeListCmd() *cobra.Command {
	var gatewayID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List nodes under a gateway",
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close node connection: %w", closeErr)
						return
					}
					log.Printf("edgectl: close node connection: %v", closeErr)
				}
			}()

			client := pb.NewNodeServiceClient(conn)
			resp, err := client.ListNodes(authCtx(), &pb.ListNodesRequest{
				GatewayId: gatewayID,
			})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(w, "ID\tGATEWAY\tSTATUS\tONLINE\tVERSION\tLAST SEEN"); err != nil {
				return err
			}
			for _, n := range resp.GetNodes() {
				lastSeen := "N/A"
				if n.GetLastSeenAt() != nil {
					lastSeen = n.GetLastSeenAt().AsTime().Format(time.RFC3339)
				}
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\t%s\n",
					n.GetId(), n.GetGatewayId(), n.GetStatus(),
					n.GetOnline(), n.GetAgentVersion(), lastSeen); err != nil {
					return err
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&gatewayID, "gateway", "", "Gateway ID")
	_ = cmd.MarkFlagRequired("gateway")
	return cmd
}

func nodeRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <node-id>",
		Short: "Revoke a node's identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close identity connection: %w", closeErr)
						return
					}
					log.Printf("edgectl: close identity connection: %v", closeErr)
				}
			}()

			client := pb.NewIdentityServiceClient(conn)
			_, err = client.RevokeIdentity(authCtx(), &pb.RevokeIdentityRequest{
				Id:     args[0],
				Reason: "revoked via edgectl",
			})
			if err != nil {
				return err
			}

			fmt.Printf("Node %s revoked successfully\n", args[0])
			return nil
		},
	}
	return cmd
}

// --- deployment commands ---

func deploymentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployment",
		Short: "Manage model deployments",
	}
	cmd.AddCommand(deploymentCreateCmd())
	return cmd
}

func deploymentCreateCmd() *cobra.Command {
	var (
		model     string
		gatewayID string
		runtime   string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a model deployment task",
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close deployment connection: %w", closeErr)
						return
					}
					log.Printf("edgectl: close deployment connection: %v", closeErr)
				}
			}()

			payload, _ := json.Marshal(map[string]string{
				"model":      model,
				"gateway_id": gatewayID,
				"runtime":    runtime,
			})

			client := pb.NewTaskServiceClient(conn)
			resp, err := client.CreateTask(authCtx(), &pb.CreateTaskRequest{
				Type:            "DeployModel",
				Scope:           pb.TaskScope_TASK_SCOPE_REGION,
				TargetGatewayId: gatewayID,
				Payload:         payload,
				CreatedBy:       "edgectl",
			})
			if err != nil {
				return err
			}

			fmt.Printf("Task ID:   %s\n", resp.GetTask().GetId())
			fmt.Printf("Type:      %s\n", resp.GetTask().GetType())
			fmt.Printf("Status:    %s\n", resp.GetTask().GetStatus())
			fmt.Printf("Gateway:   %s\n", resp.GetTask().GetTargetGatewayId())
			return nil
		},
	}
	cmd.Flags().StringVar(&model, "model", "", "Model name:version")
	cmd.Flags().StringVar(&gatewayID, "gateway", "", "Target gateway ID")
	cmd.Flags().StringVar(&runtime, "runtime", "auto", "Runtime type")
	_ = cmd.MarkFlagRequired("model")
	_ = cmd.MarkFlagRequired("gateway")
	return cmd
}

// --- task commands ---

func taskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}
	cmd.AddCommand(taskListCmd(), taskGetCmd())
	return cmd
}

func taskListCmd() *cobra.Command {
	var gatewayID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks for a gateway",
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close task connection: %w", closeErr)
						return
					}
					log.Printf("edgectl: close task connection: %v", closeErr)
				}
			}()

			client := pb.NewTaskServiceClient(conn)
			resp, err := client.ListTasks(authCtx(), &pb.ListTasksRequest{
				TargetGatewayId: gatewayID,
			})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tGATEWAY\tNODE\tCREATED"); err != nil {
				return err
			}
			for _, t := range resp.GetTasks() {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					t.GetId(), t.GetType(), t.GetStatus(),
					t.GetTargetGatewayId(), t.GetTargetNodeId(),
					t.GetCreatedAt().AsTime().Format(time.RFC3339)); err != nil {
					return err
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&gatewayID, "gateway", "", "Gateway ID")
	_ = cmd.MarkFlagRequired("gateway")
	return cmd
}

func taskGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <task-id>",
		Short: "Get task details",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close task connection: %w", closeErr)
						return
					}
					log.Printf("edgectl: close task connection: %v", closeErr)
				}
			}()

			client := pb.NewTaskServiceClient(conn)
			resp, err := client.GetTask(authCtx(), &pb.GetTaskRequest{
				Id: args[0],
			})
			if err != nil {
				return err
			}

			t := resp.GetTask()
			fmt.Printf("ID:        %s\n", t.GetId())
			fmt.Printf("Type:      %s\n", t.GetType())
			fmt.Printf("Scope:     %s\n", t.GetScope())
			fmt.Printf("Status:    %s\n", t.GetStatus())
			fmt.Printf("Gateway:   %s\n", t.GetTargetGatewayId())
			fmt.Printf("Node:      %s\n", t.GetTargetNodeId())
			fmt.Printf("Retries:   %d/%d\n", t.GetRetryCount(), t.GetMaxRetries())
			fmt.Printf("Created:   %s\n", t.GetCreatedAt().AsTime().Format(time.RFC3339))
			if len(t.GetPayload()) > 0 {
				fmt.Printf("Payload:   %s\n", string(t.GetPayload()))
			}
			if len(t.GetResult()) > 0 {
				fmt.Printf("Result:    %s\n", string(t.GetResult()))
			}
			return nil
		},
	}
	return cmd
}
