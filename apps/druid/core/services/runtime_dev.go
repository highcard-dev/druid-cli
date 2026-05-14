package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"gopkg.in/yaml.v2"
)

type DevWatchRequest struct {
	WatchPaths        []string `json:"watchPaths"`
	HotReloadCommands []string `json:"hotReloadCommands,omitempty"`
}

type DevWatchStatus struct {
	Enabled      bool     `json:"enabled"`
	WatchedPaths []string `json:"watchedPaths"`
}

func (s *RuntimeSupervisor) AddCommand(id string, command string, instruction *domain.CommandInstructionSet) error {
	session, err := s.sessionFor(id)
	if err != nil {
		return err
	}
	return session.AddCommand(command, instruction)
}

func (s *RuntimeSupervisor) EnableDevWatch(id string, request DevWatchRequest) (DevWatchStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return DevWatchStatus{}, err
	}
	return session.EnableDevWatch(request)
}

func (s *RuntimeSupervisor) DisableDevWatch(id string) (DevWatchStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return DevWatchStatus{}, err
	}
	return session.DisableDevWatch()
}

func (s *RuntimeSupervisor) DevWatchStatus(id string) (DevWatchStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return DevWatchStatus{}, err
	}
	return session.DevWatchStatus(), nil
}

func (s *RuntimeSupervisor) SubscribeDevWatch(id string) (chan *[]byte, func(), error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, nil, err
	}
	ch := session.SubscribeDevWatch()
	if ch == nil {
		return nil, nil, fmt.Errorf("dev watch is not enabled")
	}
	return ch, func() { session.UnsubscribeDevWatch(ch) }, nil
}

func (s *RuntimeSession) AddCommand(command string, instruction *domain.CommandInstructionSet) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	if instruction == nil {
		return fmt.Errorf("command instruction is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.scrollService.GetFile()
	if file.Commands == nil {
		file.Commands = map[string]*domain.CommandInstructionSet{}
	}
	file.Commands[command] = instruction
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	s.runtimeScroll.ScrollYAML = string(data)
	return s.store.UpdateScroll(s.runtimeScroll)
}

func (s *RuntimeSession) EnableDevWatch(request DevWatchRequest) (DevWatchStatus, error) {
	if len(request.WatchPaths) == 0 {
		request.WatchPaths = []string{"."}
	}
	for _, command := range request.HotReloadCommands {
		if _, err := s.scrollService.GetCommand(command); err != nil {
			return DevWatchStatus{}, err
		}
	}
	s.mu.Lock()
	root := s.runtimeScroll.Root
	id := s.runtimeScroll.ID
	routing := append([]domain.RuntimeRouteAssignment(nil), s.runtimeScroll.Routing...)
	s.devWatchPaths = append([]string(nil), request.WatchPaths...)
	s.devCommands = append([]string(nil), request.HotReloadCommands...)
	s.mu.Unlock()

	if s.devDaemonURL == "" {
		return DevWatchStatus{}, fmt.Errorf("dev daemon URL is not configured")
	}
	if err := s.runtimeBackend.StartDev(context.Background(), ports.RuntimeDevAction{
		RuntimeID:         id,
		OwnerID:           s.runtimeScroll.OwnerID,
		RootRef:           root,
		MountPath:         "/scroll",
		Listen:            ":8084",
		WatchPaths:        request.WatchPaths,
		HotReloadCommands: request.HotReloadCommands,
		Routing:           routing,
		DaemonURL:         s.devDaemonURL,
		DaemonToken:       s.devDaemonToken,
		AuthJWKSURL:       s.devAuthJWKSURL,
		RuntimeJWKSURL:    s.devRuntimeJWKSURL,
	}); err != nil {
		return DevWatchStatus{}, err
	}
	return s.DevWatchStatus(), nil
}

func (s *RuntimeSession) DisableDevWatch() (DevWatchStatus, error) {
	s.mu.Lock()
	root := s.runtimeScroll.Root
	s.devWatchPaths = nil
	s.devCommands = nil
	s.mu.Unlock()
	if err := s.runtimeBackend.StopDev(context.Background(), root); err != nil {
		return DevWatchStatus{}, err
	}
	return s.DevWatchStatus(), nil
}

func (s *RuntimeSession) DevWatchStatus() DevWatchStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.devWatchPaths) == 0 {
		return DevWatchStatus{Enabled: false, WatchedPaths: []string{}}
	}
	return DevWatchStatus{Enabled: true, WatchedPaths: append([]string(nil), s.devWatchPaths...)}
}

func (s *RuntimeSession) SubscribeDevWatch() chan *[]byte {
	return nil
}

func (s *RuntimeSession) UnsubscribeDevWatch(ch chan *[]byte) {
	if s.watchService != nil {
		s.watchService.Unsubscribe(ch)
	}
}
