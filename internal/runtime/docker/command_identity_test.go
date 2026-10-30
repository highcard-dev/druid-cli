package docker

import (
	"github.com/highcard-dev/daemon/internal/core/domain"
	"testing"
)

func TestDockerCommandHasNoCredentialMount(t *testing.T) {
	procedure := &domain.Procedure{Image: "busybox", RuntimeIdentity: &domain.RuntimeCommandIdentity{DeploymentID: "deployment", Permissions: []string{"commands:list"}}}
	_, host, err := containerSpec("automation", procedure, "docker-volume://data", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(host.Mounts) != 0 {
		t.Fatal("Docker command identity created credential state")
	}
}
