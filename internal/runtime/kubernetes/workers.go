package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) SpawnPullWorker(ctx context.Context, action ports.RuntimeWorkerAction) error {
	if err := b.config.ValidateForMaterialization(); err != nil {
		logger.Log().Error("Kubernetes pull worker config invalid", zap.String("runtime_id", action.RuntimeID), zap.Error(err))
		return err
	}
	if _, err := os.Stat(action.Artifact); err == nil {
		return fmt.Errorf("kubernetes materialization requires an OCI artifact reference; local path %q is only available with Docker runtime", action.Artifact)
	}
	if action.MountPath == "" {
		action.MountPath = "/scroll"
	}
	namespace, pvc, err := parseRef(action.RootRef)
	if err != nil {
		logger.Log().Error("Kubernetes pull worker root ref invalid", zap.String("runtime_id", action.RuntimeID), zap.String("root_ref", action.RootRef), zap.Error(err))
		return err
	}
	logger.Log().Info("Spawning Kubernetes pull worker",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("mode", string(action.Mode)),
		zap.String("namespace", namespace),
		zap.String("pvc", pvc),
		zap.String("artifact", action.Artifact),
	)
	logger.Log().Debug("Kubernetes pull worker details",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("root_ref", action.RootRef),
		zap.String("mount_path", action.MountPath),
		zap.String("pull_image", b.config.PullImage),
		zap.Bool("registry_plain_http", b.config.RegistryPlainHTTP),
		zap.Bool("has_registry_credentials", len(action.RegistryCredentials) > 0),
	)
	if action.Mode == ports.RuntimeWorkerModeCreate {
		if err := b.ensurePVC(ctx, namespace, pvc); err != nil {
			logger.Log().Error("Failed to ensure runtime PVC for pull worker", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
	}
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, action.Artifact+action.RuntimeID, action.RegistryCredentials)
	if err != nil {
		logger.Log().Error("Failed to create registry config secret for pull worker", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.Error(err))
		return err
	}
	defer cleanupRegistryConfig()
	job := workerPullJobSpec(namespace, jobName("worker-pull", action.RootRef, shortHash(string(action.Mode)+action.Artifact)), pvc, b.config.PullImage, action, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	logger.Log().Debug("Kubernetes pull worker job built", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("job", job.Name))
	if err := b.runHelperJob(ctx, job); err != nil {
		logger.Log().Error("Kubernetes pull worker failed", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("job", job.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes pull worker completed", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("job", job.Name))
	return nil
}

func (b *Backend) BackupRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	if artifact == "" {
		return fmt.Errorf("backup artifact is required")
	}
	if b.config.PullImage == "" {
		return b.config.ValidateForMaterialization()
	}
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot backup Kubernetes runtime for invalid root", zap.String("root", root), zap.String("artifact", artifact), zap.Error(err))
		return err
	}
	logger.Log().Info("Backing up Kubernetes runtime", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact))
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, artifact+root, registryCredentials)
	if err != nil {
		logger.Log().Error("Failed to create registry config secret for Kubernetes backup", zap.String("namespace", namespace), zap.String("artifact", artifact), zap.Error(err))
		return err
	}
	defer cleanupRegistryConfig()
	job := backupJobSpec(namespace, jobName("backup", root, shortHash(artifact)), pvc, b.config.PullImage, artifact, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	if err := b.runHelperJob(ctx, job); err != nil {
		logger.Log().Error("Kubernetes runtime backup failed", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact), zap.String("job", job.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes runtime backup completed", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact), zap.String("job", job.Name))
	return nil
}

func (b *Backend) ensurePVC(ctx context.Context, namespace string, name string) error {
	pvc := pvcSpec(namespace, name, b.config.StorageClass)
	logger.Log().Debug("Ensuring Kubernetes PVC", zap.String("namespace", namespace), zap.String("pvc", name), zap.String("storage_class", b.config.StorageClass))
	_, err := b.client.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		logger.Log().Debug("Kubernetes PVC already exists", zap.String("namespace", namespace), zap.String("pvc", name))
		return nil
	}
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes PVC", zap.String("namespace", namespace), zap.String("pvc", name), zap.Error(err))
		return err
	}
	logger.Log().Info("Created Kubernetes PVC", zap.String("namespace", namespace), zap.String("pvc", name))
	return err
}

func (b *Backend) createRegistryConfigSecret(ctx context.Context, namespace string, seed string, credentials []domain.RegistryCredential) (string, func(), error) {
	if len(credentials) == 0 {
		logger.Log().Debug("No registry credentials supplied; skipping Kubernetes registry config secret", zap.String("namespace", namespace))
		return "", func() {}, nil
	}
	data, err := json.Marshal(struct {
		Registries []domain.RegistryCredential `json:"registries"`
	}{Registries: credentials})
	if err != nil {
		logger.Log().Error("Failed to marshal registry credentials for Kubernetes secret", zap.String("namespace", namespace), zap.Int("registries", len(credentials)), zap.Error(err))
		return "", nil, err
	}
	name := dnsLabel("druid-registry-" + shortHash(fmt.Sprintf("%s-%d", seed, time.Now().UnixNano())))
	logger.Log().Debug("Creating Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Int("registries", len(credentials)))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy: "druid",
				labelComponent: "registry-auth",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{registryConfigSecretKey: data},
	}
	if _, err := b.client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		logger.Log().Error("Failed to create Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Error(err))
		return "", nil, err
	}
	logger.Log().Info("Created Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Int("registries", len(credentials)))
	cleanup := func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.client.CoreV1().Secrets(namespace).Delete(deleteCtx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Warn("Failed to delete Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Error(err))
			return
		}
		logger.Log().Debug("Deleted Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name))
	}
	return name, cleanup, nil
}

func (b *Backend) runHelperJob(ctx context.Context, job *batchv1.Job) error {
	_, err := b.runJobAndLogs(ctx, job)
	if err != nil {
		logger.Log().Error("Kubernetes helper job failed", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
	}
	return err
}

func (b *Backend) runJobAndLogs(ctx context.Context, job *batchv1.Job) ([]byte, error) {
	if b.jobLogRunner != nil {
		logger.Log().Debug("Running Kubernetes job through test log runner", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
		return b.jobLogRunner(ctx, job)
	}
	logger.Log().Info("Starting Kubernetes helper job", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
	logger.Log().Debug("Kubernetes helper job details", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.String("service_account", job.Spec.Template.Spec.ServiceAccountName), zap.Int("containers", len(job.Spec.Template.Spec.Containers)), zap.Int("init_containers", len(job.Spec.Template.Spec.InitContainers)))
	createdJob, err := b.createFreshJob(ctx, job)
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes helper job", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	jobName := createdJob.Name
	podName, err := b.waitForJobPod(ctx, job.Namespace, jobName, string(createdJob.UID))
	if err != nil {
		logger.Log().Error("Failed to find Kubernetes helper job pod", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("uid", string(createdJob.UID)), zap.Error(err))
		return nil, err
	}
	logger.Log().Debug("Kubernetes helper job pod found", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("pod", podName))
	exitCode, waitErr := b.waitForJob(ctx, job.Namespace, jobName)
	logs, logErr := b.podLogs(ctx, job.Namespace, podName)
	if logErr != nil {
		logger.Log().Warn("Failed to collect Kubernetes helper job logs", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("pod", podName), zap.Error(logErr))
	} else {
		logger.Log().Debug("Collected Kubernetes helper job logs", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("pod", podName), zap.Int("bytes", len(logs)))
	}
	if exitCode != nil && *exitCode == 0 {
		b.deleteFinishedJob(context.Background(), job.Namespace, jobName)
	} else if exitCode != nil {
		logger.Log().Warn("Keeping failed Kubernetes helper job for debugging", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	}
	if logErr != nil && waitErr == nil {
		waitErr = logErr
	}
	if waitErr != nil {
		logger.Log().Error("Kubernetes helper job wait failed", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Any("exit_code", exitCode), zap.Error(waitErr))
		return logs, waitErr
	}
	if exitCode != nil && *exitCode != 0 {
		logger.Log().Error("Kubernetes helper job exited non-zero", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
		return logs, fmt.Errorf("job %s exited with code %d", jobName, *exitCode)
	}
	if exitCode != nil {
		logger.Log().Info("Kubernetes helper job completed", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	}
	return logs, nil
}
