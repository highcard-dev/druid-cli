package kubernetes

import (
	"fmt"
	"path/filepath"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func pvcSpec(namespace string, name string, storageClass string) *corev1.PersistentVolumeClaim {
	quantity := resource.MustParse("1Gi")
	spec := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceStorage: quantity},
		},
	}
	if storageClass != "" {
		spec.StorageClassName = &storageClass
	}
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    baseLabels(name),
		},
		Spec: spec,
	}
}

const (
	registryConfigEnvName   = "DRUID_RUNTIME_REGISTRY_CONFIG_JSON"
	registryConfigSecretKey = "config.json"
	registryConfigScript    = `printf '%s' "$DRUID_RUNTIME_REGISTRY_CONFIG_JSON" > /tmp/druid-registry.json && exec druid --config /tmp/druid-registry.json "$@"`
	workerPullRootEnvName   = "DRUID_WORKER_ROOT"
	workerPullScript        = `set -eu
if [ -n "${DRUID_RUNTIME_REGISTRY_CONFIG_JSON:-}" ]; then
  printf '%s' "$DRUID_RUNTIME_REGISTRY_CONFIG_JSON" > /tmp/druid-registry.json
  druid --config /tmp/druid-registry.json "$@"
else
  druid "$@"
fi
chown -R 1000:1000 "$DRUID_WORKER_ROOT"`
)

func workerPullJobSpec(namespace string, jobName string, pvc string, image string, action ports.RuntimeWorkerAction, imagePullSecret string, registryConfigSecret string, registryPlainHTTP bool) *batchv1.Job {
	command := []string{
		"sh", "-c", workerPullScript, "druid-worker-pull",
		"worker", "pull",
		"--artifact", action.Artifact,
		"--runtime-id", action.RuntimeID,
		"--mode", string(action.Mode),
		"--root", action.MountPath,
		"--callback-url", action.CallbackURL,
	}
	job := helperJobSpec(namespace, jobName, pvc, image, command, imagePullSecret, map[string]string{
		labelComponent: "worker-pull",
	})
	container := &job.Spec.Template.Spec.Containers[0]
	runAsRoot := int64(0)
	runAsNonRoot := false
	container.SecurityContext = &corev1.SecurityContext{
		RunAsUser:    &runAsRoot,
		RunAsGroup:   &runAsRoot,
		RunAsNonRoot: &runAsNonRoot,
	}
	container.Env = append(container.Env,
		corev1.EnvVar{Name: "DRUID_WORKER_TOKEN", Value: action.CallbackToken},
		corev1.EnvVar{Name: workerPullRootEnvName, Value: action.MountPath},
	)
	if registryConfigSecret != "" {
		container.Env = append(container.Env, corev1.EnvVar{
			Name: registryConfigEnvName,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: registryConfigSecret},
				Key:                  registryConfigSecretKey,
			}},
		})
	}
	if registryPlainHTTP {
		container.Env = append(container.Env, corev1.EnvVar{Name: "DRUID_REGISTRY_PLAIN_HTTP", Value: "true"})
	}
	return job
}

func backupJobSpec(namespace string, jobName string, pvc string, image string, artifact string, imagePullSecret string, registryConfigSecret string, registryPlainHTTP bool) *batchv1.Job {
	command := []string{"druid", "push", artifact, "/scroll"}
	if registryConfigSecret != "" {
		command = []string{"sh", "-c", registryConfigScript, "sh", "push", artifact, "/scroll"}
	}
	job := helperJobSpec(namespace, jobName, pvc, image, command, imagePullSecret, map[string]string{
		labelComponent: "backup",
	})
	if registryConfigSecret != "" {
		job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
			Name: registryConfigEnvName,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: registryConfigSecret},
				Key:                  registryConfigSecretKey,
			}},
		})
	}
	if registryPlainHTTP {
		job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "DRUID_REGISTRY_PLAIN_HTTP", Value: "true"})
	}
	return job
}

func helperJobSpec(namespace string, jobName string, pvc string, image string, command []string, registrySecret string, labels map[string]string) *batchv1.Job {
	allLabels := map[string]string{
		labelManagedBy: "druid",
	}
	for key, value := range labels {
		allLabels[key] = value
	}
	backoff := int32(1)
	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Containers: []corev1.Container{{
			Name:         "main",
			Image:        image,
			Command:      command,
			VolumeMounts: []corev1.VolumeMount{{Name: "scroll", MountPath: "/scroll"}},
		}},
		Volumes: []corev1.Volume{pvcVolume("scroll", pvc)},
	}
	if registrySecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: registrySecret}}
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: namespace, Labels: allLabels},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: allLabels},
				Spec:       podSpec,
			},
		},
	}
}

func procedureJobSpec(namespace string, root string, commandName string, procedureName string, resourceName string, attempt int, procedure *domain.Procedure, env map[string]string, registrySecret string) (*batchv1.Job, error) {
	_, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	labels := baseLabels(pvc)
	labels[labelRuntimeID] = dnsLabel(runtimeID(root))
	labels[labelProcedure] = dnsLabel(procedureName)
	labels[labelCommand] = dnsLabel(commandName)
	labels[labelAttempt] = fmt.Sprintf("%d", attempt)
	if len(procedure.ExpectedPorts) == 1 {
		labels[labelPortName] = dnsLabel(procedure.ExpectedPorts[0].Name)
	}
	backoff := int32(0)
	container := corev1.Container{
		Name:            "main",
		Image:           procedure.Image,
		Command:         procedure.Command,
		WorkingDir:      procedure.WorkingDir,
		TTY:             procedure.TTY,
		Stdin:           true,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars(env),
		VolumeMounts:    volumeMounts(procedure.Mounts),
	}
	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Containers:    []corev1.Container{container},
		Volumes:       []corev1.Volume{pvcVolume("data", pvc)},
	}
	if registrySecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: registrySecret}}
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}, nil
}

func procedureStatefulSetSpec(namespace string, root string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, env map[string]string, registrySecret string) (*appsv1.StatefulSet, error) {
	_, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	labels := baseLabels(pvc)
	labels[labelRuntimeID] = dnsLabel(runtimeID(root))
	labels[labelProcedure] = dnsLabel(procedureName)
	labels[labelCommand] = dnsLabel(commandName)
	if len(procedure.ExpectedPorts) == 1 {
		labels[labelPortName] = dnsLabel(procedure.ExpectedPorts[0].Name)
	}
	replicas := int32(1)
	container := corev1.Container{
		Name:            "main",
		Image:           procedure.Image,
		Command:         procedure.Command,
		WorkingDir:      procedure.WorkingDir,
		TTY:             procedure.TTY,
		Stdin:           true,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars(env),
		VolumeMounts:    volumeMounts(procedure.Mounts),
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
		Volumes:    []corev1.Volume{pvcVolume("data", pvc)},
	}
	if registrySecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: registrySecret}}
	}
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: resourceName,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}, nil
}

func devStatefulSetSpec(namespace string, root string, pvc string, image string, action ports.RuntimeDevAction, registrySecret string) *appsv1.StatefulSet {
	labels := baseLabels(pvc)
	labels[labelProcedure] = "dev"
	replicas := int32(1)
	args := []string{"dev", "--root", action.MountPath, "--listen", action.Listen, "--runtime-id", action.RuntimeID, "--daemon-url", action.DaemonURL}
	if action.DaemonToken != "" {
		args = append(args, "--daemon-token", action.DaemonToken)
	}
	if action.OwnerID != "" {
		args = append(args, "--owner-id", action.OwnerID)
	}
	if action.AuthJWKSURL != "" {
		args = append(args, "--auth-jwks-url", action.AuthJWKSURL)
	}
	if action.RuntimeJWKSURL != "" {
		args = append(args, "--runtime-jwks-url", action.RuntimeJWKSURL)
	}
	for _, watchPath := range action.WatchPaths {
		args = append(args, "--watch", watchPath)
	}
	for _, command := range action.HotReloadCommands {
		args = append(args, "--command", command)
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:            "main",
			Image:           image,
			Command:         []string{"druid"},
			Args:            args,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Ports:           []corev1.ContainerPort{{Name: "webdav", ContainerPort: 8084}},
			VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: action.MountPath}},
		}},
		Volumes: []corev1.Volume{pvcVolume("data", pvc)},
	}
	if registrySecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: registrySecret}}
	}
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: devStatefulSetName(root), Namespace: namespace, Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: devStatefulSetName(root),
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template:    corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: podSpec},
		},
	}
}

func devServiceSpec(namespace string, root string, pvc string) *corev1.Service {
	labels := baseLabels(pvc)
	labels[labelProcedure] = "dev"
	labels[labelPortName] = "webdav"
	selector := baseLabels(pvc)
	selector[labelProcedure] = "dev"
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName(root, "dev", "webdav"), Namespace: namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
			Ports: []corev1.ServicePort{{
				Name:       "webdav",
				Protocol:   corev1.ProtocolTCP,
				Port:       8084,
				TargetPort: intstr.FromInt(8084),
			}},
		},
	}
}

func serviceSpec(namespace string, root string, serviceProcedure string, selector map[string]string, portName string, port domain.Port) (*corev1.Service, error) {
	_, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	labels := baseLabels(pvc)
	labels[labelProcedure] = dnsLabel(serviceProcedure)
	labels[labelPortName] = dnsLabel(portName)
	protocol := corev1.ProtocolTCP
	if normalizeProtocol(port.Protocol) == "udp" {
		protocol = corev1.ProtocolUDP
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName(root, serviceProcedure, portName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
			Ports: []corev1.ServicePort{{
				Name:       dnsLabel(portName),
				Protocol:   protocol,
				Port:       int32(port.Port),
				TargetPort: intstr.FromInt(port.Port),
			}},
		},
	}, nil
}

func pvcVolume(name string, pvc string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc},
		},
	}
}

func volumeMounts(mounts []domain.Mount) []corev1.VolumeMount {
	result := make([]corev1.VolumeMount, 0, len(mounts))
	for idx, mount := range mounts {
		result = append(result, corev1.VolumeMount{
			Name:      "data",
			MountPath: mount.Path,
			SubPath:   filepath.ToSlash(mountSubPath(mount.SubPath)),
			ReadOnly:  mount.ReadOnly,
		})
		_ = idx
	}
	return result
}

func envVars(values map[string]string) []corev1.EnvVar {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]corev1.EnvVar, 0, len(keys))
	for _, key := range keys {
		result = append(result, corev1.EnvVar{Name: key, Value: values[key]})
	}
	return result
}
