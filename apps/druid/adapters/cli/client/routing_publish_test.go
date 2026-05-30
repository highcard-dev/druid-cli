package client

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestPublishFlagsAreExposed(t *testing.T) {
	if flag := CreateCommand.Flags().Lookup("publish"); flag == nil || flag.Shorthand != "p" {
		t.Fatal("druid create should expose -p/--publish")
	}
	if flag := RoutingApplyCommand.Flags().Lookup("publish"); flag == nil || flag.Shorthand != "p" {
		t.Fatal("druid routing apply should expose -p/--publish")
	}
}

func TestCreateWithPublishAppliesRoutingWithoutStarting(t *testing.T) {
	daemon := &fakeRoutingDaemon{
		targets: []api.RuntimeRoutingTarget{{Name: "web-http", PortName: "http", Port: 80, Protocol: "http"}},
	}

	scroll, err := createScrollWithRouting(context.Background(), daemon, "artifact", "scroll-a", nil, []string{"8080:http"})
	if err != nil {
		t.Fatal(err)
	}
	if scroll.Id != "scroll-a" {
		t.Fatalf("scroll id = %s, want scroll-a", scroll.Id)
	}
	if daemon.createCalls != 1 || daemon.targetCalls != 1 || daemon.applyCalls != 1 {
		t.Fatalf("calls create=%d targets=%d apply=%d", daemon.createCalls, daemon.targetCalls, daemon.applyCalls)
	}
	if daemon.startCalls != 0 {
		t.Fatalf("start calls = %d, want 0", daemon.startCalls)
	}
	assertAssignment(t, daemon.applied[0], "web-http", "http", "127.0.0.1", 8080, "localhost", "http", "http://localhost:8080")
}

func TestCreateWithoutPublishSkipsRouting(t *testing.T) {
	daemon := &fakeRoutingDaemon{}

	if _, err := createScrollWithRouting(context.Background(), daemon, "artifact", "scroll-a", nil, nil); err != nil {
		t.Fatal(err)
	}
	if daemon.createCalls != 1 || daemon.targetCalls != 0 || daemon.applyCalls != 0 {
		t.Fatalf("calls create=%d targets=%d apply=%d", daemon.createCalls, daemon.targetCalls, daemon.applyCalls)
	}
}

func TestRoutingAssignmentsForApply(t *testing.T) {
	daemon := &fakeRoutingDaemon{
		targets: []api.RuntimeRoutingTarget{{Name: "web-http", PortName: "http", Port: 80, Protocol: "http"}},
	}

	assignments, err := routingAssignmentsForApply(context.Background(), daemon, "scroll-a", "", []string{"8080:http"})
	if err != nil {
		t.Fatal(err)
	}
	assertAssignment(t, assignments[0], "web-http", "http", "127.0.0.1", 8080, "localhost", "http", "http://localhost:8080")

	_, err = routingAssignmentsForApply(context.Background(), daemon, "scroll-a", "routes.json", []string{"8080:http"})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("error = %v, want mutual exclusion", err)
	}
}

func TestParseRoutingAssignmentsAcceptsEnvelopeAndArray(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"assignments":[{"name":"web-http","port_name":"http","public_port":8080}]}`),
		[]byte(`[{"name":"web-http","port_name":"http","public_port":8080}]`),
	} {
		assignments, err := parseRoutingAssignments(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(assignments) != 1 || value(assignments[0].Name) != "web-http" {
			t.Fatalf("assignments = %#v", assignments)
		}
	}
}

func TestReadRoutingAssignmentsFileReadsStdin(t *testing.T) {
	oldStdin := os.Stdin
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = read
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = read.Close()
	})
	if _, err := write.WriteString(`[{"name":"web-http"}]`); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := readRoutingAssignmentsFile("-")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[{"name":"web-http"}]` {
		t.Fatalf("stdin data = %q", string(data))
	}
}

func assertAssignment(t *testing.T, assignment api.RuntimeRouteAssignment, name string, portName string, externalIP string, publicPort int, host string, protocol string, url string) {
	t.Helper()
	if value(assignment.Name) != name || value(assignment.PortName) != portName || value(assignment.ExternalIp) != externalIP || intValue(assignment.PublicPort) != publicPort || value(assignment.Host) != host || value(assignment.Protocol) != protocol || value(assignment.Url) != url {
		t.Fatalf("assignment = %#v", assignment)
	}
}

func value(in *string) string {
	if in == nil {
		return ""
	}
	return *in
}

func intValue(in *int) int {
	if in == nil {
		return 0
	}
	return *in
}

type fakeRoutingDaemon struct {
	targets     []api.RuntimeRoutingTarget
	applied     []api.RuntimeRouteAssignment
	createCalls int
	targetCalls int
	applyCalls  int
	startCalls  int
}

func (f *fakeRoutingDaemon) CreateScroll(ctx context.Context, name string, artifact string, registryCredentials []api.RegistryCredential) (*api.RuntimeScroll, error) {
	f.createCalls++
	return &api.RuntimeScroll{Id: name, Artifact: artifact, Root: "/root", ScrollName: name, Status: api.RuntimeScrollStatusCreated}, nil
}

func (f *fakeRoutingDaemon) UpdateScroll(ctx context.Context, id string, artifact string, registryCredentials []api.RegistryCredential) (*api.RuntimeScroll, error) {
	return &api.RuntimeScroll{Id: id, Artifact: artifact, Root: "/root", ScrollName: id, Status: api.RuntimeScrollStatusCreated}, nil
}

func (f *fakeRoutingDaemon) ListScrolls(ctx context.Context) ([]api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeRoutingDaemon) GetScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return &api.RuntimeScroll{Id: id, Status: api.RuntimeScrollStatusCreated}, nil
}

func (f *fakeRoutingDaemon) DeleteScroll(ctx context.Context, id string) (*api.DeletedScroll, error) {
	return nil, nil
}

func (f *fakeRoutingDaemon) GetScrollConfig(ctx context.Context, id string) (*domain.File, error) {
	return &domain.File{}, nil
}

func (f *fakeRoutingDaemon) GetScrollQueue(ctx context.Context, id string) (domain.ProcedureStatusMap, error) {
	return nil, nil
}

func (f *fakeRoutingDaemon) GetScrollConsoles(ctx context.Context, id string) (map[string]domain.Console, error) {
	return nil, nil
}

func (f *fakeRoutingDaemon) GetScrollPorts(ctx context.Context, id string) ([]api.RuntimePortStatus, error) {
	return nil, nil
}

func (f *fakeRoutingDaemon) StartScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	f.startCalls++
	return nil, nil
}

func (f *fakeRoutingDaemon) StopScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeRoutingDaemon) GetScrollRoutingTargets(ctx context.Context, id string) ([]api.RuntimeRoutingTarget, error) {
	f.targetCalls++
	return f.targets, nil
}

func (f *fakeRoutingDaemon) ApplyScrollRouting(ctx context.Context, id string, assignments []api.RuntimeRouteAssignment) (*api.RuntimeScroll, error) {
	f.applyCalls++
	f.applied = assignments
	routing := append([]api.RuntimeRouteAssignment(nil), assignments...)
	return &api.RuntimeScroll{Id: id, Status: api.RuntimeScrollStatusCreated, Routing: &routing}, nil
}

func (f *fakeRoutingDaemon) GetScrollUIPackages(ctx context.Context, id string) (map[string]api.RuntimeUIPackage, error) {
	return map[string]api.RuntimeUIPackage{}, nil
}

func (f *fakeRoutingDaemon) PublishScrollUIPackage(ctx context.Context, id string, scope string, path string) (*api.RuntimeScroll, error) {
	return &api.RuntimeScroll{Id: id, Status: api.RuntimeScrollStatusCreated}, nil
}

func (f *fakeRoutingDaemon) EnableWatch(ctx context.Context, id string, request api.DevWatchRequest) (*api.DevWatchResponse, error) {
	return &api.DevWatchResponse{Status: "enabled", Enabled: true, WatchedPaths: request.WatchPaths}, nil
}

func (f *fakeRoutingDaemon) DisableWatch(ctx context.Context, id string) (*api.DevWatchResponse, error) {
	return &api.DevWatchResponse{Status: "disabled", Enabled: false}, nil
}

func (f *fakeRoutingDaemon) WatchStatus(ctx context.Context, id string) (*api.DevWatchStatus, error) {
	return &api.DevWatchStatus{Enabled: false, WatchedPaths: []string{}}, nil
}
