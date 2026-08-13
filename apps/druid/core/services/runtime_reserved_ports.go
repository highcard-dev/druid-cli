package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

var reservedPortIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (s *RuntimeSupervisor) SetReservedPorts(id string, reservations []domain.Port) error {
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

func (s *RuntimeSession) SetReservedPorts(reservations []domain.Port) error {
	next, err := normalizedReservedPorts(reservations)
	if err != nil {
		return err
	}
	return s.setReservedPorts(next)
}

func normalizedReservedPorts(reservations []domain.Port) ([]domain.Port, error) {
	next := append([]domain.Port(nil), reservations...)
	if err := validateReservedPorts(next); err != nil {
		return nil, err
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
	return next, nil
}

func (s *RuntimeSession) setReservedPorts(reservations []domain.Port) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeScroll.ReservedPorts = reservations
	return s.store.UpdateScroll(s.runtimeScroll)
}

func validateReservedPorts(reservations []domain.Port) error {
	names := map[string]struct{}{}
	ports := map[string]struct{}{}
	for _, reservation := range reservations {
		if strings.TrimSpace(reservation.Name) == "" {
			return fmt.Errorf("reserved port name is required")
		}
		if !reservedPortIdentifier.MatchString(reservation.Name) {
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

func mergeRuntimePorts(ports []domain.Port, reservations []domain.Port) ([]domain.Port, error) {
	merged := append([]domain.Port(nil), ports...)
	byName := make(map[string]domain.Port, len(merged))
	for _, port := range merged {
		byName[port.Name] = port
	}
	for _, reservation := range reservations {
		port := reservation
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
