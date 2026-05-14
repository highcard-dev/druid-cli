package routing

import (
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/api"
)

func TestAssignmentsFromPublishes(t *testing.T) {
	targets := []api.RuntimeRoutingTarget{
		{Name: "web-http", PortName: "http", Port: 80, Protocol: "http"},
		{Name: "db-postgres", PortName: "postgres", Port: 5432, Protocol: "tcp"},
	}

	assignments, err := AssignmentsFromPublishes([]string{
		"8080:http",
		"0.0.0.0:15432:5432",
		"8443:http/https",
	}, targets, "scroll-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 3 {
		t.Fatalf("assignments = %d, want 3", len(assignments))
	}
	assertAssignment(t, assignments[0], "web-http", "http", "127.0.0.1", 8080, "localhost", "http", "http://localhost:8080")
	assertAssignment(t, assignments[1], "db-postgres", "postgres", "0.0.0.0", 15432, "localhost", "tcp", "")
	assertAssignment(t, assignments[2], "web-http", "http", "127.0.0.1", 8443, "localhost", "https", "https://localhost:8443")
}

func TestAssignmentsFromPublishesErrors(t *testing.T) {
	targets := []api.RuntimeRoutingTarget{
		{Name: "web-a", PortName: "http-a", Port: 80, Protocol: "http"},
		{Name: "web-b", PortName: "http-b", Port: 80, Protocol: "http"},
	}
	for _, tc := range []struct {
		name    string
		spec    string
		wantErr string
	}{
		{name: "invalid syntax", spec: "8080", wantErr: "invalid publish"},
		{name: "missing target", spec: "8080:http", wantErr: "druid routing targets scroll-a"},
		{name: "ambiguous port", spec: "8080:80", wantErr: "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AssignmentsFromPublishes([]string{tc.spec}, targets, "scroll-a")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want contains %q", err, tc.wantErr)
			}
		})
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
