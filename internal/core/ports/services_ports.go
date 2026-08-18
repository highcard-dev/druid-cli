package ports

import (
	"context"
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

type LogManagerInterface interface {
	GetStreams() map[string]*domain.Log
	AddLine(stream string, sc []byte)
}

type RuntimeBackendInterface interface {
	Name() string
	RootRef(id string, namespace string) string
	RunCommand(command RuntimeCommand) (*int, error)
	CreateUIPackageUpload(ctx context.Context, action RuntimeUIPackageUploadAction) (RuntimeUIPackageUpload, error)
	ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port, reservedPorts []domain.Port) ([]domain.RuntimePortStatus, error)
	RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port, reservedPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error)
	StopRuntime(root string) error
	DeleteRuntime(root string, purgeData bool) error
	BackupRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error
	SpawnPullWorker(ctx context.Context, action RuntimeWorkerAction) error
	Attach(commandName string, data string) error
	Signal(commandName string, target string, signal string, root string) error
}

// RuntimeCommandStopper is implemented by backends that can remove one
// command's workloads without stopping the rest of the runtime.
type RuntimeCommandStopper interface {
	StopCommand(root string, command string) error
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
	Name                    string
	ScrollID                string
	Command                 *domain.CommandInstructionSet
	Root                    string
	GlobalPorts             []domain.Port
	ReservedPorts           []domain.Port
	Routing                 []domain.RuntimeRouteAssignment
	ProcedureEnv            map[string]map[string]string
	ProcedureStatusObserver func(procedure string, status domain.ScrollLockStatus, exitCode *int)
}

func (c RuntimeCommand) ObserveProcedureStatus(procedure string, status domain.ScrollLockStatus, exitCode *int) {
	if c.ProcedureStatusObserver != nil {
		c.ProcedureStatusObserver(procedure, status, exitCode)
	}
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

type BroadcastChannelInterface interface {
	NewHub() *domain.BroadcastChannel
	Run()
}

type ConsoleManagerInterface interface {
	GetConsole(consoleId string) *domain.Console
	GetConsoles() map[string]*domain.Console
	AddConsoleWithChannel(consoleId string, consoleType domain.ConsoleType, inputMode string, channel chan string) (*domain.Console, chan struct{})
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
