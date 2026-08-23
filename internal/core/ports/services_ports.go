package ports

import (
	"context"
	"io"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
)

type AuthorizerServiceInterface interface {
	CheckHeader(r *fiber.Ctx) (*AuthContext, error)
	CheckQuery(runtimeID string, token string) (*AuthContext, error)
	GenerateQueryToken(runtimeID string, ownerID string) string
}

type AuthContext struct {
	Subject   string
	RuntimeID string
	ExpiresAt *time.Time
}

type ScrollServiceInterface interface {
	GetCurrent() *domain.Scroll
	GetFile() *domain.File
	GetDir() string
	GetCwd() string
	GetCommand(cmd string) (*domain.CommandInstructionSet, error)
}

type RuntimeBackendInterface interface {
	Name() string
	RootRef(id string, namespace string) string
	RunCommand(command RuntimeCommand) (*int, error)
	CreateUIPackageUpload(ctx context.Context, action RuntimeUIPackageUploadAction) (RuntimeUIPackageUpload, error)
	ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, ports []domain.Port) ([]domain.RuntimePortStatus, error)
	RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, ports []domain.Port) ([]domain.RuntimeRoutingTarget, error)
	StopCommand(root string, command string) error
	StopRuntime(root string) error
	DeleteRuntime(root string, purgeData bool) error
	BackupRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error
	SpawnPullWorker(ctx context.Context, action RuntimeWorkerAction) (<-chan error, error)
	OpenConsole(ctx context.Context, root string, procedure string) (io.ReadWriteCloser, error)
	Signal(commandName string, target string, signal string, root string) error
}

// RuntimeBackendFactory creates a backend with its required status observer.
type RuntimeBackendFactory interface {
	Create(observer ProcedureStatusObserver) (RuntimeBackendInterface, error)
}

// RuntimeBackendFactoryFunc adapts a function to RuntimeBackendFactory.
type RuntimeBackendFactoryFunc func(observer ProcedureStatusObserver) (RuntimeBackendInterface, error)

func (f RuntimeBackendFactoryFunc) Create(observer ProcedureStatusObserver) (RuntimeBackendInterface, error) {
	return f(observer)
}

type RuntimeWorkerCallbackConfig struct {
	Listen string
	URL    string
}

type RuntimeWorkerCallbackBackend interface {
	WorkerCallbackDefaults(config RuntimeWorkerCallbackConfig) RuntimeWorkerCallbackConfig
	WorkerCallbackAfterListen(config RuntimeWorkerCallbackConfig) (RuntimeWorkerCallbackConfig, error)
}

// RuntimeWorkloadIdentity is the verified Kubernetes identity of a Druid
// managed pod. RuntimeID comes exclusively from labels on the live pod.
type RuntimeWorkloadIdentity struct {
	Namespace      string
	ServiceAccount string
	PodName        string
	PodUID         string
	RuntimeID      string
	Kind           string
}

type RuntimeWorkloadAuthenticator interface {
	AuthenticateWorkload(ctx context.Context, token string) (RuntimeWorkloadIdentity, error)
}

type RuntimeScrollStore interface {
	StateDir() string
	Root(id string) string
	CreateScroll(scroll *domain.RuntimeScroll) error
	ListScrolls() ([]*domain.RuntimeScroll, error)
	GetScroll(id string) (*domain.RuntimeScroll, error)
	UpdateScroll(scroll *domain.RuntimeScroll) error
	DeleteScroll(id string) error
}

type RuntimeCommand struct {
	// Name identifies the command within the Scroll.
	Name string
	// ScrollID identifies the runtime Scroll whose command is being executed.
	ScrollID string
	// Command contains the procedures and run mode to execute.
	Command *domain.CommandInstructionSet
	// Root identifies the backend-owned runtime data location.
	Root string
	// Ports contains all runtime ports after merging reservations and resolving dynamic assignments.
	Ports []domain.Port
	// Routing contains the runtime's external route assignments used by backends during execution.
	Routing []domain.RuntimeRouteAssignment
	// ProcedureEnv contains the fully resolved environment for each procedure name.
	ProcedureEnv map[string]map[string]string
}

// ProcedureStatusUpdate identifies one backend-reported procedure transition.
type ProcedureStatusUpdate struct {
	RuntimeID string
	Command   string
	Procedure string
	Status    domain.ScrollLockStatus
	ExitCode  *int
}

// ProcedureStatusObserver receives procedure transitions from a runtime backend.
type ProcedureStatusObserver interface {
	ObserveProcedureStatus(update ProcedureStatusUpdate)
}

// ProcedureStatusObserverFunc adapts a function to ProcedureStatusObserver.
type ProcedureStatusObserverFunc func(update ProcedureStatusUpdate)

func (f ProcedureStatusObserverFunc) ObserveProcedureStatus(update ProcedureStatusUpdate) {
	f(update)
}

type RuntimeUIPackageUploadAction struct {
	RuntimeID string
	RootRef   string
	Scope     domain.RuntimeUIPackageScope
	RequestID string
}

// RuntimeUIPackageUpload exposes a short-lived upload URL and the immutable
// public URL of the unique object it writes.
type RuntimeUIPackageUpload struct {
	UploadURL string
	URL       string
}

type RuntimeMaterialization struct {
	Artifact       string
	ArtifactDigest string
	Root           string
	ScrollYAML     []byte
}

type RuntimeWorkerMode string

const (
	RuntimeWorkerModeCreate  RuntimeWorkerMode = "create"
	RuntimeWorkerModeUpdate  RuntimeWorkerMode = "update"
	RuntimeWorkerModeRestore RuntimeWorkerMode = "restore"
)

type RuntimeWorkerAction struct {
	Mode                RuntimeWorkerMode
	RuntimeID           string
	Artifact            string
	Storage             string
	RootRef             string
	MountPath           string
	CallbackURL         string
	TokenFile           string
	RegistryCredentials []domain.RegistryCredential
}

type RuntimeWorkerResult struct {
	ScrollYAML     string `json:"scroll_yaml,omitempty"`
	ArtifactDigest string `json:"artifact_digest,omitempty"`
	Error          string `json:"error,omitempty"`
}

type OciRegistryInterface interface {
	GetRepo(repoUrl string) (*remote.Repository, error)
	FetchFile(artifact string, filePath string) ([]byte, error)
	ResolveDigest(artifact string) (string, error)
	ResolveAnnotationInfo(artifact string) (domain.AnnotationInfo, error)
	Pull(dir string, artifact string) error
	PullSelective(dir string, artifact string, includeData bool, progress *domain.SnapshotProgress) error
	CanUpdateTag(descriptor v1.Descriptor, folder string, tag string) (bool, error)
	Push(folder string, repo string, tag string, overrides map[string]string, packMeta bool, scrollFile *domain.File) (v1.Descriptor, error)
}

type QueueManagerInterface interface {
	AddTempItem(cmd string) error
	AddTempItemWithWait(cmd string) error
	GetQueue() map[string]domain.ScrollLockStatus
}

type PortServiceInterface interface {
	GetPorts() []*domain.AugmentedPort
}

type ColdStarterHandlerInterface interface {
	GetHandler(funcs map[string]func(data ...string)) (ColdStarterPacketHandlerInterface, error)
	SetFinishedAt(finishedAt *time.Time)
	Close() error
}

type ColdStarterPacketHandlerInterface interface {
	Handle(data []byte, funcs map[string]func(data ...string)) error
}

type ColdStarterInterface interface {
	Stop()
	Finish(*domain.AugmentedPort)
}

type ColdStarterServerInterface interface {
	Start(port int, onFinish func()) error
	Close() error
}

type UiServiceInterface interface {
	GetIndex(filePath string) ([]string, error)
}

type WatchServiceInterface interface {
	StartWatching(basePath string, paths ...string) error
	StopWatching() error
	Trigger()
	Subscribe() chan *[]byte
	Unsubscribe(client chan *[]byte)
	GetWatchedPaths() []string
	IsWatching() bool
	SetHotReloadCommands(procs []string) error
}
