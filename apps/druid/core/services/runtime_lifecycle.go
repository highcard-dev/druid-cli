package services

import "github.com/highcard-dev/daemon/internal/core/domain"

func (s *RuntimeSupervisor) Delete(id string) error {
	return s.DeleteWithPolicy(id, false)
}

func (s *RuntimeSupervisor) DeleteWithPolicy(id string, purgeData bool) error {
	session, err := s.detachSession(id)
	if err != nil {
		return err
	}
	if err := session.DeleteRuntime(purgeData); err != nil {
		return err
	}
	session.Shutdown()
	return s.store.DeleteScroll(id)
}

func (s *RuntimeSupervisor) StartScroll(id string) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	if err := session.AutoStartServe(); err != nil {
		session.markError(err)
		return nil, err
	}
	session.mu.Lock()
	session.runtimeScroll.Status = deriveRuntimeScrollStatus(session.runtimeScroll.Commands, session.scrollService.GetFile().Commands)
	if session.runtimeScroll.Status == domain.RuntimeScrollStatusCreated {
		session.runtimeScroll.Status = domain.RuntimeScrollStatusRunning
	}
	session.runtimeScroll.LastError = ""
	err = s.store.UpdateScroll(session.runtimeScroll)
	id = session.runtimeScroll.ID
	session.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) Stop(id string) (*domain.RuntimeScroll, error) {
	session, err := s.detachSession(id)
	if err != nil {
		return nil, err
	}
	if err := session.StopRuntime(); err != nil {
		session.markError(err)
		return nil, err
	}
	session.Shutdown()
	return s.store.GetScroll(id)
}
