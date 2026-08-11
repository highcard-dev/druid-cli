package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

var reservedPortIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (s *RuntimeSupervisor) SetReservedPorts(id string, reservations []domain.RuntimePortReservation) error {
	next, err := normalizedReservedPorts(reservations)
	if err != nil {
		return err
	}
	s.mu.Lock()
	session := s.sessions[id]
	s.mu.Unlock()
	if session != nil {
		return session.setReservedPorts(next)
	}

	// An older daemon may have persisted commands before it persisted their
	// runtime-level ports. Repair that state without first constructing a
	// session, because strict scroll validation correctly rejects it.
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return err
	}
	runtimeScroll.ReservedPorts = next
	return s.store.UpdateScroll(runtimeScroll)
}

func (s *RuntimeSession) SetReservedPorts(reservations []domain.RuntimePortReservation) error {
	next, err := normalizedReservedPorts(reservations)
	if err != nil {
		return err
	}
	return s.setReservedPorts(next)
}

func normalizedReservedPorts(reservations []domain.RuntimePortReservation) ([]domain.RuntimePortReservation, error) {
	next := append([]domain.RuntimePortReservation(nil), reservations...)
	if err := validateReservedPorts(next); err != nil {
		return nil, err
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
	return next, nil
}

func (s *RuntimeSession) setReservedPorts(reservations []domain.RuntimePortReservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeScroll.ReservedPorts = reservations
	return s.store.UpdateScroll(s.runtimeScroll)
}

func reservedRuntimePorts(reservations []domain.RuntimePortReservation) []domain.Port {
	ports := make([]domain.Port, 0, len(reservations))
	for _, reservation := range reservations {
		ports = append(ports, domain.Port{Name: reservation.Name, Port: reservation.Port, Protocol: reservation.Protocol})
	}
	return ports
}

func validateReservedPorts(reservations []domain.RuntimePortReservation) error {
	names := map[string]struct{}{}
	ports := map[string]struct{}{}
	for _, reservation := range reservations {
		if strings.TrimSpace(reservation.Name) == "" || strings.TrimSpace(reservation.Command) == "" || strings.TrimSpace(reservation.Procedure) == "" {
			return fmt.Errorf("reserved port name, command, and procedure are required")
		}
		if !reservedPortIdentifier.MatchString(reservation.Name) || !reservedPortIdentifier.MatchString(reservation.Command) || !reservedPortIdentifier.MatchString(reservation.Procedure) {
			return fmt.Errorf("reserved port %s contains an unsafe identifier", reservation.Name)
		}
		if reservation.Port < 1 || reservation.Port > 65535 {
			return fmt.Errorf("reserved port %s has invalid port %d", reservation.Name, reservation.Port)
		}
		switch reservation.Protocol {
		case "http", "https", "tcp", "udp":
		default:
			return fmt.Errorf("reserved port %s has invalid protocol %q", reservation.Name, reservation.Protocol)
		}
		if _, ok := names[reservation.Name]; ok {
			return fmt.Errorf("reserved port name %q is duplicated", reservation.Name)
		}
		names[reservation.Name] = struct{}{}
		key := fmt.Sprintf("%d/%s", reservation.Port, reservation.Protocol)
		if _, ok := ports[key]; ok {
			return fmt.Errorf("reserved port %s duplicates %s", reservation.Name, key)
		}
		ports[key] = struct{}{}
	}
	return nil
}

func mergeRuntimePorts(ports []domain.Port, reservations []domain.RuntimePortReservation) ([]domain.Port, error) {
	merged := append([]domain.Port(nil), ports...)
	byName := make(map[string]domain.Port, len(merged))
	for _, port := range merged {
		byName[port.Name] = port
	}
	for _, reservation := range reservations {
		port := domain.Port{Name: reservation.Name, Port: reservation.Port, Protocol: reservation.Protocol}
		if current, ok := byName[port.Name]; ok {
			if current.Port != port.Port || current.Protocol != port.Protocol {
				return nil, fmt.Errorf("reserved port %s conflicts with scroll port", port.Name)
			}
			continue
		}
		merged = append(merged, port)
		byName[port.Name] = port
	}
	return merged, nil
}

func routingCommands(commands map[string]*domain.CommandInstructionSet, reservations []domain.RuntimePortReservation) map[string]*domain.CommandInstructionSet {
	result := make(map[string]*domain.CommandInstructionSet, len(commands)+len(reservations))
	claimed := map[string]struct{}{}
	for name, command := range commands {
		result[name] = command
		if command == nil {
			continue
		}
		for _, procedure := range command.Procedures {
			if procedure == nil {
				continue
			}
			for _, expected := range procedure.ExpectedPorts {
				claimed[expected.Name] = struct{}{}
			}
		}
	}
	for _, reservation := range reservations {
		if _, ok := claimed[reservation.Name]; ok {
			continue
		}
		command := result[reservation.Command]
		if command == nil {
			command = &domain.CommandInstructionSet{Run: domain.RunModePersistent}
		} else {
			clone := *command
			clone.Procedures = append([]*domain.Procedure(nil), command.Procedures...)
			command = &clone
		}
		result[reservation.Command] = command
		id := reservation.Procedure
		command.Procedures = append(command.Procedures, &domain.Procedure{
			Id:            &id,
			ExpectedPorts: []domain.ExpectedPort{{Name: reservation.Name}},
		})
	}
	return result
}
