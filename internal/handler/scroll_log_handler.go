package handler

import (
	"net/http"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type ScrollLogHandler struct {
	scrollService  ports.ScrollServiceInterface
	logManager     ports.LogManagerInterface
	processManager ports.ProcessManagerInterface
}

type ScrollLogStream struct {
	Key string                 `json:"key" validate:"required"`
	Log []domain.StreamCommand `json:"log" validate:"required"`
} // @name ScrollLogStream

func NewScrollLogHandler(scrollService ports.ScrollServiceInterface, logManager ports.LogManagerInterface, processManager ports.ProcessManagerInterface) *ScrollLogHandler {
	return &ScrollLogHandler{scrollService: scrollService, logManager: logManager, processManager: processManager}
}

// @Summary List all logs
// @ID listLogs
// @Tags logs, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {object} []ScrollLogStream
// @Router /api/v1/logs [get]
func (sl ScrollLogHandler) ListAllLogs(c *fiber.Ctx) error {

	processes := sl.processManager.GetRunningProcesses()

	responseData := make([]ScrollLogStream, 0, len(processes))
	mutex := sync.Mutex{}
	wg := sync.WaitGroup{}

	for streamName, log := range sl.logManager.GetStreams() {
		req := make(chan domain.StreamCommand)
		wg.Add(1)
		log.Req <- req
		go func(streamName string, res <-chan domain.StreamCommand, log *domain.Log) {
			defer wg.Done()

			logResponse := ScrollLogStream{
				Key: streamName,
				Log: make([]domain.StreamCommand, 0, log.Capacity),
			}
			for {
				cmd, ok := <-res
				if !ok {
					break
				}
				logResponse.Log = append(logResponse.Log, cmd)
			}
			mutex.Lock()
			defer mutex.Unlock()
			responseData = append(responseData, logResponse)
		}(streamName, req, log)
	}
	wg.Wait()
	return c.JSON(responseData)
}

// @Summary List stream logs
// @ID listLog
// @Tags logs, druid, daemon
// @Accept */*
// @Produce json
// @Param stream path string true "Stream name"
// @Success 200 {object} ScrollLogStream
// @Router /api/v1/logs/:stream [get]
// ListStreamLogs lists logs for a specific stream.
func (sl ScrollLogHandler) ListStreamLogs(c *fiber.Ctx) error {

	steam, ok := sl.logManager.GetStreams()[c.Params("stream")]
	if !ok {
		c.SendStatus(http.StatusNotFound)
		return nil
	}

	responseData := ScrollLogStream{
		Key: c.Params("stream"),
		Log: make([]domain.StreamCommand, 0, steam.Capacity),
	}
	req := make(chan domain.StreamCommand)
	steam.Req <- req

	for {
		res, ok := <-req
		if !ok {
			break
		}
		responseData.Log = append(responseData.Log, res)
	}

	return c.JSON(responseData)
}
