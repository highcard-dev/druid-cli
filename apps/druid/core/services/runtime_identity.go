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

func commandRegistry(backend ports.RuntimeBackendInterface) workloadidentity.Registry {
	if provider, ok := backend.(ports.StoredCommandIdentities); ok {
		return provider.WorkloadIdentities()
	}
	return nil
}

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

func (s *RuntimeSession) revokeExecution(execution string) error {
	if registry := commandRegistry(s.runtimeBackend); registry != nil && execution != "" {
		return registry.Revoke(context.Background(), execution)
	}
	return nil
}

func (s *RuntimeSession) revokeCommandIdentities(command string) error {
	seen := map[string]bool{}
	for name, procedures := range s.Snapshot() {
		if command != "" && name != command {
			continue
		}
		definition := s.scrollService.GetFile().Commands[name]
		if definition == nil || definition.Identity == nil {
			continue
		}
		for _, status := range procedures {
			if status.ExecutionID != "" && !seen[status.ExecutionID] {
				seen[status.ExecutionID] = true
				if err := s.revokeExecution(status.ExecutionID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *RuntimeSupervisor) AuthenticateCommand(ctx context.Context, token string, deployment string) (workloadidentity.Record, error) {
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

// ReconcileCommandIdentities cleans up file credentials using surviving resources.
// Kubernetes has no daemon-owned credential registry to reconcile.
func (s *RuntimeSupervisor) ReconcileCommandIdentities(ctx context.Context) error {
	backend, ok := s.runtimeBackend.(ports.StoredCommandIdentities)
	if !ok || backend.WorkloadIdentities() == nil {
		return nil
	}
	// Snapshot records first: credentials provisioned after this snapshot must
	// not be revoked using an older resource/provisioning snapshot.
	records, err := backend.WorkloadIdentities().Records(ctx)
	if err != nil {
		return err
	}
	active, err := backend.ActiveExecutions(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, session := range s.sessions {
		session.provisioning.Range(func(key, value any) bool { active[key.(string)] = true; return true })
	}
	s.mu.Unlock()
	for _, record := range records {
		if !active[record.ExecutionID] {
			if err := backend.WorkloadIdentities().Revoke(ctx, record.ExecutionID); err != nil {
				return err
			}
		}
	}
	return backend.WorkloadIdentities().Prune(ctx, active)
}
