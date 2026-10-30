package services

import (
	"context"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

func (s *RuntimeSupervisor) AuthenticateCommand(ctx context.Context, token string, deployment string) (workloadidentity.Identity, error) {
	// Docker MCP tools are deliberately unauthenticated: trusted local use only.
	if s.runtimeBackend.Name() == "docker" {
		record := workloadidentity.Identity{DeploymentID: deployment}
		for _, permission := range ToolPermissions {
			record.Permissions = append(record.Permissions, permission)
		}
		return record, nil
	}
	backend, ok := s.runtimeBackend.(ports.CommandIdentityBackend)
	if !ok {
		return workloadidentity.Identity{}, workloadidentity.ErrUnauthorized
	}
	record, err := backend.AuthenticateCommandIdentity(ctx, token)
	if err != nil {
		return workloadidentity.Identity{}, err
	}
	if record.DeploymentID != deployment {
		return workloadidentity.Identity{}, workloadidentity.ErrUnauthorized
	}
	return record, nil
}
