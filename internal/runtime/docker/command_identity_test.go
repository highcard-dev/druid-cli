package docker

import (
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"testing"
)

func TestCommandCredentialMountIsReadOnlyAndOutsideData(t *testing.T) {
	procedure := &domain.Procedure{Image: "busybox", RuntimeIdentity: &domain.RuntimeCommandIdentity{ExecutionID: "execution", TokenPath: "/protected/identities/execution"}}
	config, host, err := containerSpec("automation", procedure, "docker-volume://data", nil, nil, map[string]string{"DRUID_TOKEN_FILE": workloadidentity.TokenFile})
	if err != nil {
		t.Fatal(err)
	}
	if config.Labels[workloadidentity.ExecutionLabel] != "execution" {
		t.Fatal("execution label missing")
	}
	if len(host.Mounts) != 1 || host.Mounts[0].Target != workloadidentity.TokenFile || !host.Mounts[0].ReadOnly || host.Mounts[0].Source != "/protected/identities/execution" {
		t.Fatal("invalid credential mount")
	}
	procedure.RuntimeIdentity = nil
	_, host, err = containerSpec("ordinary", procedure, "docker-volume://data", nil, nil, nil)
	if err != nil || len(host.Mounts) != 0 {
		t.Fatal("non-opted-in command received a credential mount")
	}
}
