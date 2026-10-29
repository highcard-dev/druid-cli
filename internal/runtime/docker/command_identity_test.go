package docker

import (
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"testing"
)

func TestDockerCommandHasExecutionLabelButNoCredentialMount(t *testing.T) {
	procedure := &domain.Procedure{Image: "busybox", RuntimeIdentity: &domain.RuntimeCommandIdentity{ExecutionID: "execution"}}
	config, host, err := containerSpec("automation", procedure, "docker-volume://data", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.Labels[workloadidentity.ExecutionLabel] != "execution" || len(host.Mounts) != 0 {
		t.Fatal("Docker command identity created credential state")
	}
}
