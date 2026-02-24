package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/zap"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type OciClient struct {
	host     string
	username string
	password string
}

func NewOciClient(host string, username string, password string) *OciClient {
	return &OciClient{
		host:     host,
		username: username,
		password: password,
	}
}

func (c *OciClient) GetRepo(repoUrl string) (*remote.Repository, error) {

	repo, err := remote.NewRepository(repoUrl)

	if err != nil {
		return nil, err
	}

	if c.host == "" {
		return nil, fmt.Errorf("registry host must be set. Please use `druid registry login` to set them")
	}

	if c.username == "" || c.password == "" {
		logger.Log().Warn("No registry credentials found. Trying to pull anonymously")
	} else {
		repo.Client = &auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.DefaultCache,
			Credential: auth.StaticCredential(c.host, auth.Credential{
				Username: c.username,
				Password: c.password,
			}),
		}
	}

	return repo, nil
}

func (c *OciClient) Pull(dir string, artifact string) error {
	return c.PullSelective(dir, artifact, true, nil)
}

func (c *OciClient) PullSelective(dir string, artifact string, includeData bool, progress *domain.SnapshotProgress) error {

	repo, tag := utils.SplitArtifact(artifact)

	if tag == "" {
		return fmt.Errorf("tag must be set")
	}

	ctx := context.Background()

	repoInstance, err := c.GetRepo(repo)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	fs, err := file.New(filepath.Join(dir))
	if err != nil {
		return err
	}

	// Track progress for data pulls
	var completed atomic.Int64
	var totalLayers atomic.Int64

	if progress != nil {
		progress.Mode.Store("restore")
		progress.Percentage.Store(0)
	}

	copyOpts := oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			FindSuccessors: func(ctx context.Context, fetcher content.Fetcher, desc v1.Descriptor) ([]v1.Descriptor, error) {
				successors, err := content.Successors(ctx, fetcher, desc)
				if err != nil {
					return nil, err
				}

				if !includeData {
					filtered := make([]v1.Descriptor, 0, len(successors))
					for _, s := range successors {
						// Filter out data layers by checking media type.
						// ORAS appends +gzip to the media type for directories.
						baseType := strings.TrimSuffix(s.MediaType, "+gzip")
						if baseType == string(domain.ArtifactTypeScrollData) {
							logger.Log().Debug("Skipping data layer", zap.String("digest", s.Digest.String()))
							continue
						}
						filtered = append(filtered, s)
					}
					totalLayers.Store(int64(len(filtered)))
					return filtered, nil
				}

				totalLayers.Store(int64(len(successors)))
				return successors, nil
			},
			PostCopy: func(ctx context.Context, desc v1.Descriptor) error {
				if progress != nil {
					done := completed.Add(1)
					total := totalLayers.Load()
					if total > 0 {
						pct := done * 100 / total
						progress.Percentage.Store(pct)
					}
					logger.Log().Info("Pulled layer", zap.String("digest", desc.Digest.String()), zap.Int64("completed", done), zap.Int64("total", total))
				}
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc v1.Descriptor) error {
				if progress != nil {
					done := completed.Add(1)
					total := totalLayers.Load()
					if total > 0 {
						pct := done * 100 / total
						progress.Percentage.Store(pct)
					}
				}
				return nil
			},
		},
	}

	manifestDescriptor, err := oras.Copy(ctx, repoInstance, tag, fs, tag, copyOpts)
	if err != nil {
		if progress != nil {
			progress.Mode.Store("noop")
		}
		return err
	}

	if progress != nil {
		progress.Percentage.Store(100)
		progress.Mode.Store("noop")
	}

	logger.Log().Info("Manifest pulled", zap.String("digest", manifestDescriptor.Digest.String()), zap.String("mediaType", manifestDescriptor.MediaType))

	jsonData, err := json.Marshal(&manifestDescriptor)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest descriptor: %w", err)
	}

	bs, err := content.FetchAll(ctx, repoInstance, manifestDescriptor)
	if err != nil {
		return fmt.Errorf("failed to fetch manifest descriptor: %w", err)
	}

	fullDesc := v1.Descriptor{}
	err = json.Unmarshal(bs, &fullDesc)
	if err != nil {
		return fmt.Errorf("failed to unmarshal manifest descriptor: %w", err)
	}

	annotations := fullDesc.Annotations
	fileName := filepath.Join(dir, "manifest.json")
	err = os.WriteFile(fileName, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write manifest descriptor: %w", err)
	}

	annotationsJson, err := json.Marshal(annotations)
	if err != nil {
		return fmt.Errorf("failed to marshal annotations: %w", err)
	}
	fileName = filepath.Join(dir, "annotations.json")
	err = os.WriteFile(fileName, annotationsJson, 0644)
	if err != nil {
		return fmt.Errorf("failed to write annotations: %w", err)
	}

	return nil
}

func (c *OciClient) CanUpdateTag(current v1.Descriptor, r string, tag string) (bool, error) {

	repo, err := remote.NewRepository(r)

	if err != nil {
		return false, err
	}
	disc, err := oras.Resolve(context.TODO(), repo, tag, oras.DefaultResolveOptions)
	if err != nil {
		return false, err
	}

	if current.Digest.String() != disc.Digest.String() {
		return true, nil
	}

	return false, nil

}

func (c *OciClient) PackFolders(fs *file.Store, dirs []string, artifactType domain.ArtifactType, path string) ([]v1.Descriptor, error) {

	ctx := context.Background()

	fileDescriptors := make([]v1.Descriptor, 0, len(dirs))
	for _, name := range dirs {

		fullPath := filepath.Join(path, name)

		fileDescriptor, err := fs.Add(ctx, name, string(artifactType), fullPath)
		if err != nil {
			return []v1.Descriptor{}, err
		}
		fileDescriptors = append(fileDescriptors, fileDescriptor)
		logger.Log().Info(fmt.Sprintf("file descriptor for %s: %v\n", name, fileDescriptor.Digest))
	}

	return fileDescriptors, nil
}

// the root has to leaves, one is the real scroll (fs) and the other is meta information about the scroll
func (c *OciClient) Push(folder string, repo string, tag string, annotationInfo domain.AnnotationInfo, packMeta bool, scrollFile *domain.File) (v1.Descriptor, error) {

	availableFileNames := []string{"update", "scroll.yaml", "packet_handler", "public", "private"}
	fsFileNames := []string{}

	//check if files exisits (file or folder) and remove from slice if not
	for _, fileName := range availableFileNames {
		exists, _ := utils.FileExists(filepath.Join(folder, fileName))
		if exists {
			fsFileNames = append(fsFileNames, fileName)
		} else {
			logger.Log().Warn(fmt.Sprintf("path %s does not exist, skipping", fileName))
		}
	}

	if len(fsFileNames) == 0 {
		return v1.Descriptor{}, fmt.Errorf("no files found to push")
	}

	fs, err := file.New(folder)
	if err != nil {
		return v1.Descriptor{}, err
	}
	defer fs.Close()

	repoInstance, err := c.GetRepo(repo)

	if err != nil {
		return v1.Descriptor{}, err
	}

	scrollFsManifestDescriptor, err := c.PackFolders(fs, fsFileNames, domain.ArtifactTypeScrollFs, "")

	if err != nil {
		return v1.Descriptor{}, err
	}
	descriptorsForRoot := scrollFsManifestDescriptor

	if packMeta {

		scrollMetaManifestDescriptor, err := c.CreateMetaDescriptors(fs, folder, ".meta")
		if err != nil {
			return v1.Descriptor{}, err
		}
		descriptorsForRoot = append(descriptorsForRoot, scrollMetaManifestDescriptor...)
	}

	// Pack data directory chunks
	dataDir := utils.GetDataDirFromScrollDir(folder)
	dataExists, _ := utils.FileExists(dataDir)
	if dataExists {
		var explicitChunks []*domain.Chunks
		if scrollFile != nil {
			explicitChunks = scrollFile.Chunks
		}
		chunks, err := utils.AutoChunkDataDir(dataDir, explicitChunks)
		if err != nil {
			return v1.Descriptor{}, fmt.Errorf("failed to auto-chunk data directory: %w", err)
		}
		for _, chunk := range chunks {
			chunkFullPath := filepath.Join(dataDir, chunk.Path)
			chunkExists, _ := utils.FileExists(chunkFullPath)
			if !chunkExists {
				logger.Log().Warn(fmt.Sprintf("data chunk path %s does not exist, skipping", chunk.Path))
				continue
			}
			// Name the layer "data/<path>" so it extracts to the correct location on pull
			layerName := filepath.Join("data", chunk.Path)
			desc, err := fs.Add(context.Background(), layerName, string(domain.ArtifactTypeScrollData), chunkFullPath)
			if err != nil {
				return v1.Descriptor{}, fmt.Errorf("failed to pack data chunk %s: %w", chunk.Name, err)
			}
			logger.Log().Info(fmt.Sprintf("packed data chunk %s: %v", chunk.Name, desc.Digest))
			descriptorsForRoot = append(descriptorsForRoot, desc)
		}
	} else {
		logger.Log().Info("No data directory found, skipping data chunk packing")
	}

	ctx := context.Background()

	annotations := map[string]string{}
	if annotationInfo.MinRam != "" {
		annotations["gg.druid.scroll.minRam"] = annotationInfo.MinRam
	}
	if annotationInfo.MinDisk != "" {
		annotations["gg.druid.scroll.minDisk"] = annotationInfo.MinDisk
	}
	if annotationInfo.MinCpu != "" {
		annotations["gg.druid.scroll.minCpu"] = annotationInfo.MinCpu
	}
	if annotationInfo.Image != "" {
		annotations["gg.druid.scroll.image"] = annotationInfo.Image
	}
	if annotationInfo.Smart {
		annotations["gg.druid.scroll.smart"] = "true"
	}

	for name, port := range annotationInfo.Ports {
		annotations[fmt.Sprintf("gg.druid.scroll.port.%s", name)] = port
	}

	opts := oras.PackManifestOptions{
		Layers:              descriptorsForRoot,
		ManifestAnnotations: annotations,
	}
	rootManifestDescriptor, err := oras.PackManifest(ctx, fs, oras.PackManifestVersion1_1, string(domain.ArtifactTypeScrollRoot), opts)
	if err != nil {
		return v1.Descriptor{}, err
	}

	logger.Log().Info(fmt.Sprintf("root manifest descriptor: %v\n", rootManifestDescriptor.Digest))

	if err = fs.Tag(ctx, rootManifestDescriptor, tag); err != nil {
		return v1.Descriptor{}, err
	}

	pushCopyOpts := oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			PostCopy: func(ctx context.Context, desc v1.Descriptor) error {
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Pushed layer",
					zap.String("digest", desc.Digest.String()),
					zap.String("mediaType", desc.MediaType),
					zap.Int64("size", desc.Size),
					zap.String("title", title),
				)
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc v1.Descriptor) error {
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Layer already exists in registry, skipping",
					zap.String("digest", desc.Digest.String()),
					zap.String("mediaType", desc.MediaType),
					zap.Int64("size", desc.Size),
					zap.String("title", title),
				)
				return nil
			},
		},
	}
	_, err = oras.Copy(ctx, fs, tag, repoInstance, tag, pushCopyOpts)

	return rootManifestDescriptor, err
}

func (c *OciClient) PushMeta(folder string, repo string) (v1.Descriptor, error) {

	fs, err := file.New(folder)
	if err != nil {
		return v1.Descriptor{}, err
	}
	defer fs.Close()

	repoInstance, err := c.GetRepo(repo)

	if err != nil {
		return v1.Descriptor{}, err
	}

	manifestDescriptors, err := c.CreateMetaDescriptors(fs, folder, "")

	ctx := context.Background()

	if err != nil {
		return v1.Descriptor{}, err
	}

	rootManifestDescriptor, err := oras.PackManifest(ctx, fs, oras.PackManifestVersion1_1, string(domain.ArtifactTypeScrollRoot), oras.PackManifestOptions{
		Layers: manifestDescriptors,
	})

	if err != nil {
		return v1.Descriptor{}, err
	}

	logger.Log().Info(fmt.Sprintf("Meta manifest descriptor: %v\n", rootManifestDescriptor.Digest))

	tag := "meta"
	if err = fs.Tag(ctx, rootManifestDescriptor, tag); err != nil {
		return v1.Descriptor{}, err
	}

	metaCopyOpts := oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			PostCopy: func(ctx context.Context, desc v1.Descriptor) error {
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Pushed layer",
					zap.String("digest", desc.Digest.String()),
					zap.String("mediaType", desc.MediaType),
					zap.Int64("size", desc.Size),
					zap.String("title", title),
				)
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc v1.Descriptor) error {
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Layer already exists in registry, skipping",
					zap.String("digest", desc.Digest.String()),
					zap.String("mediaType", desc.MediaType),
					zap.Int64("size", desc.Size),
					zap.String("title", title),
				)
				return nil
			},
		},
	}
	_, err = oras.Copy(ctx, fs, tag, repoInstance, tag, metaCopyOpts)

	return rootManifestDescriptor, err
}

func (c *OciClient) CreateMetaDescriptors(fs *file.Store, folder string, fsPath string) ([]v1.Descriptor, error) {

	metaPath := filepath.Join(folder, fsPath)
	exists, _ := utils.FileExists(metaPath)
	if !exists {
		return []v1.Descriptor{}, fmt.Errorf("meta file %s not found", metaPath)
	}
	fsFileNames := []string{}
	subitems, _ := os.ReadDir(metaPath)
	for _, subitem := range subitems {
		fsFileNames = append(fsFileNames, subitem.Name())
	}

	return c.PackFolders(fs, fsFileNames, domain.ArtifactTypeScrollMeta, fsPath)
}
