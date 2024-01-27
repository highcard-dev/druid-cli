package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/utils"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/zap"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type ArtifactType string

const (
	ArtifactTypeScrollRoot ArtifactType = "application/vnd.highcard.druid.scroll.config.v1+json"
	ArtifactTypeScrollFs   ArtifactType = "application/vnd.highcard.druid.scroll-fs.config.v1+json"
	ArtifactTypeScrollMeta ArtifactType = "application/vnd.highcard.druid.scroll-meta.config.v1+json"
)

type OciClient struct {
	host     string
	username string
	password string
}

type AnnotationInfo struct {
	MinRam  string
	MinDisk string
	MinCpu  string
	Image   string
	Ports   map[string]string
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

	repo, tag := utils.SplitArtifact(artifact)

	if tag == "" {
		return fmt.Errorf("tag must be set")
	}

	// 1. Connect to a remote repository
	ctx := context.Background()

	repoInstance, err := c.GetRepo(repo)

	if err != nil {
		return err
	}

	fs, err := file.New(filepath.Join(dir))

	if err != nil {
		return err
	}

	manifestDescriptor, err := oras.Copy(ctx, repoInstance, tag, fs, tag, oras.DefaultCopyOptions)
	if err != nil {
		return err
	}
	logger.Log().Info("Manifest pulled", zap.String("digest", manifestDescriptor.Digest.String()), zap.String("mediaType", manifestDescriptor.MediaType))

	jsonData, err := json.Marshal(&manifestDescriptor)

	if err != nil {
		return fmt.Errorf("failed to marshal manifest descriptor: %w", err)
	}

	fileName := filepath.Join(dir, "manifest.json")
	err = ioutil.WriteFile(fileName, jsonData, 0644)

	if err != nil {
		return fmt.Errorf("failed to write manifest descriptor: %w", err)
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

func (c *OciClient) PackFolders(fs *file.Store, dirs []string, artifactType ArtifactType, path string) (v1.Descriptor, error) {

	ctx := context.Background()

	artifactTypeString := string(artifactType)

	fileDescriptors := make([]v1.Descriptor, 0, len(dirs))
	for _, name := range dirs {

		fullPath := filepath.Join(path, name)

		fileDescriptor, err := fs.Add(ctx, name, string(artifactType), fullPath)
		if err != nil {
			return v1.Descriptor{}, err
		}
		fileDescriptors = append(fileDescriptors, fileDescriptor)
		logger.Log().Info(fmt.Sprintf("file descriptor for %s: %v\n", name, fileDescriptor.Digest))
	}

	artifactDescriptor, err := oras.Pack(ctx, fs, artifactTypeString, fileDescriptors, oras.PackOptions{
		PackImageManifest:   true,
		ManifestAnnotations: map[string]string{},
	})

	return artifactDescriptor, err
}

// the root has to leaves, one is the real scroll (fs) and the other is meta information about the scroll
func (c *OciClient) Push(folder string, repo string, tag string, annotationInfo AnnotationInfo, packMeta bool) (v1.Descriptor, error) {

	availableFileNames := []string{"init-files", "init-files-template", "scroll-switch", "update", "scroll.yaml"}
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

	scrollFsManifestDescriptor, err := c.PackFolders(fs, fsFileNames, ArtifactTypeScrollFs, "")

	if err != nil {
		return v1.Descriptor{}, err
	}
	descriptorsForRoot := []v1.Descriptor{scrollFsManifestDescriptor}

	if packMeta {

		scrollMetaManifestDescriptor, err := c.CreateMetaDescriptors(fs, folder, ".meta")
		if err != nil {
			return v1.Descriptor{}, err
		}
		descriptorsForRoot = append(descriptorsForRoot, scrollMetaManifestDescriptor)
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

	for name, port := range annotationInfo.Ports {
		annotations[fmt.Sprintf("gg.druid.scroll.port.%s", name)] = port
	}

	//Pack everything together
	rootManifestDescriptor, err := oras.Pack(ctx, fs, string(ArtifactTypeScrollRoot), descriptorsForRoot, oras.PackOptions{
		ManifestAnnotations: annotations,
		PackImageManifest:   true,
	})

	if err != nil {
		return v1.Descriptor{}, err
	}

	logger.Log().Info(fmt.Sprintf("root manifest descriptor: %v\n", rootManifestDescriptor.Digest))

	if err = fs.Tag(ctx, rootManifestDescriptor, tag); err != nil {
		return v1.Descriptor{}, err
	}
	_, err = oras.Copy(ctx, fs, tag, repoInstance, tag, oras.DefaultCopyOptions)

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

	rootManifestDescriptor, err := c.CreateMetaDescriptors(fs, folder, "")

	ctx := context.Background()

	if err != nil {
		return v1.Descriptor{}, err
	}

	logger.Log().Info(fmt.Sprintf("Meta manifest descriptor: %v\n", rootManifestDescriptor.Digest))

	tag := "meta"

	if err = fs.Tag(ctx, rootManifestDescriptor, tag); err != nil {
		return v1.Descriptor{}, err
	}
	_, err = oras.Copy(ctx, fs, tag, repoInstance, tag, oras.DefaultCopyOptions)

	return rootManifestDescriptor, err
}

func (c *OciClient) CreateMetaDescriptors(fs *file.Store, folder string, fsPath string) (v1.Descriptor, error) {

	metaPath := filepath.Join(folder, fsPath)
	exists, _ := utils.FileExists(metaPath)
	if !exists {
		return v1.Descriptor{}, fmt.Errorf("meta file not found")
	}
	fsFileNames := []string{}
	subitems, _ := ioutil.ReadDir(metaPath)
	for _, subitem := range subitems {
		fsFileNames = append(fsFileNames, subitem.Name())
	}

	return c.PackFolders(fs, fsFileNames, ArtifactTypeScrollMeta, fsPath)
}
