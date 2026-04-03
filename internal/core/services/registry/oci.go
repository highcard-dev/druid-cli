package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

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
	credentialStore *CredentialStore
	// httpClient optionally overrides the HTTP client used by GetRepo.
	// When nil, the default retry client or auth client is used.
	httpClient *http.Client
	// plainHTTP forces plain HTTP (no TLS) for registry communication.
	plainHTTP bool
}

func NewOciClient(credentialStore *CredentialStore) *OciClient {
	return &OciClient{
		credentialStore: credentialStore,
	}
}

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func startProgressTicker(direction string, bytesTransferred *atomic.Int64, totalBytes *atomic.Int64, layersDone *atomic.Int64, totalLayers *atomic.Int64) func() {
	ticker := time.NewTicker(60 * time.Second)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				transferred := bytesTransferred.Load()
				total := totalBytes.Load()
				layers := layersDone.Load()
				layerTotal := totalLayers.Load()
				logger.Log().Info(fmt.Sprintf("%s progress", direction),
					zap.String("transferred", formatBytes(transferred)),
					zap.String("total", formatBytes(total)),
					zap.String("layers", fmt.Sprintf("%d/%d", layers, layerTotal)),
				)
			case <-done:
				return
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
	}
}

func (c *OciClient) GetRepo(repoUrl string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(repoUrl)
	if err != nil {
		return nil, err
	}

	repo.PlainHTTP = c.plainHTTP

	httpClient := retry.DefaultClient
	if c.httpClient != nil {
		httpClient = c.httpClient
	}

	cred, _ := c.credentialStore.CredentialForRepo(repoUrl)
	if cred.Username == "" || cred.Password == "" {
		logger.Log().Warn("No registry credentials found for " + repoUrl + ". Trying to pull anonymously")
		if c.httpClient != nil {
			repo.Client = &auth.Client{
				Client: httpClient,
				Cache:  auth.DefaultCache,
			}
		}
	} else {
		host := extractHost(repoUrl)
		repo.Client = &auth.Client{
			Client:     httpClient,
			Cache:      auth.DefaultCache,
			Credential: auth.StaticCredential(host, cred),
		}
	}

	return repo, nil
}

func extractHost(repoUrl string) string {
	repoUrl = strings.TrimPrefix(repoUrl, "https://")
	repoUrl = strings.TrimPrefix(repoUrl, "http://")
	if idx := strings.Index(repoUrl, "/"); idx > 0 {
		return repoUrl[:idx]
	}
	return repoUrl
}

func ValidateCredentials(host, username, password string) error {
	registryHost := extractHost(host)

	reg, err := remote.NewRegistry(registryHost)
	if err != nil {
		return fmt.Errorf("invalid registry host: %w", err)
	}

	reg.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.DefaultCache,
		Credential: auth.StaticCredential(registryHost, auth.Credential{
			Username: username,
			Password: password,
		}),
	}

	if err := reg.Ping(context.Background()); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	return nil
}

func (c *OciClient) Pull(dir string, artifact string) error {
	return c.PullSelective(dir, artifact, true, nil)
}

func (c *OciClient) PullSelective(dir string, artifact string, includeData bool, progress *domain.SnapshotProgress) error {

	repo, ref, _ := utils.ParseArtifactRef(artifact)
	if repo == "" || ref == "" {
		return fmt.Errorf("reference (tag or digest) must be set")
	}

	logger.Log().Info("Starting pull from registry",
		zap.String("repo", repo),
		zap.String("ref", ref),
		zap.Bool("includeData", includeData),
	)

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

	var completed atomic.Int64
	var totalLayers atomic.Int64
	var totalPullBytes atomic.Int64
	var bytesDownloaded atomic.Int64

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
						baseType := strings.TrimSuffix(s.MediaType, "+gzip")
						if baseType == string(domain.ArtifactTypeScrollData) {
							path := s.Annotations["org.opencontainers.image.path"]
							logger.Log().Debug("Skipping data layer", zap.String("digest", s.Digest.String()), zap.String("path", path))
							continue
						}
						filtered = append(filtered, s)
					}
					totalLayers.Store(int64(len(filtered)))
					var size int64
					for _, s := range filtered {
						size += s.Size
					}
					totalPullBytes.Store(size)
					return filtered, nil
				}

				totalLayers.Store(int64(len(successors)))
				var size int64
				for _, s := range successors {
					size += s.Size
				}
				totalPullBytes.Store(size)
				return successors, nil
			},
			PreCopy: func(ctx context.Context, desc v1.Descriptor) error {
				title := desc.Annotations["org.opencontainers.image.title"]
				compressed := strings.HasSuffix(desc.MediaType, "+gzip")
				logger.Log().Info("Downloading layer",
					zap.String("title", title),
					zap.String("mediaType", desc.MediaType),
					zap.Int64("size", desc.Size),
					zap.Bool("compressed", compressed),
					zap.String("digest", desc.Digest.String()),
				)
				return nil
			},
			PostCopy: func(ctx context.Context, desc v1.Descriptor) error {
				done := completed.Add(1)
				total := totalLayers.Load()
				bytesDownloaded.Add(desc.Size)
				if progress != nil && total > 0 {
					pct := done * 100 / total
					progress.Percentage.Store(pct)
				}
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Pulled layer",
					zap.String("title", title),
					zap.Int64("size", desc.Size),
					zap.String("digest", desc.Digest.String()),
					zap.String("progress", fmt.Sprintf("%d/%d", done, total)),
				)
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc v1.Descriptor) error {
				done := completed.Add(1)
				total := totalLayers.Load()
				bytesDownloaded.Add(desc.Size)
				if progress != nil && total > 0 {
					pct := done * 100 / total
					progress.Percentage.Store(pct)
				}
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Layer already exists locally, skipped",
					zap.String("title", title),
					zap.Int64("size", desc.Size),
					zap.String("digest", desc.Digest.String()),
					zap.String("progress", fmt.Sprintf("%d/%d", done, total)),
				)
				return nil
			},
		},
	}

	stopProgress := startProgressTicker("Download", &bytesDownloaded, &totalPullBytes, &completed, &totalLayers)

	// Use a constant destination reference for the local file store so digest references
	// (which contain ':' and other characters) don't become a tag key.
	const dstRef = "root"
	manifestDescriptor, err := oras.Copy(ctx, repoInstance, ref, fs, dstRef, copyOpts)
	stopProgress()
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
	repo, err := c.GetRepo(r)
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
		logger.Log().Info("Packing file", zap.String("path", name), zap.String("artifactType", string(artifactType)))
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
func (c *OciClient) Push(folder string, repo string, tag string, overrides map[string]string, packMeta bool, scrollFile *domain.File) (v1.Descriptor, error) {

	fsFileNames := []string{}

	//check if files exisits (file or folder) and remove from slice if not
	for fileName, artifactType := range domain.ScrollFiles {
		if artifactType != domain.ArtifactTypeScrollData && artifactType != domain.ArtifactTypeScrollFs {
			continue
		}
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
			fileInfo, err := os.Stat(chunkFullPath)
			if err != nil {
				return v1.Descriptor{}, fmt.Errorf("failed to stat data chunk %s: %w", chunk.Name, err)
			}
			// Some registries reject zero-byte blob uploads (sha256:e3b0...).
			// Skip empty data files to keep push resilient.
			if fileInfo.Mode().IsRegular() && fileInfo.Size() == 0 {
				logger.Log().Warn(fmt.Sprintf("data chunk %s is empty, skipping", chunk.Path))
				continue
			}

			logger.Log().Info("Packing data chunk", zap.String("path", chunk.Path), zap.Int64("size", fileInfo.Size()))

			// Name the layer "data/<path>" so it extracts to the correct location on pull
			layerName := filepath.Join("data", chunk.Path)
			// Use a path relative to the file store root (folder), not the full chunkFullPath,
			// because fs.Add resolves relative paths against its working directory.
			chunkStoreRelPath := filepath.Join("data", chunk.Path)
			desc, err := fs.Add(context.Background(), layerName, string(domain.ArtifactTypeScrollData), chunkStoreRelPath)
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

	// annotations.json (if present) is treated as the source of truth for OCI manifest annotations.
	// This ensures a subsequent pull can restore the same annotations that were previously set.
	annotations := map[string]string{}
	annotationsFile := filepath.Join(folder, "annotations.json")
	if b, err := os.ReadFile(annotationsFile); err == nil {
		err := json.Unmarshal(b, &annotations)
		if err != nil {
			return v1.Descriptor{}, fmt.Errorf("failed to unmarshal annotations.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		logger.Log().Info("No annotations.json found, skipping")
	}

	// Apply CLI overrides on top of any local `annotations.json`.
	for k, v := range overrides {
		annotations[k] = v
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

	var totalBytes atomic.Int64
	for _, desc := range descriptorsForRoot {
		totalBytes.Add(desc.Size)
	}
	logger.Log().Info("Starting push to registry",
		zap.String("repo", repo),
		zap.String("tag", tag),
		zap.Int("layers", len(descriptorsForRoot)),
		zap.Int64("totalSize", totalBytes.Load()),
	)

	var completed atomic.Int64
	var bytesUploaded atomic.Int64
	var totalLayers atomic.Int64
	totalLayers.Store(int64(len(descriptorsForRoot)) + 1) // +1 for manifest

	pushCopyOpts := oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			PreCopy: func(ctx context.Context, desc v1.Descriptor) error {
				title := desc.Annotations["org.opencontainers.image.title"]
				compressed := strings.HasSuffix(desc.MediaType, "+gzip")
				logger.Log().Info("Uploading layer",
					zap.String("title", title),
					zap.String("mediaType", desc.MediaType),
					zap.Int64("size", desc.Size),
					zap.Bool("compressed", compressed),
					zap.String("digest", desc.Digest.String()),
				)
				return nil
			},
			PostCopy: func(ctx context.Context, desc v1.Descriptor) error {
				done := completed.Add(1)
				bytesUploaded.Add(desc.Size)
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Pushed layer",
					zap.String("title", title),
					zap.Int64("size", desc.Size),
					zap.String("digest", desc.Digest.String()),
					zap.String("progress", fmt.Sprintf("%d/%d", done, totalLayers.Load())),
				)
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc v1.Descriptor) error {
				done := completed.Add(1)
				bytesUploaded.Add(desc.Size)
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Layer already exists, skipped",
					zap.String("title", title),
					zap.Int64("size", desc.Size),
					zap.String("digest", desc.Digest.String()),
					zap.String("progress", fmt.Sprintf("%d/%d", done, totalLayers.Load())),
				)
				return nil
			},
		},
	}

	stopProgress := startProgressTicker("Upload", &bytesUploaded, &totalBytes, &completed, &totalLayers)
	_, err = oras.Copy(ctx, fs, tag, repoInstance, tag, pushCopyOpts)
	stopProgress()

	if err != nil {
		return v1.Descriptor{}, err
	}

	logger.Log().Info("Push complete",
		zap.String("repo", repo),
		zap.String("tag", tag),
		zap.String("digest", rootManifestDescriptor.Digest.String()),
	)

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

	var metaTotalBytes atomic.Int64
	for _, desc := range manifestDescriptors {
		metaTotalBytes.Add(desc.Size)
	}
	logger.Log().Info("Starting meta push to registry",
		zap.String("repo", repo),
		zap.Int("layers", len(manifestDescriptors)),
		zap.Int64("totalSize", metaTotalBytes.Load()),
	)

	var metaCompleted atomic.Int64
	var metaBytesUploaded atomic.Int64
	var metaTotalLayers atomic.Int64
	metaTotalLayers.Store(int64(len(manifestDescriptors)) + 1)

	metaCopyOpts := oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			PreCopy: func(ctx context.Context, desc v1.Descriptor) error {
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Uploading meta layer",
					zap.String("title", title),
					zap.String("mediaType", desc.MediaType),
					zap.Int64("size", desc.Size),
					zap.String("digest", desc.Digest.String()),
				)
				return nil
			},
			PostCopy: func(ctx context.Context, desc v1.Descriptor) error {
				done := metaCompleted.Add(1)
				metaBytesUploaded.Add(desc.Size)
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Pushed meta layer",
					zap.String("title", title),
					zap.Int64("size", desc.Size),
					zap.String("digest", desc.Digest.String()),
					zap.String("progress", fmt.Sprintf("%d/%d", done, metaTotalLayers.Load())),
				)
				return nil
			},
			OnCopySkipped: func(ctx context.Context, desc v1.Descriptor) error {
				done := metaCompleted.Add(1)
				metaBytesUploaded.Add(desc.Size)
				title := desc.Annotations["org.opencontainers.image.title"]
				logger.Log().Info("Meta layer already exists, skipped",
					zap.String("title", title),
					zap.Int64("size", desc.Size),
					zap.String("digest", desc.Digest.String()),
					zap.String("progress", fmt.Sprintf("%d/%d", done, metaTotalLayers.Load())),
				)
				return nil
			},
		},
	}

	stopMetaProgress := startProgressTicker("Meta upload", &metaBytesUploaded, &metaTotalBytes, &metaCompleted, &metaTotalLayers)
	_, err = oras.Copy(ctx, fs, tag, repoInstance, tag, metaCopyOpts)
	stopMetaProgress()

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
