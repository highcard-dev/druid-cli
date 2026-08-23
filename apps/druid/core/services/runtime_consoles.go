package services

import (
	"context"
	"fmt"
	"io"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func (s *RuntimeSupervisor) Consoles(id string) (map[string]domain.Console, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.consoles(), nil
}

func (s *RuntimeSupervisor) OpenConsole(ctx context.Context, id string, procedure string) (io.ReadWriteCloser, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	if _, ok := session.consoles()[procedure]; !ok {
		return nil, fmt.Errorf("console %s is unavailable", procedure)
	}
	session.mu.Lock()
	root := session.runtimeScroll.Root
	session.mu.Unlock()
	return session.runtimeBackend.OpenConsole(ctx, root, procedure)
}

func (s *RuntimeSession) consoles() map[string]domain.Console {
	s.mu.Lock()
	statuses := copyProcedureStatuses(s.runtimeScroll.Procedures)
	s.mu.Unlock()
	consoles := map[string]domain.Console{}
	for commandName, command := range s.scrollService.GetFile().Commands {
		if command == nil {
			continue
		}
		for index, procedure := range command.Procedures {
			name := domain.ProcedureName(commandName, index, procedure)
			status, ok := statuses[commandName][name]
			if !ok || procedure == nil || !procedure.IsContainer() {
				continue
			}
			consoleType := domain.ConsoleTypeContainer
			if procedure.TTY {
				consoleType = domain.ConsoleTypeTTY
			}
			consoles[name] = domain.Console{Type: consoleType, InputMode: "stdin", Exit: status.ExitCode}
		}
	}
	return consoles
}
