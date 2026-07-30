package services

import (
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (s *RuntimeSupervisor) detachSession(id string) (*RuntimeSession, error) {
	s.mu.Lock()
	session := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	if session != nil {
		return session, nil
	}
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	return session, nil
}

func (s *RuntimeSupervisor) sessionFor(id string) (*RuntimeSession, error) {
	s.mu.Lock()
	session := s.sessions[id]
	s.mu.Unlock()
	if session != nil {
		return session, nil
	}
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	return s.startSession(runtimeScroll)
}

func (s *RuntimeSupervisor) startSession(runtimeScroll *domain.RuntimeScroll) (*RuntimeSession, error) {
	s.mu.Lock()
	if session := s.sessions[runtimeScroll.ID]; session != nil {
		s.mu.Unlock()
		return session, nil
	}
	s.mu.Unlock()

	session, err := NewRuntimeSession(s.store, runtimeScroll, s.runtimeBackend)
	if err != nil {
		return nil, err
	}
	session.devDaemonURL = s.workerDaemonURL
	session.devDaemonToken = s.internalToken
	session.devAuthJWKSURL = s.authJWKSURL
	session.devRuntimeJWKSURL = s.runtimeJWKSURL
	session.Start()

	s.mu.Lock()
	if existing := s.sessions[runtimeScroll.ID]; existing != nil {
		s.mu.Unlock()
		session.stopDeploymentQueue()
		return existing, nil
	}
	s.sessions[runtimeScroll.ID] = session
	s.mu.Unlock()
	return session, nil
}

func (s *RuntimeSupervisor) markScrollError(runtimeScroll *domain.RuntimeScroll, err error) {
	logger.Log().Error("failed to restore runtime scroll", zap.String("scroll", runtimeScroll.ID), zap.Error(err))
	runtimeScroll.Status = domain.RuntimeScrollStatusError
	runtimeScroll.LastError = err.Error()
	if runtimeScroll.Procedures == nil {
		runtimeScroll.Procedures = domain.ProcedureStatusMap{}
	}
	_ = s.store.UpdateScroll(runtimeScroll)
}
