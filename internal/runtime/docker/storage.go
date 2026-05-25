package docker

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/mount"
)

const (
	StorageVolume = "volume"
	StorageBind   = "bind"

	volumeRootPrefix = "docker-volume://"
	bindRootPrefix   = "docker-bind://"
)

type RootRef struct {
	Kind   string
	Source string
}

func (c Config) RuntimeRootRef(id string) (string, error) {
	c = c.WithDefaults()
	name := sanitizeVolumePart(id)
	if name == "" {
		return "", fmt.Errorf("runtime id is required")
	}
	switch c.Storage {
	case StorageVolume:
		return volumeRootPrefix + sanitizeVolumePart(c.VolumePrefix+"-"+name+"-data"), nil
	case StorageBind:
		if c.BindRoot == "" {
			return "", fmt.Errorf("docker bind root is required when docker storage is bind")
		}
		if !filepath.IsAbs(c.BindRoot) {
			return "", fmt.Errorf("docker bind root must be absolute: %s", c.BindRoot)
		}
		return bindRootPrefix + filepath.Join(c.BindRoot, name), nil
	default:
		return "", fmt.Errorf("unknown docker storage %q", c.Storage)
	}
}

func ParseRootRef(root string) (RootRef, error) {
	if root == "" {
		return RootRef{}, fmt.Errorf("root ref is required")
	}
	if strings.HasPrefix(root, volumeRootPrefix) {
		name := strings.TrimPrefix(root, volumeRootPrefix)
		if name == "" || strings.Contains(name, "/") {
			return RootRef{}, fmt.Errorf("invalid docker volume root ref %q", root)
		}
		return RootRef{Kind: StorageVolume, Source: name}, nil
	}
	if strings.HasPrefix(root, bindRootPrefix) {
		path := strings.TrimPrefix(root, bindRootPrefix)
		if !filepath.IsAbs(path) {
			return RootRef{}, fmt.Errorf("docker bind root must be absolute: %s", path)
		}
		return RootRef{Kind: StorageBind, Source: filepath.Clean(path)}, nil
	}
	if filepath.IsAbs(root) {
		return RootRef{Kind: StorageBind, Source: filepath.Clean(root)}, nil
	}
	return RootRef{}, fmt.Errorf("unsupported docker root ref %q", root)
}

func DockerMount(root string, target string, readOnly bool, subPath string) (mount.Mount, error) {
	ref, err := ParseRootRef(root)
	if err != nil {
		return mount.Mount{}, err
	}
	if target == "" {
		return mount.Mount{}, fmt.Errorf("mount target is required")
	}
	cleanSubPath, err := cleanRootSubPath(subPath)
	if err != nil {
		return mount.Mount{}, err
	}
	switch ref.Kind {
	case StorageVolume:
		result := mount.Mount{
			Type:     mount.TypeVolume,
			Source:   ref.Source,
			Target:   target,
			ReadOnly: readOnly,
		}
		if cleanSubPath != "" {
			result.VolumeOptions = &mount.VolumeOptions{Subpath: cleanSubPath}
		}
		return result, nil
	case StorageBind:
		source := ref.Source
		if cleanSubPath != "" {
			source = filepath.Join(source, filepath.FromSlash(cleanSubPath))
		}
		return mount.Mount{
			Type:        mount.TypeBind,
			Source:      source,
			Target:      target,
			ReadOnly:    readOnly,
			BindOptions: &mount.BindOptions{CreateMountpoint: true},
		}, nil
	default:
		return mount.Mount{}, fmt.Errorf("unsupported docker root kind %q", ref.Kind)
	}
}

func CleanContainerRelativePath(relativePath string) (string, error) {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(relativePath, "/")))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid data file path %q", relativePath)
	}
	return cleaned, nil
}

func cleanRootSubPath(subPath string) (string, error) {
	if subPath == "" || subPath == "." {
		return "", nil
	}
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(subPath, "/")))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid docker mount subpath %q", subPath)
	}
	return cleaned, nil
}

func sanitizeVolumePart(value string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
	value = re.ReplaceAllString(value, "-")
	return strings.Trim(value, "-_.")
}
