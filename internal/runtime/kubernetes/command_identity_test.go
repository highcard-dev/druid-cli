package kubernetes

import (
	"reflect"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestPrepareCommandIdentityIsBackendLocal(t *testing.T) {
	procedure := &domain.Procedure{Image: "busybox", Env: map[string]string{"SOURCE": "procedure"}}
	resolved := map[string]string{"SOURCE": "resolved"}
	definition := &domain.CommandInstructionSet{Identity: &domain.CommandIdentity{Permissions: []string{"commands:list"}}, Procedures: []*domain.Procedure{procedure, procedure}}
	input := ports.RuntimeCommand{Name: "agent", ScrollID: "deployment", WorkloadAPIURL: "http://runtime.local", Command: definition, ProcedureEnv: map[string]map[string]string{"agent.0": resolved}}
	prepared, err := prepareCommandIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	for index, p := range prepared.Command.Procedures {
		name := domain.ProcedureName("agent", index, p)
		if p.RuntimeIdentity == nil || p.RuntimeIdentity.DeploymentID != "deployment" || !reflect.DeepEqual(p.RuntimeIdentity.Permissions, definition.Identity.Permissions) {
			t.Fatal("missing backend identity metadata")
		}
		env := prepared.ProcedureEnv[name]
		if env["DRUID_TOKEN_FILE"] != "/run/secrets/druid/token" || env["DRUID_API_URL"] != "http://runtime.local/deployment" {
			t.Fatalf("environment=%v", env)
		}
	}
	if prepared.ProcedureEnv["agent.0"]["SOURCE"] != "resolved" || prepared.ProcedureEnv["agent.1"]["SOURCE"] != "procedure" {
		t.Fatal("existing environment was lost")
	}
	if procedure.RuntimeIdentity != nil || len(procedure.Env) != 1 || len(resolved) != 1 || len(input.ProcedureEnv) != 1 {
		t.Fatal("backend mutated input definition/environment")
	}
	input.WorkloadAPIURL = ""
	if _, err := prepareCommandIdentity(input); err == nil {
		t.Fatal("missing API URL accepted")
	}
	input.Command = &domain.CommandInstructionSet{Procedures: []*domain.Procedure{procedure}}
	input.WorkloadAPIURL = "http://runtime.local"
	unchanged, err := prepareCommandIdentity(input)
	if err != nil || unchanged.Command != input.Command || unchanged.Command.Procedures[0].RuntimeIdentity != nil {
		t.Fatal("non-identity command was changed")
	}
}

func TestRunCommandDoesNotProvisionServiceAccounts(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "games"}, client)
	_, _ = backend.RunCommand(ports.RuntimeCommand{Name: "agent", ScrollID: "deployment", Root: ref("games", "data"), WorkloadAPIURL: "http://runtime.local", Command: &domain.CommandInstructionSet{Identity: &domain.CommandIdentity{}, Procedures: []*domain.Procedure{{}}}})
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "serviceaccounts" {
			t.Fatalf("command execution accessed ServiceAccounts: %v", action)
		}
	}
}

func TestCommandIdentityMountedInJobsAndPersistentProcedures(t *testing.T) {
	identity := &domain.RuntimeCommandIdentity{DeploymentID: "deployment", Permissions: []string{"commands:list", "logs:read"}}
	procedure := &domain.Procedure{Image: "busybox", RuntimeIdentity: identity}
	job, err := procedureJobSpec("runtime", ref("runtime", "data"), "automation", "automation.0", "job", 1, procedure, nil, "", "")
	if err != nil {
		t.Fatal(err)
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
	if set.Spec.Template.Annotations[commandPermissionsAnnotation] != "commands:list logs:read" {
		t.Fatal("persistent Pod authorization grants missing")
	}
}

func TestCommandsWithoutIdentityReceiveNoCredential(t *testing.T) {
	procedure := &domain.Procedure{Image: "busybox"}
	job, err := procedureJobSpec("runtime", ref("runtime", "data"), "command", "command.0", "job", 1, procedure, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	pod := job.Spec.Template.Spec
	if len(pod.Volumes) != 1 || pod.ServiceAccountName != "" || pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken || len(job.Spec.Template.Annotations) != 0 {
		t.Fatal("non-identity command received credentials")
	}
}
