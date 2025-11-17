package services

import (
	"fmt"
	"os/exec"

	"al.essio.dev/pkg/shellescape"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type NixDependencyService struct{}

func NewNixDependencyService() *NixDependencyService { return &NixDependencyService{} }

func (s *NixDependencyService) EnsureNixInstalled() error {
	if _, err := exec.LookPath("nix-shell"); err != nil {
		return fmt.Errorf("nix-shell not found in PATH; install Nix from https://nixos.org/download and ensure 'nix-shell' is available: %w", err)
	}
	return nil
}

func (s *NixDependencyService) GetCommand(cmd []string, deps []string) []string {

	var cmds = []string{"nix-shell", "--pure"}
	for _, dep := range deps {
		cmds = append(cmds, "-p", dep)
	}
	cmds = append(cmds, "--command", shellescape.QuoteCommand(cmd))

	return cmds

}

var _ ports.NixDependencyServiceInterface = (*NixDependencyService)(nil)
