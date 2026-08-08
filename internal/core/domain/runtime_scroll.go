package domain

import (
	"errors"
	"time"
)

var (
	ErrRuntimeScrollNotFound      = errors.New("runtime scroll not found")
	ErrRuntimeScrollAlreadyExists = errors.New("runtime scroll already exists")
)

type RuntimeScrollStatus string

const (
	RuntimeScrollStatusCreated RuntimeScrollStatus = "created"
	RuntimeScrollStatusRunning RuntimeScrollStatus = "running"
	RuntimeScrollStatusStopped RuntimeScrollStatus = "stopped"
	RuntimeScrollStatusError   RuntimeScrollStatus = "error"
	RuntimeScrollStatusDeleted RuntimeScrollStatus = "deleted"
)

type RuntimeScroll struct {
	ID                 string                                     `json:"id"`
	OwnerID            string                                     `json:"owner_id,omitempty"`
	Artifact           string                                     `json:"artifact"`
	ArtifactDigest     string                                     `json:"artifact_digest,omitempty"`
	Root               string                                     `json:"root"`
	ScrollName         string                                     `json:"scroll_name"`
	ScrollYAML         string                                     `json:"-"`
	Status             RuntimeScrollStatus                        `json:"status"`
	LastError          string                                     `json:"last_error,omitempty"`
	Routing            []RuntimeRouteAssignment                   `json:"routing,omitempty"`
	UIPackages         RuntimeUIPackages                          `json:"ui_packages,omitempty"`
	UIPackagePublishes map[RuntimeUIPackageScope]UIPackagePublish `json:"ui_package_publishes,omitempty"`
	CreatedAt          time.Time                                  `json:"created_at"`
	UpdatedAt          time.Time                                  `json:"updated_at"`
	Procedures         ProcedureStatusMap                         `json:"procedures,omitempty"`
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

type RuntimeUIPackageScope string

const (
	RuntimeUIPackageScopePrivate RuntimeUIPackageScope = "private"
	RuntimeUIPackageScopePublic  RuntimeUIPackageScope = "public"
)

type RuntimeUIPackages map[RuntimeUIPackageScope]RuntimeUIPackage

type RuntimeUIPackage struct {
	URL       string    `json:"url"`
	Path      string    `json:"path"`
	SHA256    string    `json:"sha256"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UIPackagePublishStatus string

const (
	UIPackagePublishPending   UIPackagePublishStatus = "pending"
	UIPackagePublishClaimed   UIPackagePublishStatus = "claimed"
	UIPackagePublishCompleted UIPackagePublishStatus = "completed"
	UIPackagePublishFailed    UIPackagePublishStatus = "failed"
)

// UIPackagePublish is durable work owned by a Druid dev workload. The daemon
// never reads package bytes from that workload.
type UIPackagePublish struct {
	ID        string                 `json:"id"`
	Scope     RuntimeUIPackageScope  `json:"scope"`
	Path      string                 `json:"path"`
	Status    UIPackagePublishStatus `json:"status"`
	ClaimPod  string                 `json:"claim_pod,omitempty"`
	SHA256    string                 `json:"sha256,omitempty"`
	Size      int64                  `json:"size,omitempty"`
	ExpiresAt time.Time              `json:"expires_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	Error     string                 `json:"error,omitempty"`
}
