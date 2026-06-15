package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

// SelfRegisterConfig captures the inputs the gateway-runtime needs in
// order to register itself with the apiserver on startup. The config is
// deliberately small and value-typed so callers (cmd/gateway-runtime) can
// build it from env vars with no extra plumbing.
type SelfRegisterConfig struct {
	// GatewayID is the unique identifier of this gateway. The
	// implementation uses it as the gateway name when the operator
	// has not supplied an explicit SelfRegisterName.
	GatewayID string
	// Region is optional; surfaces in the apiserver's gateway list.
	Region string
	// Endpoint is the publicly-reachable mTLS address edge-agents use
	// to reach this gateway-runtime (host:port). Optional.
	Endpoint string
	// Name, when non-empty, overrides GatewayID as the gateway name.
	// Useful when the operator wants a stable human-friendly name
	// decoupled from the cluster node name.
	Name string
	// Labels are merged into the gateway's metadata.
	Labels map[string]string
	// Timeout is the overall budget for the registration RPC. Defaults
	// to 30s when zero.
	Timeout time.Duration
}

// SelfRegisterResult is the data the runtime logs / surfaces to its
// caller after a successful self-registration. The GatewayID is the
// apiserver-assigned UUID; on idempotent re-registration of an
// already-existing gateway it is the existing record's id.
type SelfRegisterResult struct {
	// GatewayID is the apiserver-assigned UUID.
	GatewayID string
	// Name is the unique name this gateway was registered under.
	Name string
	// AlreadyExisted is true when the call short-circuited because a
	// gateway with the same name was already present.
	AlreadyExisted bool
}

// SelfRegister creates (or finds) a gateway row in the apiserver using
// the given upstream connection. It is intended to be called once at
// gateway-runtime startup, after the upstream gRPC connection is up but
// before the gRPC server starts serving traffic.
//
// The function is idempotent on gateway NAME: a second invocation with
// the same name returns the existing gateway_id and
// SelfRegisterResult.AlreadyExisted=true instead of erroring. Any other
// gRPC error is wrapped with the gateway name so the runtime log makes
// it obvious which node failed to register.
func SelfRegister(ctx context.Context, upstream *grpc.ClientConn, cfg SelfRegisterConfig) (*SelfRegisterResult, error) {
	if cfg.GatewayID == "" {
		return nil, fmt.Errorf("gateway self-register: gateway_id is required")
	}

	name := cfg.Name
	if name == "" {
		name = cfg.GatewayID
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := pb.NewGatewayServiceClient(upstream)
	req := &pb.CreateGatewayRequest{
		Name:     name,
		Region:   cfg.Region,
		Endpoint: cfg.Endpoint,
	}
	if len(cfg.Labels) > 0 {
		req.Labels = &pb.Labels{Items: cfg.Labels}
	}

	resp, err := client.CreateGateway(callCtx, req)
	if err == nil {
		gw := resp.GetGateway()
		log.Printf("gateway-runtime: self-registered as %q id=%s", gw.GetName(), gw.GetId())
		return &SelfRegisterResult{
			GatewayID:      gw.GetId(),
			Name:           gw.GetName(),
			AlreadyExisted: false,
		}, nil
	}

	// Idempotency: gateway NAME has a UNIQUE constraint server-side.
	// If the row is already there, locate it by listing and return
	// the existing id so restarts of the same node are safe.
	if status.Code(err) != codes.AlreadyExists {
		return nil, fmt.Errorf("gateway self-register %q: %w", name, err)
	}

	existing, lookupErr := findGatewayByName(callCtx, client, name)
	if lookupErr != nil {
		return nil, fmt.Errorf("gateway self-register %q already exists but lookup failed: %w", name, lookupErr)
	}
	log.Printf("gateway-runtime: self-register %q already exists, reusing id=%s", name, existing.GetId())
	return &SelfRegisterResult{
		GatewayID:      existing.GetId(),
		Name:           existing.GetName(),
		AlreadyExisted: true,
	}, nil
}

// findGatewayByName resolves a gateway name to its apiserver record.
// ListGateways is the only available lookup primitive; we cap the page
// at 1000 because gateway counts are inherently small (one row per
// region, not per node).
func findGatewayByName(ctx context.Context, client pb.GatewayServiceClient, name string) (*pb.Gateway, error) {
	resp, err := client.ListGateways(ctx, &pb.ListGatewaysRequest{
		Page: &pb.PageRequest{PageSize: 1000},
	})
	if err != nil {
		return nil, fmt.Errorf("list gateways: %w", err)
	}
	for _, g := range resp.GetGateways() {
		if g.GetName() == name {
			return g, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "gateway %q not found", name)
}

// ValidateSelfRegisterName returns an error when the gateway name
// contains characters that are likely to confuse apiserver queries or
// break column-formatted log output. The check is intentionally loose
// (DNS-1123-ish) and runs only when the name is operator-supplied.
func ValidateSelfRegisterName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("name exceeds 63 characters")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			// allowed
		default:
			return fmt.Errorf("name %q contains invalid character %q", name, r)
		}
	}
	return nil
}
