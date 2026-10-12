package kubernetes

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	corev1 "k8s.io/api/core/v1"
)

func TestRuntimeWorkloadsUseImagePullPolicy(t *testing.T) {
	for _, tc := range []struct {
		image string
		want  corev1.PullPolicy
	}{
		{"artifacts.druid.gg/druid-team/druid-dev:stable", corev1.PullAlways},
		{"alpine:3.20", corev1.PullAlways},
		{"druid@sha256:abc", corev1.PullIfNotPresent},
		{"druid:local", corev1.PullIfNotPresent},
	} {
		t.Run(tc.image, func(t *testing.T) {
			root := ref("games", "runtime-data")
			procedure := &domain.Procedure{Image: tc.image}
			job, err := procedureJobSpec("games", root, "build", "build", "build-0", 0, procedure, nil, "", "")
			if err != nil {
				t.Fatal(err)
			}
			initName, mainName := "init", "files"
			command := &domain.CommandInstructionSet{Procedures: []*domain.Procedure{
				{Id: &initName, Image: tc.image},
				{Id: &mainName, Image: tc.image},
			}}
			sts, err := persistentStatefulSetSpec("games", root, "dev", "dev", command, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			helper := helperJobSpec("games", "helper", "runtime-data", tc.image, nil, "", nil)
			for _, container := range []corev1.Container{
				job.Spec.Template.Spec.Containers[0],
				sts.Spec.Template.Spec.Containers[0],
				sts.Spec.Template.Spec.InitContainers[0],
				helper.Spec.Template.Spec.Containers[0],
			} {
				if container.ImagePullPolicy != tc.want {
					t.Errorf("container %s (%s) pull policy = %s, want %s", container.Name, container.Image, container.ImagePullPolicy, tc.want)
				}
			}
		})
	}
}
