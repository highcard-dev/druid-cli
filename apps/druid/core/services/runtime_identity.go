package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
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

func (s *RuntimeSession) beginExecution(command, execution string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeScroll.Procedures == nil {
		s.runtimeScroll.Procedures = domain.ProcedureStatusMap{}
	}
	definition := s.scrollService.GetFile().Commands[command]
	if definition == nil {
		return fmt.Errorf("command is unavailable")
	}
	statuses := map[string]domain.LockStatus{}
	for index, procedure := range definition.Procedures {
		statuses[domain.ProcedureName(command, index, procedure)] = domain.LockStatus{ExecutionID: execution, Status: domain.ScrollLockStatusWaiting, LastStatusChange: time.Now().Unix()}
	}
	s.runtimeScroll.Procedures[command] = statuses
	return s.store.UpdateScroll(s.runtimeScroll)
}

func (s *RuntimeSession) assignExecutionIdentity(command, execution string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for procedure, status := range s.runtimeScroll.Procedures[command] {
		status.ExecutionID = execution
		s.runtimeScroll.Procedures[command][procedure] = status
	}
	return s.store.UpdateScroll(s.runtimeScroll)
}

func (s *RuntimeSupervisor) AuthenticateCommand(ctx context.Context, token string, deployment string) (workloadidentity.Record, error) {
	// Docker MCP tools are deliberately unauthenticated: trusted local use only.
	if s.runtimeBackend.Name() == "docker" {
		record := workloadidentity.Record{DeploymentID: deployment}
		for _, permission := range ToolPermissions {
			record.Permissions = append(record.Permissions, permission)
		}
		return record, nil
	}
	backend, ok := s.runtimeBackend.(ports.CommandIdentityBackend)
	if !ok {
		return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
	}
	record, err := backend.AuthenticateCommandIdentity(ctx, token)
	if err != nil {
		return workloadidentity.Record{}, err
	}
	if record.DeploymentID != deployment {
		return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
	}
	return record, nil
}
