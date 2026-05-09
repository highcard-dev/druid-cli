package services

import (
	"errors"
	"os"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils"
)

type ScrollService struct {
	scrollDir string
	scroll    *domain.Scroll
}

func NewScrollService(
	scrollDir string,
) (*ScrollService, error) {
	s := &ScrollService{
		scrollDir: scrollDir,
	}

	_, err := s.ReloadScroll()

	return s, err
}

func NewCachedScrollService(scrollDir string, scrollYAML []byte) (*ScrollService, error) {
	s := &ScrollService{
		scrollDir: scrollDir,
	}
	scroll, err := domain.NewScrollFromBytes(scrollDir, scrollYAML)
	if err != nil {
		return nil, err
	}
	if err := scroll.Validate(false); err != nil {
		return nil, err
	}
	s.scroll = scroll
	return s, nil
}

func (sc *ScrollService) ReloadScroll() (*domain.Scroll, error) {
	scroll, err := domain.NewScroll(sc.GetDir())

	if err != nil {
		return nil, err
	}

	//enseure data dir exists
	err = os.MkdirAll(sc.GetCwd(), os.ModePerm)
	if err != nil {
		return nil, err
	}

	sc.scroll = scroll

	return scroll, nil
}

func (sc *ScrollService) GetDir() string {
	return sc.scrollDir
}

func (sc *ScrollService) GetCwd() string {
	return utils.GetDataDirFromScrollDir(sc.scrollDir)
}

func (s ScrollService) GetCurrent() *domain.Scroll {
	return s.scroll
}
func (s ScrollService) GetFile() *domain.File {
	return &s.scroll.File
}

func (s ScrollService) ScrollExists() bool {

	filePath := s.GetDir() + "/scroll.yaml"
	b, err := utils.FileExists(filePath)
	return b && err == nil
}

func (sc *ScrollService) GetCommand(cmd string) (*domain.CommandInstructionSet, error) {
	scroll := sc.GetFile()
	//check if we can accually do it before we start
	if cmds, ok := scroll.Commands[cmd]; ok {
		return cmds, nil
	} else {
		return nil, errors.New("command " + cmd + " not found")
	}
}
