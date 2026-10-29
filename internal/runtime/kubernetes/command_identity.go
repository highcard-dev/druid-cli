package kubernetes

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	corev1 "k8s.io/api/core/v1"
)

const commandServiceAccount = "druid-workload-agent"
const commandTokenAudience = "druid-command-tools"
const commandDeploymentAnnotation = "druid.gg/deployment-id"
const commandPermissionsAnnotation = "druid.gg/permissions"

func commandIdentityAnnotations(identity *domain.RuntimeCommandIdentity, command string) map[string]string {
	if identity == nil {
		return nil
	}
	return map[string]string{commandDeploymentAnnotation: identity.DeploymentID, labelCommand: command, commandPermissionsAnnotation: strings.Join(identity.Permissions, " ")}
}

func prepareCommandIdentity(command ports.RuntimeCommand) (ports.RuntimeCommand, error) {
	if command.Command.Identity == nil {
		return command, nil
	}
	if command.WorkloadAPIURL == "" {
		return command, fmt.Errorf("command identity requires workload API URL")
	}
	definition := *command.Command
	definition.Procedures = make([]*domain.Procedure, len(command.Command.Procedures))
	command.ProcedureEnv = maps.Clone(command.ProcedureEnv)
	if command.ProcedureEnv == nil {
		command.ProcedureEnv = make(map[string]map[string]string)
	}
	identity := &domain.RuntimeCommandIdentity{DeploymentID: command.ScrollID, Permissions: definition.Identity.Permissions}
	for index, procedure := range command.Command.Procedures {
		if procedure == nil {
			continue
		}
		copyProcedure := *procedure
		copyProcedure.RuntimeIdentity = identity
		definition.Procedures[index] = &copyProcedure
		name := domain.ProcedureName(command.Name, index, procedure)
		env := command.ProcedureEnv[name]
		if env == nil {
			env = procedure.Env
		}
		env = maps.Clone(env)
		if env == nil {
			env = make(map[string]string)
		}
		env["DRUID_TOKEN_FILE"] = workloadidentity.TokenFile
		env["DRUID_API_URL"] = command.WorkloadAPIURL + "/" + url.PathEscape(command.ScrollID)
		command.ProcedureEnv[name] = env
	}
	command.Command = &definition
	return command, nil
}

func (b *Backend) AuthenticateCommandIdentity(ctx context.Context, token string) (workloadidentity.Identity, error) {
	pod, err := b.authenticateBoundPod(ctx, token, commandTokenAudience)
	if err != nil {
		return workloadidentity.Identity{}, err
	}
	deployment, command := pod.Annotations[commandDeploymentAnnotation], pod.Annotations[labelCommand]
	if pod.Spec.ServiceAccountName != commandServiceAccount || pod.DeletionTimestamp != nil || (pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending) || pod.Labels[labelManagedBy] != "druid" || pod.Labels[labelComponent] != "runtime" || deployment == "" || command == "" || pod.Labels[labelCommand] != dnsLabel(command) {
		return workloadidentity.Identity{}, workloadidentity.ErrUnauthorized
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "data" && volume.PersistentVolumeClaim != nil && pod.Labels[labelScrollID] == dnsLabel(volume.PersistentVolumeClaim.ClaimName) {
			root := ref(pod.Namespace, volume.PersistentVolumeClaim.ClaimName)
			if pod.Labels[labelRuntimeID] != dnsLabel(runtimeID(root)) {
				return workloadidentity.Identity{}, workloadidentity.ErrUnauthorized
			}
			return workloadidentity.Identity{DeploymentID: deployment, Permissions: strings.Fields(pod.Annotations[commandPermissionsAnnotation])}, nil
		}
	}
	return workloadidentity.Identity{}, workloadidentity.ErrUnauthorized
}
