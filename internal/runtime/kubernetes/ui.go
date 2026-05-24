package kubernetes

import (
	"context"
	"fmt"
	"path"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

func (b *Backend) PublishUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (ports.RuntimeUIPackageResult, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	namespace, pvc, err := parseRef(action.RootRef)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	keyPrefix := strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/"+namespace+"/"+action.RuntimeID+"/"+string(action.Scope), "/")
	job := uiPublishJobSpec(namespace, jobName("ui-publish", action.RootRef, string(action.Scope)), pvc, b.config.PullImage, action, b.config.RegistrySecret, b.config.UIS3Secret, b.config.UIS3Bucket, b.config.UIS3Region, b.config.UIS3Endpoint, keyPrefix)
	logs, err := b.runJobAndLogs(ctx, job)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	hash := strings.TrimSpace(string(logs))
	if idx := strings.LastIndex(hash, "\n"); idx >= 0 {
		hash = strings.TrimSpace(hash[idx+1:])
	}
	if hash == "" {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("ui publish job did not return a content hash")
	}
	key := path.Join(keyPrefix, hash, "app.wasm")
	return ports.RuntimeUIPackageResult{
		URL:    strings.TrimRight(b.config.UIS3PublicBaseURL, "/") + "/" + key,
		Path:   action.SourcePath,
		SHA256: hash,
	}, nil
}

func boolPtr(value bool) *bool {
	return &value
}

func uiPublishJobSpec(namespace string, jobName string, pvc string, image string, action ports.RuntimeUIPackageAction, imagePullSecret string, s3Secret string, bucket string, region string, endpoint string, keyPrefix string) *batchv1.Job {
	command := []string{
		"druid", "ui", "publish-s3",
		"--root", "/scroll",
		"--source", action.SourcePath,
		"--bucket", bucket,
		"--region", region,
		"--key-prefix", keyPrefix,
	}
	if endpoint != "" {
		command = append(command, "--endpoint", endpoint)
	}
	job := helperJobSpec(namespace, jobName, pvc, image, command, imagePullSecret, map[string]string{
		labelComponent: "ui-publish",
		labelRuntimeID: dnsLabel(action.RuntimeID),
	})
	container := &job.Spec.Template.Spec.Containers[0]
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: s3Secret},
				Key:                  "AWS_ACCESS_KEY_ID",
			}},
		},
		corev1.EnvVar{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: s3Secret},
				Key:                  "AWS_SECRET_ACCESS_KEY",
			}},
		},
		corev1.EnvVar{
			Name: "AWS_SESSION_TOKEN",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: s3Secret},
				Key:                  "AWS_SESSION_TOKEN",
				Optional:             boolPtr(true),
			}},
		},
	)
	return job
}
