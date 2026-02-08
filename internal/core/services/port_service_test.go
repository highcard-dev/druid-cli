package services_test

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
)

func TestPortMonitor_AddPort_Success(t *testing.T) {
	pm := services.NewPortService([]int{})

	port := domain.Port{
		Port:     8080,
		Protocol: "tcp",
		Name:     "http",
	}

	result, err := pm.AddPort(port)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result.Port.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", result.Port.Port)
	}
	if result.Port.Protocol != "tcp" {
		t.Errorf("Expected protocol tcp, got %s", result.Port.Protocol)
	}
	if result.Port.Name != "http" {
		t.Errorf("Expected name http, got %s", result.Port.Name)
	}
	if result.InactiveSince.IsZero() {
		t.Error("Expected InactiveSince to be set")
	}

	ports := pm.GetPorts()
	if len(ports) != 1 {
		t.Errorf("Expected 1 port, got %d", len(ports))
	}
}

func TestPortMonitor_AddPort_InvalidPortRange(t *testing.T) {
	pm := services.NewPortService([]int{})

	tests := []struct {
		name string
		port int
	}{
		{"port zero", 0},
		{"negative port", -1},
		{"port too high", 65536},
		{"port way too high", 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pm.AddPort(domain.Port{
				Port:     tt.port,
				Protocol: "tcp",
				Name:     "test",
			})
			if err == nil {
				t.Errorf("Expected error for port %d, got nil", tt.port)
			}
		})
	}
}

func TestPortMonitor_AddPort_InvalidProtocol(t *testing.T) {
	pm := services.NewPortService([]int{})

	tests := []struct {
		name     string
		protocol string
	}{
		{"empty protocol", ""},
		{"invalid protocol", "http"},
		{"icmp", "icmp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pm.AddPort(domain.Port{
				Port:     8080,
				Protocol: tt.protocol,
				Name:     "test",
			})
			if err == nil {
				t.Errorf("Expected error for protocol '%s', got nil", tt.protocol)
			}
		})
	}
}

func TestPortMonitor_AddPort_ValidProtocols(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
	}{
		{"tcp lowercase", "tcp"},
		{"udp lowercase", "udp"},
		{"TCP uppercase", "TCP"},
		{"UDP uppercase", "UDP"},
		{"Tcp mixed case", "Tcp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := services.NewPortService([]int{})
			result, err := pm.AddPort(domain.Port{
				Port:     8080,
				Protocol: tt.protocol,
				Name:     "test",
			})
			if err != nil {
				t.Errorf("Expected no error for protocol '%s', got %v", tt.protocol, err)
			}
			if result.Port.Protocol != "tcp" && result.Port.Protocol != "udp" {
				t.Errorf("Expected protocol to be normalized, got '%s'", result.Port.Protocol)
			}
		})
	}
}

func TestPortMonitor_AddPort_Duplicate(t *testing.T) {
	pm := services.NewPortService([]int{8080})

	_, err := pm.AddPort(domain.Port{
		Port:     8080,
		Protocol: "tcp",
		Name:     "duplicate",
	})
	if err == nil {
		t.Error("Expected error for duplicate port, got nil")
	}
}

func TestPortMonitor_AddPort_MultipleDifferentPorts(t *testing.T) {
	pm := services.NewPortService([]int{})

	_, err := pm.AddPort(domain.Port{Port: 8080, Protocol: "tcp", Name: "http"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, err = pm.AddPort(domain.Port{Port: 443, Protocol: "tcp", Name: "https"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, err = pm.AddPort(domain.Port{Port: 27015, Protocol: "udp", Name: "game"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	ports := pm.GetPorts()
	if len(ports) != 3 {
		t.Errorf("Expected 3 ports, got %d", len(ports))
	}
}

func TestPortMonitor_RemovePort_Success(t *testing.T) {
	pm := services.NewPortService([]int{8080, 443})

	err := pm.RemovePort(8080)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	ports := pm.GetPorts()
	if len(ports) != 1 {
		t.Errorf("Expected 1 port, got %d", len(ports))
	}
	if ports[0].Port.Port != 443 {
		t.Errorf("Expected remaining port 443, got %d", ports[0].Port.Port)
	}
}

func TestPortMonitor_RemovePort_NotFound(t *testing.T) {
	pm := services.NewPortService([]int{8080})

	err := pm.RemovePort(9999)
	if err == nil {
		t.Error("Expected error for non-existent port, got nil")
	}
}

func TestPortMonitor_RemovePort_EmptyList(t *testing.T) {
	pm := services.NewPortService([]int{})

	err := pm.RemovePort(8080)
	if err == nil {
		t.Error("Expected error for removing from empty list, got nil")
	}
}

func TestPortMonitor_RemovePort_LastPort(t *testing.T) {
	pm := services.NewPortService([]int{8080})

	err := pm.RemovePort(8080)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	ports := pm.GetPorts()
	if len(ports) != 0 {
		t.Errorf("Expected 0 ports, got %d", len(ports))
	}
}

func TestPortMonitor_AddThenRemove(t *testing.T) {
	pm := services.NewPortService([]int{})

	_, err := pm.AddPort(domain.Port{Port: 8080, Protocol: "tcp", Name: "http"})
	if err != nil {
		t.Fatalf("Unexpected error adding port: %v", err)
	}

	err = pm.RemovePort(8080)
	if err != nil {
		t.Fatalf("Unexpected error removing port: %v", err)
	}

	ports := pm.GetPorts()
	if len(ports) != 0 {
		t.Errorf("Expected 0 ports after add and remove, got %d", len(ports))
	}

	// Should be able to add again after removing
	_, err = pm.AddPort(domain.Port{Port: 8080, Protocol: "tcp", Name: "http"})
	if err != nil {
		t.Fatalf("Unexpected error re-adding port: %v", err)
	}

	ports = pm.GetPorts()
	if len(ports) != 1 {
		t.Errorf("Expected 1 port after re-add, got %d", len(ports))
	}
}
