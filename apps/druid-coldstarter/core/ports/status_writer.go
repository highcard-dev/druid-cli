package ports

import "github.com/highcard-dev/daemon/internal/core/domain"

type StatusWriter interface {
	Write(scrollRoot string, statusFile string, port *domain.AugmentedPort) error
}
