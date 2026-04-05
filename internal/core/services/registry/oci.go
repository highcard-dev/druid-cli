package registry

import (
	"context"
	"encoding/json"
	"errors"
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
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
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

// checkPushAccess triggers the OCI auth challenge-response flow against
// the repository so that credential / network problems surface before
// any expensive local packing work.
func (c *OciClient) checkPushAccess(ctx context.Context, repo *remote.Repository) error {
	err := repo.Tags(ctx, "", func(tags []string) error { return nil })
	if err == nil || isRepoNotFound(err) {
		return nil
	}
	return fmt.Errorf("registry auth check failed: %w", err)
}

func isRepoNotFound(err error) bool {
	var errResp *errcode.ErrorResponse
	if errors.As(err, &errResp) && errResp.StatusCode == http.StatusNotFound {
		return true
	}
	return errors.Is(err, errdef.ErrNotFound)
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

	stopProgress := utils.StartTransferProgressTicker("Download", &bytesDownloaded, &totalPullBytes, &completed, &totalLayers)

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

func (c *OciClient) packFolders(fs *file.Store, dirs []string, artifactType domain.ArtifactType, path string) ([]v1.Descriptor, error) {
	ctx := context.Background()

	fileDescriptors := make([]v1.Descriptor, 0, len(dirs))
	for _, name := range dirs {
		fullPath := filepath.Join(path, name)
		logger.Log().Info("Packing layer",
			zap.String("path", fullPath),
			zap.String("artifactType", string(artifactType)),
		)

		fileDescriptor, err := fs.Add(ctx, fullPath, string(artifactType), fullPath)
		if err != nil {
			return []v1.Descriptor{}, err
		}
		fileDescriptors = append(fileDescriptors, fileDescriptor)
		logger.Log().Info("Packed layer",
			zap.String("path", fullPath),
			zap.String("digest", fileDescriptor.Digest.String()),
		)
	}

	return fileDescriptors, nil
}

func (c *OciClient) createMetaDescriptors(fs *file.Store, folder string, fsPath string) ([]v1.Descriptor, error) {
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

	return c.packFolders(fs, fsFileNames, domain.ArtifactTypeScrollMeta, fsPath)
}

// pushBlobWithRetry pushes a single blob from src to dst, retrying up to
// maxRetries times on transient failures. bytesUploaded is updated in
// real-time as data is streamed. Returns (true, nil) when the blob already
// exists in the destination and was skipped.
func pushBlobWithRetry(ctx context.Context, src *file.Store, dst *remote.Repository, desc v1.Descriptor, maxRetries int, bytesUploaded *atomic.Int64) (bool, error) {
	baseline := bytesUploaded.Load()
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			bytesUploaded.Store(baseline)
		}

		exists, err := dst.Exists(ctx, desc)
		if err == nil && exists {
			bytesUploaded.Store(baseline + desc.Size)
			return true, nil
		}

		rc, err := src.Fetch(ctx, desc)
		if err != nil {
			return false, fmt.Errorf("failed to read local blob: %w", err)
		}

		pr := utils.NewCountingReader(rc, bytesUploaded)
		err = dst.Push(ctx, desc, pr)
		rc.Close()
		if err == nil {
			return false, nil
		}

		if errors.Is(err, errdef.ErrAlreadyExists) {
			bytesUploaded.Store(baseline + desc.Size)
			return true, nil
		}

		lastErr = err
		if attempt < maxRetries {
			backoff := time.Duration(attempt+1) * 2 * time.Second
			logger.Log().Warn("Layer push failed, retrying",
				zap.String("title", desc.Annotations["org.opencontainers.image.title"]),
				zap.String("digest", desc.Digest.String()),
				zap.Int("attempt", attempt+1),
				zap.Int("maxRetries", maxRetries),
				zap.Duration("backoff", backoff),
				zap.Error(err),
			)
			time.Sleep(backoff)
		}
	}
	return false, fmt.Errorf("failed after %d attempts: %w", maxRetries+1, lastErr)
}

// copyToRegistry handles progress tracking, per-layer retries, and the
// final manifest push via oras.Copy. It is shared by Push and PushMeta.
func copyToRegistry(ctx context.Context, fs *file.Store, repo *remote.Repository, tag string, layers []v1.Descriptor, maxRetries int) error {
	var totalBytes atomic.Int64
	for _, desc := range layers {
		totalBytes.Add(desc.Size)
	}

	logger.Log().Info("Starting push to registry",
		zap.String("repo", repo.Reference.Repository),
		zap.String("tag", tag),
		zap.Int("layers", len(layers)),
		zap.Int64("totalSize", totalBytes.Load()),
	)

	var completed atomic.Int64
	var bytesUploaded atomic.Int64
	var totalLayers atomic.Int64
	totalLayers.Store(int64(len(layers)))

	stopProgress := utils.StartTransferProgressTicker("Upload", &bytesUploaded, &totalBytes, &completed, &totalLayers)

	for _, desc := range layers {
		title := desc.Annotations["org.opencontainers.image.title"]
		logger.Log().Info("Uploading layer",
			zap.String("title", title),
			zap.String("mediaType", desc.MediaType),
			zap.Int64("size", desc.Size),
			zap.String("digest", desc.Digest.String()),
		)

		skipped, err := pushBlobWithRetry(ctx, fs, repo, desc, maxRetries, &bytesUploaded)
		if err != nil {
			stopProgress()
			return fmt.Errorf("failed to push layer %s: %w", title, err)
		}

		done := completed.Add(1)
		total := totalLayers.Load()

		if skipped {
			logger.Log().Info("Layer already exists, skipped",
				zap.String("title", title),
				zap.Int64("size", desc.Size),
				zap.String("digest", desc.Digest.String()),
				zap.String("progress", fmt.Sprintf("%d/%d", done, total)),
			)
		} else {
			logger.Log().Info("Pushed layer",
				zap.String("title", title),
				zap.Int64("size", desc.Size),
				zap.String("digest", desc.Digest.String()),
				zap.String("progress", fmt.Sprintf("%d/%d", done, total)),
			)
		}
	}

	stopProgress()

	logger.Log().Info("Pushing manifest",
		zap.String("repo", repo.Reference.Repository),
		zap.String("tag", tag),
	)
	_, err := oras.Copy(ctx, fs, tag, repo, tag, oras.CopyOptions{})
	if err != nil {
		return err
	}

	logger.Log().Info("Push complete",
		zap.String("repo", repo.Reference.Repository),
		zap.String("tag", tag),
	)

	return nil
}

func (c *OciClient) Push(folder string, repo string, tag string, overrides map[string]string, packMeta bool, scrollFile *domain.File) (v1.Descriptor, error) {
	ctx := context.Background()

	// Authenticate before doing any expensive local work.
	repoInstance, err := c.GetRepo(repo)
	if err != nil {
		return v1.Descriptor{}, err
	}
	if err := c.checkPushAccess(ctx, repoInstance); err != nil {
		return v1.Descriptor{}, err
	}

	// Discover pushable files.
	fsFileNames := []string{}
	for fileName, artifactType := range domain.ScrollFiles {
		if artifactType != domain.ArtifactTypeScrollData && artifactType != domain.ArtifactTypeScrollFs {
			continue
		}
		// Skip the data directory, it is auto-chunked and packed individually further below.
		if fileName == domain.ScrollDataDir {
			continue
		}
		exists, _ := utils.FileExists(filepath.Join(folder, fileName))
		if exists {
			fsFileNames = append(fsFileNames, fileName)
		} else {
			logger.Log().Warn("Path does not exist, skipping", zap.String("path", fileName))
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

	// Pack scroll FS layers.
	descriptorsForRoot, err := c.packFolders(fs, fsFileNames, domain.ArtifactTypeScrollFs, "")
	if err != nil {
		return v1.Descriptor{}, err
	}

	// Pack meta descriptors.
	if packMeta {
		metaDescriptors, err := c.createMetaDescriptors(fs, folder, ".meta")
		if err != nil {
			return v1.Descriptor{}, err
		}
		descriptorsForRoot = append(descriptorsForRoot, metaDescriptors...)
	}

	// Pack data directory chunks.
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
				logger.Log().Warn("Data chunk does not exist, skipping", zap.String("path", chunk.Path))
				continue
			}
			fileInfo, err := os.Stat(chunkFullPath)
			if err != nil {
				return v1.Descriptor{}, fmt.Errorf("failed to stat data chunk %s: %w", chunk.Name, err)
			}
			// Some registries reject zero-byte blob uploads (sha256:e3b0...).
			if fileInfo.Mode().IsRegular() && fileInfo.Size() == 0 {
				logger.Log().Warn("Data chunk is empty, skipping", zap.String("path", chunk.Path))
				continue
			}

			layerPath := filepath.Join("data", chunk.Path)
			logger.Log().Info("Packing layer",
				zap.String("path", layerPath),
				zap.String("artifactType", string(domain.ArtifactTypeScrollData)),
				zap.Int64("size", fileInfo.Size()),
			)
			desc, err := fs.Add(ctx, layerPath, string(domain.ArtifactTypeScrollData), layerPath)
			if err != nil {
				return v1.Descriptor{}, fmt.Errorf("failed to pack data chunk %s: %w", chunk.Name, err)
			}
			logger.Log().Info("Packed layer",
				zap.String("path", layerPath),
				zap.String("digest", desc.Digest.String()),
			)
			descriptorsForRoot = append(descriptorsForRoot, desc)
		}
	} else {
		logger.Log().Info("No data directory found, skipping data chunk packing")
	}

	// Build manifest with annotations.
	annotations := map[string]string{}
	annotationsFile := filepath.Join(folder, "annotations.json")
	if b, err := os.ReadFile(annotationsFile); err == nil {
		if err := json.Unmarshal(b, &annotations); err != nil {
			return v1.Descriptor{}, fmt.Errorf("failed to unmarshal annotations.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		logger.Log().Info("No annotations.json found, skipping")
	}
	for k, v := range overrides {
		annotations[k] = v
	}

	rootManifestDescriptor, err := oras.PackManifest(ctx, fs, oras.PackManifestVersion1_1, string(domain.ArtifactTypeScrollRoot), oras.PackManifestOptions{
		Layers:              descriptorsForRoot,
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return v1.Descriptor{}, err
	}

	logger.Log().Info("Manifest packed",
		zap.String("digest", rootManifestDescriptor.Digest.String()),
	)

	if err = fs.Tag(ctx, rootManifestDescriptor, tag); err != nil {
		return v1.Descriptor{}, err
	}

	// Upload everything.
	if err := copyToRegistry(ctx, fs, repoInstance, tag, descriptorsForRoot, 3); err != nil {
		return v1.Descriptor{}, err
	}

	return rootManifestDescriptor, nil
}

func (c *OciClient) PushMeta(scrollDir string, repo string) (v1.Descriptor, error) {
	ctx := context.Background()

	// Authenticate before doing any expensive local work.
	repoInstance, err := c.GetRepo(repo)
	if err != nil {
		return v1.Descriptor{}, err
	}
	if err := c.checkPushAccess(ctx, repoInstance); err != nil {
		return v1.Descriptor{}, err
	}

	fs, err := file.New(scrollDir)
	if err != nil {
		return v1.Descriptor{}, err
	}
	defer fs.Close()

	manifestDescriptors, err := c.createMetaDescriptors(fs, scrollDir, ".meta")
	if err != nil {
		return v1.Descriptor{}, err
	}

	rootManifestDescriptor, err := oras.PackManifest(ctx, fs, oras.PackManifestVersion1_1, string(domain.ArtifactTypeScrollRoot), oras.PackManifestOptions{
		Layers: manifestDescriptors,
	})
	if err != nil {
		return v1.Descriptor{}, err
	}

	logger.Log().Info("Manifest packed",
		zap.String("digest", rootManifestDescriptor.Digest.String()),
	)

	tag := "meta"
	if err = fs.Tag(ctx, rootManifestDescriptor, tag); err != nil {
		return v1.Descriptor{}, err
	}

	if err := copyToRegistry(ctx, fs, repoInstance, tag, manifestDescriptors, 3); err != nil {
		return v1.Descriptor{}, err
	}

	return rootManifestDescriptor, nil
}
