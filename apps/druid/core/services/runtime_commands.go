package services

import (
	"fmt"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"gopkg.in/yaml.v2"
)

// AddCommand persists a normal runtime command and makes it available to the
// command scheduler. It is intentionally independent from development mode.
func (s *RuntimeSupervisor) AddCommand(id string, command string, instruction *domain.CommandInstructionSet) error {
	session, err := s.sessionFor(id)
	if err != nil {
		return err
	}
	return session.AddCommand(command, instruction)
}

func (s *RuntimeSupervisor) RemoveCommand(id string, command string) error {
	session, err := s.sessionFor(id)
	if err != nil {
		return err
	}
	return session.RemoveCommand(command)
}

func (s *RuntimeSession) AddCommand(command string, instruction *domain.CommandInstructionSet) error {
	command = strings.Clone(command)
	if command == "" {
		return fmt.Errorf("command is required")
	}
	if instruction == nil {
		return fmt.Errorf("command instruction is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := *s.scrollService.GetFile()
	file.Commands = make(map[string]*domain.CommandInstructionSet, len(s.scrollService.GetFile().Commands)+1)
	for name, existing := range s.scrollService.GetFile().Commands {
		file.Commands[name] = existing
	}
	if file.Commands == nil {
		file.Commands = map[string]*domain.CommandInstructionSet{}
	}
	file.Commands[command] = instruction
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	validated, err := coreservices.NewCachedScrollServiceWithPorts(s.runtimeScroll.Root, data, s.runtimeScroll.ReservedPorts)
	if err != nil {
		return err
	}
	s.runtimeScroll.ScrollYAML = string(data)
	if err := s.store.UpdateScroll(s.runtimeScroll); err != nil {
		return err
	}
	s.scrollService = validated
	return nil
}

func (s *RuntimeSession) RemoveCommand(command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.queueMu.Lock()
	delete(s.queue, command)
	s.queueMu.Unlock()

	s.mu.Lock()
	root := s.runtimeScroll.Root
	s.mu.Unlock()
	if stopper, ok := s.runtimeBackend.(interface{ StopCommand(string, string) error }); ok {
		if err := stopper.StopCommand(root, command); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.scrollService.GetFile()
	delete(file.Commands, command)
	delete(s.runtimeScroll.Procedures, command)
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	s.runtimeScroll.ScrollYAML = string(data)
	return s.store.UpdateScroll(s.runtimeScroll)
}
