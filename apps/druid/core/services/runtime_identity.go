package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

func (s *RuntimeSupervisor) SetWorkloadAPIURL(base string) error {
	if base != "" {
		parsed, err := url.Parse(base)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("workload API URL must be an explicit HTTP(S) base URL")
		}
	}
	s.workloadAPIURL = strings.TrimRight(base, "/")
	return nil
}

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
