package filesystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

type StatusWriter struct{}

type status struct {
	FinishedAt time.Time `json:"finished_at"`
	PortName   string    `json:"port_name,omitempty"`
	Port       int       `json:"port,omitempty"`
	Protocol   string    `json:"protocol,omitempty"`
}

func NewStatusWriter() *StatusWriter {
	return &StatusWriter{}
}

func (w *StatusWriter) Write(root string, statusFile string, port *domain.AugmentedPort) error {
	path := statusFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, statusFile)
	}

	data := status{FinishedAt: time.Now().UTC()}
	if port != nil {
		data.PortName = port.Name
		data.Port = port.Port.Port
		data.Protocol = port.Protocol
	}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0644)
}
