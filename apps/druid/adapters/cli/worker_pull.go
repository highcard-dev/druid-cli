package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		if workerPullAction.CallbackToken == "" {
			workerPullAction.CallbackToken = os.Getenv("DRUID_WORKER_TOKEN")
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
	WorkerPullCommand.Flags().StringVar(&workerPullAction.CallbackToken, "callback-token", "", "One-time worker callback token")
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
	return mergePulledRoot(tmp, root, skipData)
}

func pullWorkerRestore(root string, artifact string, oci ports.OciRegistryInterface) error {
	tmp, err := os.MkdirTemp("", "druid-worker-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := coreservices.MaterializeScrollArtifact(artifact, tmp, oci, true); err != nil {
		return err
	}
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
	return copyPath(tmp, root)
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

func mergePulledRoot(src string, dst string, skipData map[string]bool) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		if name == domain.RuntimeDataDir {
			if err := copyDataUpdate(srcPath, dstPath, skipData); err != nil {
				return err
			}
			continue
		}
		if err := os.RemoveAll(dstPath); err != nil {
			return err
		}
		if err := copyPath(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyDataUpdate(srcData string, dstData string, skipData map[string]bool) error {
	return filepath.WalkDir(srcData, func(srcPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcData, srcPath)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dstData, 0755)
		}
		rel = filepath.ToSlash(rel)
		if shouldSkipWorkerUpdate(rel, skipData) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dstData, filepath.FromSlash(rel))
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyPath(srcPath, target)
	})
}

func shouldSkipWorkerUpdate(rel string, skipData map[string]bool) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for skip := range skipData {
		if skip == "" || rel == skip || strings.HasPrefix(rel, skip+"/") {
			return true
		}
	}
	return false
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
		Token:          action.CallbackToken,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := client.CompleteWorkerWithResponse(ctx, action.RuntimeID, body)
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
