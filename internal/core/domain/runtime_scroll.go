package domain

import "time"

type RuntimeScrollStatus string

const (
	RuntimeScrollStatusCreated RuntimeScrollStatus = "created"
	RuntimeScrollStatusRunning RuntimeScrollStatus = "running"
	RuntimeScrollStatusStopped RuntimeScrollStatus = "stopped"
	RuntimeScrollStatusError   RuntimeScrollStatus = "error"
	RuntimeScrollStatusDeleted RuntimeScrollStatus = "deleted"
)

type RuntimeScroll struct {
	ID             string                   `json:"id"`
	OwnerID        string                   `json:"owner_id,omitempty"`
	Artifact       string                   `json:"artifact"`
	ArtifactDigest string                   `json:"artifact_digest,omitempty"`
	Root           string                   `json:"root"`
	ScrollName     string                   `json:"scroll_name"`
	ScrollYAML     string                   `json:"-"`
	Status         RuntimeScrollStatus      `json:"status"`
	LastError      string                   `json:"last_error,omitempty"`
	Routing        []RuntimeRouteAssignment `json:"routing,omitempty"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
	Commands       map[string]LockStatus    `json:"commands,omitempty"`
}

type RuntimeState struct {
	Scrolls map[string]*RuntimeScroll `json:"scrolls"`
}

type RuntimeRoutingTarget struct {
	Name        string            `json:"name"`
	Procedure   string            `json:"procedure"`
	PortName    string            `json:"port_name"`
	Port        int               `json:"port"`
	Protocol    string            `json:"protocol"`
	Namespace   string            `json:"namespace,omitempty"`
	ServiceName string            `json:"service_name"`
	ServicePort int               `json:"service_port"`
	Selector    map[string]string `json:"selector,omitempty"`
}

type RuntimeRouteAssignment struct {
	Name       string `json:"name"`
	PortName   string `json:"port_name,omitempty"`
	Host       string `json:"host,omitempty"`
	ExternalIP string `json:"external_ip,omitempty"`
	PublicPort int    `json:"public_port,omitempty"`
	URL        string `json:"url,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
}
