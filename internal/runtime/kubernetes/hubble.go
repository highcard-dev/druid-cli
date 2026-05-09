package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	hubbleflow "github.com/cilium/cilium/api/v1/flow"
	hubbleobserver "github.com/cilium/cilium/api/v1/observer"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TrafficQuery struct {
	Namespace     string
	ScrollID      string
	ProcedureName string
	Port          domain.Port
	ExpectedPort  domain.ExpectedPort
	Window        time.Duration
}

type HubbleClient interface {
	HasFlow(ctx context.Context, query TrafficQuery) (bool, error)
}

type HubbleRelayClient struct {
	addr string
}

func NewHubbleRelayClient(addr string) *HubbleRelayClient {
	return &HubbleRelayClient{addr: addr}
}

func (c *HubbleRelayClient) HasFlow(ctx context.Context, query TrafficQuery) (bool, error) {
	if c.addr == "" {
		return false, fmt.Errorf("hubble relay address is required")
	}
	window := query.Window
	if window <= 0 {
		window = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(c.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false, err
	}
	defer conn.Close()
	client := hubbleobserver.NewObserverClient(conn)
	filter := &hubbleflow.FlowFilter{
		DestinationLabel: []string{
			labelManagedBy + "=druid",
			labelScrollID + "=" + dnsLabel(query.ScrollID),
			labelProcedure + "=" + dnsLabel(query.ProcedureName),
		},
		DestinationPort: []string{fmt.Sprintf("%d", query.Port.Port)},
	}
	if query.Namespace != "" {
		filter.DestinationPod = []string{query.Namespace + "/"}
	}
	if protocol := normalizeProtocol(query.Port.Protocol); protocol != "" {
		filter.Protocol = []string{protocol}
	}
	stream, err := client.GetFlows(ctx, &hubbleobserver.GetFlowsRequest{
		Since:     timestamppb.New(time.Now().Add(-window)),
		Whitelist: []*hubbleflow.FlowFilter{filter},
	})
	if err != nil {
		return false, err
	}
	for {
		_, err := stream.Recv()
		if err == nil {
			return true, nil
		}
		if ctx.Err() != nil {
			return false, nil
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
}

func normalizeProtocol(protocol string) string {
	switch protocol {
	case "", "tcp", "TCP":
		return "tcp"
	case "udp", "UDP":
		return "udp"
	default:
		return protocol
	}
}
