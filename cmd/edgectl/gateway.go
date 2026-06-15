// Package main — edgectl gateway subcommands.
//
// `edgectl gateway` is the operator's entry point for managing logical
// gateways. The `register` subcommand replaces the old "manually insert a
// row into the `gateways` table" workflow: it calls the apiserver's
// `GatewayService.CreateGateway` RPC and prints the newly-assigned
// `gateway_id` so the operator can pass it to `edgectl token create
// --gateway <id>` or directly to `install-edge-agent.sh GATEWAY_ID=<id>`.
//
// The CLI itself is a thin wrapper around the same gRPC services that
// gateway-runtime and the controller use, so behaviour is identical no
// matter which entry point the operator chooses.
package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

// gatewayCmd is the parent command for "edgectl gateway ...".
func gatewayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Manage logical gateways (regions)",
		Long: `Register, list, inspect, and delete logical gateways.

A gateway represents a region that hosts a gateway-runtime DaemonSet. It
owns the bootstrap tokens that edge-agents exchange for mTLS certificates
on first boot. Registering a gateway is the first step before creating
bootstrap tokens or onboarding edge nodes.`,
	}
	cmd.AddCommand(
		gatewayRegisterCmd(),
		gatewayListCmd(),
		gatewayGetCmd(),
		gatewayUpdateCmd(),
		gatewayDeleteCmd(),
	)
	return cmd
}

// gatewayRegisterCmd creates a new logical gateway.
//
// Example:
//
//	edgectl gateway register --name gateway-shanghai \
//	    --region cn-east-1 \
//	    --endpoint gateway-shanghai.example.com:9443 \
//	    --label env=prod --label site=shanghai
//
// The newly-created gateway_id is printed to stdout as the LAST line in
// the form `gateway_id: <id>` so the operator can capture it in a shell
// variable without parsing the rest of the output:
//
//	GATEWAY_ID=$(edgectl ... register ... | tail -n1 | awk '{print $2}')
func gatewayRegisterCmd() *cobra.Command {
	var (
		name     string
		region   string
		endpoint string
		labels   []string
	)
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a new logical gateway and print its gateway_id",
		Long: `Create a new gateway row in the apiserver.

This is the only sanctioned way to obtain a gateway_id: the apiserver
generates the id (UUID) and persists the gateway atomically. The id is
printed as the final line ("gateway_id: <id>") so it can be captured
programmatically.

The command is idempotent on the gateway NAME: a second invocation with
the same --name returns the existing gateway_id and exits 0.`,
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close gateway connection: %w", closeErr)
						return
					}
					fmt.Fprintf(os.Stderr, "edgectl: close gateway connection: %v\n", closeErr)
				}
			}()

			parsedLabels, err := parseLabelPairs(labels)
			if err != nil {
				return err
			}

			client := pb.NewGatewayServiceClient(conn)
			req := &pb.CreateGatewayRequest{
				Name:     name,
				Region:   region,
				Endpoint: endpoint,
			}
			if len(parsedLabels) > 0 {
				req.Labels = &pb.Labels{Items: parsedLabels}
			}

			resp, err := client.CreateGateway(authCtx(), req)
			if err != nil {
				// Idempotency: name uniqueness is enforced server-side; if
				// the gateway already exists, look it up and return its
				// id so a retry of the same command is safe.
				if status.Code(err) == codes.AlreadyExists {
					gw, lookupErr := lookupGatewayByName(client, name)
					if lookupErr != nil {
						return fmt.Errorf("gateway %q already exists but lookup failed: %w", name, lookupErr)
					}
					printGatewaySummary(gw, true)
					fmt.Printf("gateway_id: %s\n", gw.GetId())
					return nil
				}
				return fmt.Errorf("register gateway: %w", err)
			}

			gw := resp.GetGateway()
			printGatewaySummary(gw, false)
			fmt.Printf("gateway_id: %s\n", gw.GetId())
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Gateway name (must be unique, required)")
	cmd.Flags().StringVar(&region, "region", "", "Region identifier (e.g. cn-east-1)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Public mTLS endpoint edge-agents use to reach this gateway (host:port)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "Label as key=value; repeat for multiple")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// gatewayListCmd lists gateways, optionally filtered by region.
func gatewayListCmd() *cobra.Command {
	var region string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered gateways",
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close gateway connection: %w", closeErr)
						return
					}
					fmt.Fprintf(os.Stderr, "edgectl: close gateway connection: %v\n", closeErr)
				}
			}()

			client := pb.NewGatewayServiceClient(conn)
			resp, err := client.ListGateways(authCtx(), &pb.ListGatewaysRequest{
				Region: region,
			})
			if err != nil {
				return fmt.Errorf("list gateways: %w", err)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(w, "ID\tNAME\tREGION\tSTATUS\tENDPOINT\tCREATED"); err != nil {
				return err
			}
			for _, g := range resp.GetGateways() {
				created := "N/A"
				if g.GetCreatedAt() != nil {
					created = g.GetCreatedAt().AsTime().Format(time.RFC3339)
				}
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					g.GetId(), g.GetName(), g.GetRegion(),
					g.GetStatus(), g.GetEndpoint(), created); err != nil {
					return err
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&region, "region", "", "Filter by region")
	return cmd
}

// gatewayGetCmd prints the full record for a single gateway by id or name.
func gatewayGetCmd() *cobra.Command {
	var byName bool
	cmd := &cobra.Command{
		Use:   "get <gateway-id-or-name>",
		Short: "Print a single gateway by id or --by-name",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close gateway connection: %w", closeErr)
						return
					}
					fmt.Fprintf(os.Stderr, "edgectl: close gateway connection: %v\n", closeErr)
				}
			}()

			client := pb.NewGatewayServiceClient(conn)
			gw, err := resolveGateway(client, args[0], byName)
			if err != nil {
				return err
			}
			printGatewaySummary(gw, false)
			fmt.Printf("gateway_id: %s\n", gw.GetId())
			return nil
		},
	}
	cmd.Flags().BoolVar(&byName, "by-name", false, "Treat the argument as a gateway NAME instead of an id")
	return cmd
}

// gatewayUpdateCmd patches one or more business attributes (region /
// endpoint / labels) on an existing gateway. The server applies
// COALESCE semantics: every field not passed on the command line is
// left untouched on the database row. The `id` itself cannot be
// changed — it is the apiserver-assigned UUID.
//
// Example:
//
//	edgectl gateway update <node-name> \
//	    --region cn-east-1 \
//	    --endpoint gateway-shanghai-01.example.com:9443 \
//	    --label env=prod --label site=shanghai
//
// `gateway update` is the post-register counterpart to
// `gateway register` and the documented way to backfill per-gateway
// metadata on gateways that were created by gateway-runtime's
// self-registration flow (which only writes NAME = spec.nodeName).
func gatewayUpdateCmd() *cobra.Command {
	var (
		byName      bool
		region      string
		endpoint    string
		labels      []string
		clearLabels bool
		regionSet   bool
		endpointSet bool
	)
	cmd := &cobra.Command{
		Use:   "update <gateway-id-or-name>",
		Short: "Patch region / endpoint / labels on an existing gateway",
		Long: `Update a gateway's business attributes.

The id of the gateway cannot be changed. Every other field is
optional; only the flags actually passed on the command line are
sent to the server, and the server applies COALESCE semantics so
omitted fields keep their previous values. NAME in particular is
owned by the K8s node name (or whatever the operator wired up at
register time) and is never modified by this command.

This is the supported way to backfill per-gateway metadata on a
gateway that was created by gateway-runtime's self-registration
flow — that flow only writes the NAME, so region / endpoint /
labels have to be filled in here.

Examples:

  # Backfill region + endpoint on a self-registered gateway
  edgectl gateway update gateway-shanghai-01 \
      --region cn-east-1 \
      --endpoint gateway-shanghai-01.example.com:9443

  # Replace the entire labels map (atomically, last write wins)
  edgectl gateway update gateway-shanghai-01 \
      --label env=prod --label site=shanghai

  # Look up the gateway by name instead of by id
  edgectl gateway update gateway-shanghai-01 --by-name \
      --region cn-east-1`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) (err error) {
			// Track which optional fields the operator actually
			// set on the command line. cobra's StringVar binds
			// the default to the zero value (""), so we cannot
			// distinguish "unset" from "explicitly empty" by
			// reading the variable alone — we have to look at
			// the Changed() flag instead. This is what lets
			// the server keep a previously-set region / endpoint
			// when the operator only sets --label.
			regionSet = c.Flags().Changed("region")
			endpointSet = c.Flags().Changed("endpoint")

			parsedLabels, err := parseLabelPairs(labels)
			if err != nil {
				return err
			}
			if clearLabels && len(parsedLabels) > 0 {
				return fmt.Errorf("--clear-labels cannot be combined with --label (they are mutually exclusive)")
			}
			if !regionSet && !endpointSet && !clearLabels && len(parsedLabels) == 0 {
				return fmt.Errorf("no update fields provided: pass at least one of --region / --endpoint / --label / --clear-labels")
			}

			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close gateway connection: %w", closeErr)
						return
					}
					fmt.Fprintf(os.Stderr, "edgectl: close gateway connection: %v\n", closeErr)
				}
			}()

			client := pb.NewGatewayServiceClient(conn)
			gw, err := resolveGateway(client, args[0], byName)
			if err != nil {
				return err
			}

			req := &pb.UpdateGatewayRequest{Id: gw.GetId()}
			if regionSet {
				req.Region = region
			}
			if endpointSet {
				req.Endpoint = endpoint
			}
			if clearLabels {
				// Explicit empty map: the server's
				// marshalLabels path persists this as `{}`
				// and the COALESCE does NOT fire (the
				// pointer is non-nil).
				req.Labels = &pb.Labels{Items: map[string]string{}}
			} else if len(parsedLabels) > 0 {
				req.Labels = &pb.Labels{Items: parsedLabels}
			}

			resp, err := client.UpdateGateway(authCtx(), req)
			if err != nil {
				return fmt.Errorf("update gateway: %w", err)
			}

			printGatewaySummary(resp.GetGateway(), false)
			fmt.Printf("gateway_id: %s\n", resp.GetGateway().GetId())
			return nil
		},
	}
	cmd.Flags().BoolVar(&byName, "by-name", false, "Treat the argument as a gateway NAME instead of an id")
	cmd.Flags().StringVar(&region, "region", "", "New region (e.g. cn-east-1); omit to keep current")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "New public mTLS endpoint (host:port); omit to keep current")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "Replace the entire labels map; key=value, repeat for multiple")
	cmd.Flags().BoolVar(&clearLabels, "clear-labels", false, "Clear the labels map (mutually exclusive with --label)")
	return cmd
}

// gatewayDeleteCmd soft-deletes a gateway.
func gatewayDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <gateway-id>",
		Short: "Soft-delete a gateway (sets status=Deleted)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) (err error) {
			conn, err := dial()
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("close gateway connection: %w", closeErr)
						return
					}
					fmt.Fprintf(os.Stderr, "edgectl: close gateway connection: %v\n", closeErr)
				}
			}()

			client := pb.NewGatewayServiceClient(conn)
			if _, err := client.DeleteGateway(authCtx(), &pb.DeleteGatewayRequest{
				Id: args[0],
			}); err != nil {
				return fmt.Errorf("delete gateway: %w", err)
			}
			fmt.Printf("Gateway %s deleted\n", args[0])
			return nil
		},
	}
	return cmd
}

// resolveGateway looks up a gateway by id by default, or by name when
// --by-name is set. ListGateways is used as the lookup primitive for
// name resolution — there is no GetByName RPC, and the dataset is small
// (one row per region).
func resolveGateway(client pb.GatewayServiceClient, target string, byName bool) (*pb.Gateway, error) {
	if !byName {
		resp, err := client.GetGateway(authCtx(), &pb.GetGatewayRequest{Id: target})
		if err != nil {
			return nil, fmt.Errorf("get gateway: %w", err)
		}
		return resp.GetGateway(), nil
	}
	gw, err := lookupGatewayByName(client, target)
	if err != nil {
		return nil, err
	}
	return gw, nil
}

// lookupGatewayByName finds a gateway by its unique name. Returns
// (gateway, nil) on success, (nil, NotFound error) when missing, and
// (nil, error) on transport / RPC failures. Pagination is intentionally
// not handled: gateway counts are small and a real production
// deployment has tens, not thousands.
func lookupGatewayByName(client pb.GatewayServiceClient, name string) (*pb.Gateway, error) {
	resp, err := client.ListGateways(authCtx(), &pb.ListGatewaysRequest{
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

// printGatewaySummary prints a stable, multi-line human-readable summary.
// The format is intentionally script-friendly: every field has the form
// "Label: value" with no decoration, and the gateway_id line is the LAST
// line (also printed by the caller as a separate "gateway_id: <id>" line
// for easy capture by shell scripts).
func printGatewaySummary(gw *pb.Gateway, alreadyExisted bool) {
	if alreadyExisted {
		fmt.Printf("Gateway %q already exists; using existing record.\n", gw.GetName())
	} else {
		fmt.Printf("Gateway registered.\n")
	}
	fmt.Printf("ID:       %s\n", gw.GetId())
	fmt.Printf("Name:     %s\n", gw.GetName())
	if r := gw.GetRegion(); r != "" {
		fmt.Printf("Region:   %s\n", r)
	}
	if e := gw.GetEndpoint(); e != "" {
		fmt.Printf("Endpoint: %s\n", e)
	}
	if s := gw.GetStatus(); s != "" {
		fmt.Printf("Status:   %s\n", s)
	}
	if items := gw.GetLabels().GetItems(); len(items) > 0 {
		fmt.Printf("Labels:   ")
		first := true
		for k, v := range items {
			if !first {
				fmt.Print(",")
			}
			fmt.Printf("%s=%s", k, v)
			first = false
		}
		fmt.Println()
	}
}

// parseLabelPairs converts ["k1=v1", "k2=v2"] into a map, returning an
// error if any entry is malformed.
func parseLabelPairs(items []string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(items))
	for _, raw := range items {
		for i := 0; i < len(raw); i++ {
			if raw[i] == '=' {
				k := raw[:i]
				v := raw[i+1:]
				if k == "" {
					return nil, fmt.Errorf("invalid label %q: empty key", raw)
				}
				out[k] = v
				goto next
			}
		}
		return nil, fmt.Errorf("invalid label %q: expected key=value", raw)
	next:
	}
	return out, nil
}
