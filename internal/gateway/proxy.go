package gateway

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

// OnboardingProxy forwards Bootstrap and Renew requests to the Control Plane.
// The gateway-runtime does NOT hold CA private keys and does NOT read/write
// identity tables directly; it is a pure pass-through.
type OnboardingProxy struct {
	apiv1.UnimplementedNodeOnboardingServiceServer

	upstream  apiv1.NodeOnboardingServiceClient
	gatewayID string
}

// NewOnboardingProxy creates a proxy backed by the given upstream connection.
func NewOnboardingProxy(cc grpc.ClientConnInterface, gatewayID string) *OnboardingProxy {
	return &OnboardingProxy{
		upstream:  apiv1.NewNodeOnboardingServiceClient(cc),
		gatewayID: gatewayID,
	}
}

// Bootstrap forwards the request to the Control Plane apiserver.
func (p *OnboardingProxy) Bootstrap(ctx context.Context, req *apiv1.BootstrapRequest) (*apiv1.BootstrapResponse, error) {
	if req.GetGatewayId() == "" {
		cloned := proto.Clone(req).(*apiv1.BootstrapRequest)
		cloned.GatewayId = p.gatewayID
		req = cloned
	}
	return p.upstream.Bootstrap(ctx, req)
}

// Renew forwards the certificate renewal request to the Control Plane apiserver.
func (p *OnboardingProxy) Renew(ctx context.Context, req *apiv1.RenewRequest) (*apiv1.RenewResponse, error) {
	return p.upstream.Renew(ctx, req)
}
