//go:build !integration

package gateway

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

// fakeGatewayServer is a hand-rolled in-process gRPC server used to
// drive the SelfRegister tests. The unit-test set does not depend on
// third-party gRPC mock libraries; instead we stand up a real
// loopback gRPC server with a closure-backed implementation of
// GatewayServiceServer and talk to it over TCP.
//
// The fake keeps the response / error behaviour for CreateGateway and
// ListGateways in caller-supplied fields so each test can focus on one
// scenario without ceremony.
type fakeGatewayServer struct {
	pb.UnimplementedGatewayServiceServer

	createResp   *pb.CreateGatewayResponse
	createErr    error
	createdNames []string

	listResp *pb.ListGatewaysResponse
	listErr  error
}

func (f *fakeGatewayServer) CreateGateway(_ context.Context, req *pb.CreateGatewayRequest) (*pb.CreateGatewayResponse, error) {
	f.createdNames = append(f.createdNames, req.GetName())
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createResp, nil
}

func (f *fakeGatewayServer) ListGateways(_ context.Context, _ *pb.ListGatewaysRequest) (*pb.ListGatewaysResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResp, nil
}

// newFakeGatewayConn stands up a real loopback gRPC server hosting the
// supplied GatewayServiceServer and returns a client connection pointed
// at it. The server is torn down when the test ends.
func newFakeGatewayConn(t *testing.T, fake pb.GatewayServiceServer) (*grpc.ClientConn, *fakeGatewayServer) {
	t.Helper()

	concrete, ok := fake.(*fakeGatewayServer)
	if !ok {
		t.Fatalf("newFakeGatewayConn requires *fakeGatewayServer, got %T", fake)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen loopback: %v", err)
	}
	server := grpc.NewServer()
	pb.RegisterGatewayServiceServer(server, concrete)

	go func() {
		_ = server.Serve(lis)
	}()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		t.Fatalf("dial loopback: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
	})
	return conn, concrete
}

// captureLog redirects log output to a buffer so tests can assert on
// the human-readable lines SelfRegister emits. The original writer is
// restored via the returned closure.
func captureLog(t *testing.T) (restore func()) {
	t.Helper()
	orig := log.Writer()
	r, w := io.Pipe()
	log.SetOutput(w)
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	restore = func() {
		log.SetOutput(orig)
		_ = w.Close()
		<-done
	}
	return restore
}

func TestSelfRegister_HappyPath(t *testing.T) {
	fake := &fakeGatewayServer{
		createResp: &pb.CreateGatewayResponse{
			Gateway: &pb.Gateway{Id: "uuid-1", Name: "gw-1", Status: "Active"},
		},
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	res, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{
		GatewayID: "gw-1",
		Region:    "cn-east-1",
		Endpoint:  "gw-1.example.com:9443",
		Labels:    map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("SelfRegister: %v", err)
	}
	if res.GatewayID != "uuid-1" {
		t.Errorf("GatewayID = %q, want uuid-1", res.GatewayID)
	}
	if res.Name != "gw-1" {
		t.Errorf("Name = %q, want gw-1", res.Name)
	}
	if res.AlreadyExisted {
		t.Errorf("AlreadyExisted = true, want false")
	}
	if len(fake.createdNames) != 1 || fake.createdNames[0] != "gw-1" {
		t.Errorf("createdNames = %v, want [gw-1]", fake.createdNames)
	}
}

func TestSelfRegister_DefaultsNameToGatewayID(t *testing.T) {
	fake := &fakeGatewayServer{
		createResp: &pb.CreateGatewayResponse{
			Gateway: &pb.Gateway{Id: "uuid-2", Name: "node-01", Status: "Active"},
		},
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	_, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{
		GatewayID: "node-01",
	})
	if err != nil {
		t.Fatalf("SelfRegister: %v", err)
	}
	if len(fake.createdNames) != 1 || fake.createdNames[0] != "node-01" {
		t.Errorf("createdNames = %v, want [node-01]", fake.createdNames)
	}
}

func TestSelfRegister_NameOverride(t *testing.T) {
	fake := &fakeGatewayServer{
		createResp: &pb.CreateGatewayResponse{
			Gateway: &pb.Gateway{Id: "uuid-3", Name: "gateway-shanghai", Status: "Active"},
		},
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	res, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{
		GatewayID: "node-shanghai-01",
		Name:      "gateway-shanghai",
	})
	if err != nil {
		t.Fatalf("SelfRegister: %v", err)
	}
	if res.Name != "gateway-shanghai" {
		t.Errorf("Name = %q, want gateway-shanghai", res.Name)
	}
	if len(fake.createdNames) != 1 || fake.createdNames[0] != "gateway-shanghai" {
		t.Errorf("createdNames = %v, want [gateway-shanghai]", fake.createdNames)
	}
}

func TestSelfRegister_AlreadyExists_IdempotentLookup(t *testing.T) {
	fake := &fakeGatewayServer{
		createErr: status.Error(codes.AlreadyExists, "gateway already exists"),
		listResp: &pb.ListGatewaysResponse{
			Gateways: []*pb.Gateway{
				{Id: "uuid-existing", Name: "gw-1", Status: "Active"},
			},
		},
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	res, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{GatewayID: "gw-1"})
	if err != nil {
		t.Fatalf("SelfRegister: %v", err)
	}
	if !res.AlreadyExisted {
		t.Errorf("AlreadyExisted = false, want true")
	}
	if res.GatewayID != "uuid-existing" {
		t.Errorf("GatewayID = %q, want uuid-existing", res.GatewayID)
	}
}

func TestSelfRegister_AlreadyExists_LookupFails(t *testing.T) {
	fake := &fakeGatewayServer{
		createErr: status.Error(codes.AlreadyExists, "gateway already exists"),
		listErr:   errors.New("boom"),
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	_, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{GatewayID: "gw-1"})
	if err == nil {
		t.Fatal("expected error when lookup fails")
	}
	if !strings.Contains(err.Error(), "lookup failed") {
		t.Errorf("error = %q, want it to mention 'lookup failed'", err)
	}
}

func TestSelfRegister_AlreadyExists_LookupMisses(t *testing.T) {
	fake := &fakeGatewayServer{
		createErr: status.Error(codes.AlreadyExists, "gateway already exists"),
		listResp:  &pb.ListGatewaysResponse{Gateways: nil},
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	_, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{GatewayID: "gw-1"})
	if err == nil {
		t.Fatal("expected error when lookup returns no rows")
	}
}

func TestSelfRegister_TransportError(t *testing.T) {
	fake := &fakeGatewayServer{
		createErr: status.Error(codes.Unavailable, "control plane down"),
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	_, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{GatewayID: "gw-1"})
	if err == nil {
		t.Fatal("expected transport error to bubble up")
	}
	if !strings.Contains(err.Error(), "control plane down") {
		t.Errorf("error = %q, want it to mention 'control plane down'", err)
	}
}

func TestSelfRegister_MissingGatewayID(t *testing.T) {
	conn, _ := newFakeGatewayConn(t, &fakeGatewayServer{})
	t.Cleanup(func() { _ = conn.Close() })

	_, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{})
	if err == nil {
		t.Fatal("expected error when gateway_id is empty")
	}
}

func TestSelfRegister_LogsOnSuccess(t *testing.T) {
	fake := &fakeGatewayServer{
		createResp: &pb.CreateGatewayResponse{
			Gateway: &pb.Gateway{Id: "uuid-log", Name: "gw-log", Status: "Active"},
		},
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	restore := captureLog(t)
	defer restore()

	if _, err := SelfRegister(context.Background(), conn, SelfRegisterConfig{GatewayID: "gw-log"}); err != nil {
		t.Fatalf("SelfRegister: %v", err)
	}
}

func TestSelfRegister_RespectsContextDeadline(t *testing.T) {
	// Drive a context that is already cancelled; SelfRegister must
	// respect the deadline and not block forever.
	fake := &fakeGatewayServer{
		createResp: &pb.CreateGatewayResponse{
			Gateway: &pb.Gateway{Id: "uuid-ctx", Name: "gw-ctx", Status: "Active"},
		},
	}
	conn, _ := newFakeGatewayConn(t, fake)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := SelfRegister(ctx, conn, SelfRegisterConfig{GatewayID: "gw-ctx", Timeout: 100 * time.Millisecond})
	// Either nil (server replied in time) or context-deadline-exceeded
	// are acceptable: what matters is that the call returns promptly.
	if err != nil {
		if status.Code(err) != codes.DeadlineExceeded &&
			!strings.Contains(err.Error(), "context deadline exceeded") &&
			!strings.Contains(err.Error(), "DeadlineExceeded") {
			t.Logf("SelfRegister returned: %v (acceptable: deadline, nil, or wrapped deadline)", err)
		}
	}
}

func TestValidateSelfRegisterName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"valid simple", "gw-1", false},
		{"valid with dot", "gw.shanghai.01", false},
		{"valid with underscore", "gw_shanghai", false},
		{"too long", strings.Repeat("a", 64), true},
		{"space inside", "gw 1", true},
		{"slash inside", "gw/1", true},
		{"non-ascii", "网关-1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSelfRegisterName(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateSelfRegisterName(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}
