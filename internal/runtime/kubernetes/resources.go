package kubernetes

import (
	"path/filepath"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/highcard-dev/daemon/internal/core/domain"
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

func pullJobSpec(namespace string, jobName string, pvc string, image string, artifact string, registrySecret string, registryPlainHTTP bool) *batchv1.Job {
	command := []string{"druid-client", "pull", artifact, "/scroll"}
	job := helperJobSpec(namespace, jobName, pvc, image, command, registrySecret, map[string]string{
		labelComponent: "materializer",
	})
	if registryPlainHTTP {
		job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "DRUID_REGISTRY_PLAIN_HTTP", Value: "true"})
	}
	return job
}

func readScrollJobSpec(namespace string, jobName string, pvc string, helperImage string) *batchv1.Job {
	return helperJobSpec(namespace, jobName, pvc, helperImage, []string{"cat", "/scroll/scroll.yaml"}, "", map[string]string{
		labelComponent: "read-scroll",
	})
}

func copyPVCJobSpec(namespace string, jobName string, sourcePVC string, targetPVC string, helperImage string) *batchv1.Job {
	labels := map[string]string{
		labelManagedBy: "druid",
		labelComponent: "copy-scroll",
	}
	backoff := int32(1)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "copy",
						Image:   helperImage,
						Command: []string{"sh", "-c", "cp -a /stage/. /final/"},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "stage", MountPath: "/stage"},
							{Name: "final", MountPath: "/final"},
						},
					}},
					Volumes: []corev1.Volume{
						pvcVolume("stage", sourcePVC),
						pvcVolume("final", targetPVC),
					},
				},
			},
		},
	}
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

func procedureJobSpec(namespace string, dataRoot string, procedureName string, procedure *domain.Procedure, registrySecret string) (*batchv1.Job, error) {
	_, pvc, err := parseRef(dataRoot)
	if err != nil {
		return nil, err
	}
	labels := baseLabels(pvc)
	labels[labelProcedure] = dnsLabel(procedureName)
	labels[labelCommand] = dnsLabel(procedureName)
	backoff := int32(0)
	container := corev1.Container{
		Name:            "main",
		Image:           procedure.Image,
		Command:         procedure.Command,
		WorkingDir:      procedure.WorkingDir,
		TTY:             procedure.TTY,
		Stdin:           procedure.TTY,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars(procedure.Env),
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
			Name:      jobName("proc", dataRoot, procedureName),
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

func procedureStatefulSetSpec(namespace string, dataRoot string, procedureName string, procedure *domain.Procedure, registrySecret string) (*appsv1.StatefulSet, error) {
	_, pvc, err := parseRef(dataRoot)
	if err != nil {
		return nil, err
	}
	labels := baseLabels(pvc)
	labels[labelProcedure] = dnsLabel(procedureName)
	labels[labelCommand] = dnsLabel(procedureName)
	replicas := int32(1)
	container := corev1.Container{
		Name:            "main",
		Image:           procedure.Image,
		Command:         procedure.Command,
		WorkingDir:      procedure.WorkingDir,
		TTY:             procedure.TTY,
		Stdin:           procedure.TTY,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars(procedure.Env),
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
			Name:      statefulSetName(dataRoot, procedureName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: statefulSetName(dataRoot, procedureName),
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}, nil
}

func serviceSpec(namespace string, dataRoot string, procedureName string, portName string, port domain.Port) (*corev1.Service, error) {
	_, pvc, err := parseRef(dataRoot)
	if err != nil {
		return nil, err
	}
	labels := baseLabels(pvc)
	labels[labelProcedure] = dnsLabel(procedureName)
	labels[labelPortName] = dnsLabel(portName)
	selector := baseLabels(pvc)
	selector[labelProcedure] = dnsLabel(procedureName)
	protocol := corev1.ProtocolTCP
	if normalizeProtocol(port.Protocol) == "udp" {
		protocol = corev1.ProtocolUDP
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName(dataRoot, procedureName, portName),
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
