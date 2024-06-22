package services

import (
	"strings"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type CronManager struct {
	crons           []*domain.Cronjob
	processLauncher ports.ProcedureLauchnerInterface
}

func NewCronManager(cronjobs []*domain.Cronjob, processLauncher ports.ProcedureLauchnerInterface) *CronManager {
	return &CronManager{
		crons:           cronjobs,
		processLauncher: processLauncher,
	}
}

func (c *CronManager) Init() error {
	scheduler := gocron.NewScheduler(time.UTC)
	for _, cron := range c.crons {
		//gocron
		_, err := scheduler.Cron(cron.Schedule).Do(func() {
			logger.Log().Info("Cronjob started", zap.String("name", cron.Name))

			//parse cron.Command e.g. main.start
			//split by dot
			parts := strings.Split(cron.Command, ".")
			process := parts[0]
			command := parts[1]

			err := c.processLauncher.RunNew(command, process, false)

			if err != nil {
				logger.Log().Error("error running cronjob", zap.String("name", cron.Name), zap.Error(err))
			} else {
				logger.Log().Info("Cronjob finished", zap.String("name", cron.Name))
			}
		})
		if err != nil {
			return err
		}

	}
	scheduler.StartAsync()
	return nil

}
