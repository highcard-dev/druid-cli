package services

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type SnapshotService struct {
	currentProgressTracker ports.ProgressTracker
	currentMode            ports.SnapshotMode
}

func NewSnapshotService() *SnapshotService {
	return &SnapshotService{
		currentMode: ports.SnapshotModeNoop,
	}
}

func (rc *SnapshotService) setActivity(mode ports.SnapshotMode, progressTracker ports.ProgressTracker) {
	rc.currentMode = mode
	rc.currentProgressTracker = progressTracker
}

func (rc *SnapshotService) GetCurrentProgressTracker() *ports.ProgressTracker {
	if rc.currentMode == ports.SnapshotModeNoop {
		return nil
	}
	return &rc.currentProgressTracker
}

func (rc *SnapshotService) Snapshot(dir string, destination string, options ports.SnapshotOptions) error {

	totalFiles, _ := GetTotalFiles(dir)
	progessTracker := NewBasicTracker(totalFiles)

	rc.setActivity(ports.SnapshotModeSnapshot, progessTracker)
	defer rc.setActivity(ports.SnapshotModeNoop, progessTracker)
	//check if rootPath exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("source path does not exist: %s", dir)
	}

	err := Upload(dir, destination, options.S3Destination, options.CompressionLevel, progessTracker)
	if err != nil {
		return fmt.Errorf("failed to upload snapshot: %w", err)
	}
	logger.Log().Info("Snapshot created", zap.String("destination", destination))
	return nil
}

func (rc *SnapshotService) RestoreSnapshot(dir string, source string, options ports.RestoreSnapshotOptions) error {

	info, err := CheckFileInfo(context.Background(), source)
	if err != nil {
		logger.Log().Error("Error checking file info", zap.Error(err))
		return err
	}

	progressReader := NewBasicTracker(info.Size)

	dest := path.Join(dir, ".snap_dl")

	os.RemoveAll(dest)
	defer os.RemoveAll(dest)

	if _, err := os.Stat(dir); err == nil {
		logger.Log().Info("Removing folder to make space for backup", zap.String("dir", dir))
		err := utils.RemoveContents(dir)
		if err != nil {
			return err
		}
	}

	rc.setActivity(ports.SnapshotModeRestore, progressReader)
	defer rc.setActivity(ports.SnapshotModeNoop, nil)

	logger.Log().Info("Restoring backup", zap.String("source", source), zap.String("destination", dir))

	// Download the file
	err = DownloadToDirectory(context.Background(), dest, source, progressReader)
	if err != nil {
		os.RemoveAll(dest)
		logger.Log().Error("Error occured while getting backup", zap.Error(err))
		return err
	}

	// Move the downloaded file to the destination
	err = utils.MoveContents(dest, dir)
	if err != nil {
		return err
	}

	logger.Log().Info("Backup restored", zap.String("source", source), zap.String("destination", dir))
	return nil
}

func (rc *SnapshotService) GetProgressTracker() *ports.ProgressTracker {
	if rc.currentMode == ports.SnapshotModeNoop {
		return nil
	}
	return &rc.currentProgressTracker
}

func (rc *SnapshotService) GetCurrentMode() ports.SnapshotMode {
	return rc.currentMode
}
