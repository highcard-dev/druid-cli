package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/highcard-dev/daemon/internal/callbackapi"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var workerPullAction ports.RuntimeWorkerAction
var workerPullMode string

var WorkerPullCommand = &cobra.Command{
	Use:   "pull",
	Short: "Pull or update a runtime root and report the result",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		workerPullAction.Mode = ports.RuntimeWorkerMode(workerPullMode)
		if workerPullAction.Mode == "" {
			workerPullAction.Mode = ports.RuntimeWorkerModeCreate
		}
		if workerPullAction.TokenFile == "" {
			workerPullAction.TokenFile = os.Getenv("DRUID_WORKER_TOKEN_FILE")
		}
		result := runWorkerPull(workerPullAction)
		if result.Error != "" {
			_ = reportWorkerResult(workerPullAction, result)
			return fmt.Errorf("%s", result.Error)
		}
		return reportWorkerResult(workerPullAction, result)
	},
}

func init() {
	WorkerCommand.AddCommand(WorkerPullCommand)
	WorkerPullCommand.Flags().StringVar(&workerPullAction.Artifact, "artifact", "", "OCI artifact to pull")
	WorkerPullCommand.Flags().StringVar(&workerPullAction.RuntimeID, "runtime-id", "", "Runtime scroll id")
	WorkerPullCommand.Flags().StringVar(&workerPullAction.MountPath, "root", "/scroll", "Mounted runtime root path")
	WorkerPullCommand.Flags().StringVar(&workerPullAction.CallbackURL, "callback-url", "", "Daemon worker callback URL")
	WorkerPullCommand.Flags().StringVar(&workerPullAction.TokenFile, "callback-token-file", "", "Projected ServiceAccount token file for callbacks")
	WorkerPullCommand.Flags().BoolVar(&workerPullAction.PreserveReleaseManifest, "preserve-release-manifest", false, "Restore the release manifest.json carried by a backup")
	WorkerPullCommand.Flags().StringVar(&workerPullMode, "mode", string(ports.RuntimeWorkerModeCreate), "Pull mode: create, update, or restore")
	WorkerPullCommand.MarkFlagRequired("artifact")
	WorkerPullCommand.MarkFlagRequired("runtime-id")
}

func runWorkerPull(action ports.RuntimeWorkerAction) ports.RuntimeWorkerResult {
	result := ports.RuntimeWorkerResult{}
	if action.Artifact == "" {
		result.Error = "artifact is required"
		return result
	}
	root := action.MountPath
	if root == "" {
		root = "/scroll"
	}
	oci := registry.NewOciClient(loadWorkerRegistryStore())
	digest, err := oci.ResolveDigest(action.Artifact)
	if err == nil {
		result.ArtifactDigest = digest
	}
	switch action.Mode {
	case ports.RuntimeWorkerModeUpdate:
		err = pullWorkerUpdate(root, action.Artifact, oci)
	case ports.RuntimeWorkerModeRestore:
		err = pullWorkerRestore(root, action.Artifact, oci)
	default:
		err = pullWorkerCreate(root, action.Artifact, oci)
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	scrollYAML, err := os.ReadFile(filepath.Join(root, "scroll.yaml"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if _, err := domain.NewScrollFromBytes(root, scrollYAML); err != nil {
		result.Error = err.Error()
		return result
	}
	result.ScrollYAML = string(scrollYAML)
	return result
}

func loadWorkerRegistryStore() *registry.CredentialStore {
	var config struct {
		Registries []domain.RegistryCredential `json:"registries"`
	}
	if raw := os.Getenv("DRUID_RUNTIME_REGISTRY_CONFIG_JSON"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &config)
	}
	if len(config.Registries) == 0 {
		_ = viper.UnmarshalKey("registries", &config.Registries)
	}
	if len(config.Registries) == 0 {
		if path := viper.ConfigFileUsed(); path != "" {
			if raw, err := os.ReadFile(path); err == nil {
				_ = json.Unmarshal(raw, &config)
			}
		}
	}
	return registry.NewCredentialStore(config.Registries)
}

func pullWorkerCreate(root string, artifact string, oci ports.OciRegistryInterface) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	if info, err := os.Stat(artifact); err == nil {
		if !info.IsDir() {
			if filepath.Base(artifact) != "scroll.yaml" {
				return fmt.Errorf("local file artifact must be scroll.yaml")
			}
			return copyPath(artifact, filepath.Join(root, "scroll.yaml"))
		}
		return copyPath(artifact, root)
	}
	return oci.PullSelective(root, artifact, true, nil)
}

func pullWorkerUpdate(root string, artifact string, oci ports.OciRegistryInterface) error {
	tmp, err := os.MkdirTemp("", "druid-worker-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := coreservices.MaterializeScrollArtifact(artifact, tmp, oci, true); err != nil {
		return err
	}
	scrollYAML, err := os.ReadFile(filepath.Join(tmp, "scroll.yaml"))
	if err != nil {
		return err
	}
	scroll, err := domain.NewScrollFromBytes(tmp, scrollYAML)
	if err != nil {
		return err
	}
	skipData := map[string]bool{}
	collectSkipUpdatePaths(skipData, "", scroll.Chunks)
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".druid-worker-update-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := copyPath(tmp, stage); err != nil {
		return err
	}
	if err := preserveSkippedUpdateData(root, stage, skipData); err != nil {
		return err
	}
	return replaceRestoredRoot(root, stage)
}

// preserveSkippedUpdateData copies only the paths a Scroll explicitly marks as
// skip_update from the installed root into the staged candidate. Candidate
// content is otherwise complete, so unprotected files absent from the
// candidate are removed when the staged root replaces the installed root.
func preserveSkippedUpdateData(root string, stage string, skipData map[string]bool) error {
	for skip := range skipData {
		skip = filepath.ToSlash(filepath.Clean(skip))
		if skip == "." {
			skip = ""
		}
		source := filepath.Join(root, domain.RuntimeDataDir, filepath.FromSlash(skip))
		target := filepath.Join(stage, domain.RuntimeDataDir, filepath.FromSlash(skip))
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if _, err := os.Lstat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyPath(source, target); err != nil {
			return err
		}
	}
	return nil
}

func pullWorkerRestore(root string, artifact string, oci ports.OciRegistryInterface) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".druid-worker-restore-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	puller, ok := oci.(interface {
		PullSelectiveWithOptions(string, string, bool, *domain.SnapshotProgress, registry.TransferOptions) error
	})
	if !ok {
		return fmt.Errorf("OCI registry does not support preserve-release-manifest")
	}
	if err := puller.PullSelectiveWithOptions(stage, artifact, true, nil, registry.TransferOptions{PreserveReleaseManifest: true}); err != nil {
		return err
	}
	return replaceRestoredRoot(root, stage)
}

const restoreRootUnsafePrefix = "restore root may be partial:"

func replaceRestoredRoot(root string, stage string) error {
	return replaceRestoredRootWithRename(root, stage, os.Rename)
}

// replaceRestoredRootWithRename swaps staged artifact entries into a runtime
// root without copying over a live tree. If an entry move fails, it restores
// the original entries before returning. An error with restoreRootUnsafePrefix
// means that rollback itself failed and callers must keep the runtime stopped.
func replaceRestoredRootWithRename(root string, stage string, rename func(string, string) error) error {
	rollback, err := os.MkdirTemp(root, ".druid-worker-restore-rollback-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(rollback)

	stageName := filepath.Base(stage)
	rollbackName := filepath.Base(rollback)
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	original := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() != stageName && entry.Name() != rollbackName {
			original = append(original, entry.Name())
		}
	}
	stagedEntries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}

	restoreOriginal := func(installed []string) error {
		for _, name := range installed {
			if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
				return err
			}
		}
		for _, name := range original {
			from := filepath.Join(rollback, name)
			if _, err := os.Lstat(from); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return err
			}
			if err := rename(from, filepath.Join(root, name)); err != nil {
				return err
			}
		}
		return nil
	}

	for _, entry := range original {
		if err := rename(filepath.Join(root, entry), filepath.Join(rollback, entry)); err != nil {
			if rollbackErr := restoreOriginal(nil); rollbackErr != nil {
				return fmt.Errorf("%s failed to restore original runtime after moving %s: %w", restoreRootUnsafePrefix, entry, rollbackErr)
			}
			return fmt.Errorf("restore transaction rolled back while moving original entry %s: %w", entry, err)
		}
	}

	installed := make([]string, 0, len(stagedEntries))
	for _, entry := range stagedEntries {
		name := entry.Name()
		if err := rename(filepath.Join(stage, name), filepath.Join(root, name)); err != nil {
			if rollbackErr := restoreOriginal(installed); rollbackErr != nil {
				return fmt.Errorf("%s failed to restore original runtime after staging %s: %w", restoreRootUnsafePrefix, name, rollbackErr)
			}
			return fmt.Errorf("restore transaction rolled back while staging %s: %w", name, err)
		}
		installed = append(installed, name)
	}
	return nil
}

func collectSkipUpdatePaths(out map[string]bool, parent string, chunks []*domain.Chunks) {
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		chunkPath := filepath.ToSlash(filepath.Clean(filepath.Join(parent, filepath.FromSlash(chunk.Path))))
		if chunkPath == "." {
			chunkPath = ""
		}
		if chunk.SkipUpdate {
			out[chunkPath] = true
		}
		collectSkipUpdatePaths(out, chunkPath, chunk.Chunks)
	}
}

func copyPath(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dst, rel)
			if entry.IsDir() {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				return os.MkdirAll(target, info.Mode().Perm())
			}
			return copyPath(path, target)
		})
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func reportWorkerResult(action ports.RuntimeWorkerAction, result ports.RuntimeWorkerResult) error {
	if action.CallbackURL == "" {
		body, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(body))
		return nil
	}
	suffix := "/internal/v1/workers/" + action.RuntimeID + "/complete"
	base := strings.TrimSuffix(action.CallbackURL, suffix)
	if base == action.CallbackURL || base == "" {
		return fmt.Errorf("worker callback URL %q must end with %s", action.CallbackURL, suffix)
	}
	client, err := callbackapi.NewClientWithResponses(base)
	if err != nil {
		return err
	}
	body := callbackapi.WorkerResult{
		ArtifactDigest: workerString(result.ArtifactDigest),
		Error:          workerString(result.Error),
		ScrollYaml:     workerString(result.ScrollYAML),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := client.CompleteWorkerWithResponse(ctx, action.RuntimeID, body, func(_ context.Context, request *http.Request) error {
		if action.TokenFile == "" {
			return nil
		}
		token, err := os.ReadFile(action.TokenFile)
		if err != nil {
			return fmt.Errorf("read worker token: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		return nil
	})
	if err != nil {
		return err
	}
	if res.StatusCode() >= 400 {
		return fmt.Errorf("worker callback returned %d: %s", res.StatusCode(), strings.TrimSpace(string(res.Body)))
	}
	return nil
}

func workerString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
