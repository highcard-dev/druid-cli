package kubernetes

import (
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"testing"
)

func TestCommandIdentityMountedInJobsAndPersistentProcedures(t *testing.T) {
	identity := &domain.RuntimeCommandIdentity{ExecutionID: "execution", DeploymentID: "deployment", Enabled: true, Permissions: []string{"commands:list", "logs:read"}}
	procedure := &domain.Procedure{Image: "busybox", RuntimeIdentity: identity}
	job, err := procedureJobSpec("runtime", ref("runtime", "data"), "automation", "automation.0", "job", 1, procedure, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if job.Labels[workloadidentity.ExecutionLabel] != "execution" || job.Spec.Template.Labels[workloadidentity.ExecutionLabel] != "execution" {
		t.Fatal("execution label missing")
	}
	pod := job.Spec.Template.Spec
	if job.Spec.Template.Annotations[commandDeploymentAnnotation] != "deployment" || job.Spec.Template.Annotations[commandPermissionsAnnotation] != "commands:list logs:read" {
		t.Fatal("Pod authorization grants missing")
	}
	if len(pod.Volumes) != 2 || pod.Volumes[1].Projected == nil || pod.ServiceAccountName != commandServiceAccount || pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("bound ServiceAccount token not mounted")
	}
	projection := pod.Volumes[1].Projected.Sources[0].ServiceAccountToken
	if projection.Audience != commandTokenAudience || *projection.ExpirationSeconds != 3600 || projection.Path != "token" {
		t.Fatal("incorrect command token projection")
	}
	if mounts := pod.Containers[0].VolumeMounts; len(mounts) != 1 || mounts[0].MountPath != "/run/secrets/druid" || !mounts[0].ReadOnly {
		t.Fatal("credential mount is not protected")
	}
	set, err := persistentStatefulSetSpec("runtime", ref("runtime", "data"), "automation", "set", &domain.CommandInstructionSet{Procedures: []*domain.Procedure{procedure, procedure}}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Spec.Template.Spec.InitContainers[0].VolumeMounts) != 1 || len(set.Spec.Template.Spec.Containers[0].VolumeMounts) != 1 {
		t.Fatal("persistent command procedures did not share the execution credential")
	}
	if _, ok := set.Spec.Selector.MatchLabels[workloadidentity.ExecutionLabel]; ok {
		t.Fatal("execution identity changed immutable StatefulSet selector")
	}
	if set.Spec.Template.Annotations[commandPermissionsAnnotation] != "commands:list logs:read" {
		t.Fatal("persistent Pod authorization grants missing")
	}
}

func TestCommandsWithoutIdentityReceiveNoCredential(t *testing.T) {
	for _, identity := range []*domain.RuntimeCommandIdentity{nil, {ExecutionID: "execution"}} {
		procedure := &domain.Procedure{Image: "busybox", RuntimeIdentity: identity}
		job, err := procedureJobSpec("runtime", ref("runtime", "data"), "command", "command.0", "job", 1, procedure, nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		pod := job.Spec.Template.Spec
		if len(pod.Volumes) != 1 || pod.ServiceAccountName != "" || pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken || len(job.Spec.Template.Annotations) != 0 {
			t.Fatal("non-identity command received credentials")
		}
	}
}
