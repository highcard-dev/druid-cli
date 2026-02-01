package handler

import (
	"net/http"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type ScrollLogHandler struct {
	scrollService  ports.ScrollServiceInterface
	logManager     ports.LogManagerInterface
	processManager ports.ProcessManagerInterface
}

func NewScrollLogHandler(scrollService ports.ScrollServiceInterface, logManager ports.LogManagerInterface, processManager ports.ProcessManagerInterface) *ScrollLogHandler {
	return &ScrollLogHandler{scrollService: scrollService, logManager: logManager, processManager: processManager}
}

func (sl ScrollLogHandler) ListAllLogs(c *fiber.Ctx) error {

	streams := sl.logManager.GetStreams()

	responseData := make([]api.ScrollLogStream, 0, len(streams))
	mutex := sync.Mutex{}
	wg := sync.WaitGroup{}

	for streamName, log := range streams {
		req := make(chan []byte)
		wg.Add(1)
		log.Req <- req
		go func(streamName string, res <-chan []byte, log *domain.Log) {
			defer wg.Done()

			logResponse := api.ScrollLogStream{
				Key: streamName,
				Log: make([]string, 0, log.Capacity),
			}
			for {
				cmd, ok := <-res
				if !ok {
					break
				}
				logResponse.Log = append(logResponse.Log, string(cmd))
			}
			mutex.Lock()
			defer mutex.Unlock()
			responseData = append(responseData, logResponse)
		}(streamName, req, log)
	}
	wg.Wait()
	return c.JSON(responseData)
}

func (sl ScrollLogHandler) ListStreamLogs(c *fiber.Ctx, stream string) error {

	steam, ok := sl.logManager.GetStreams()[c.Params("stream")]
	if !ok {
		c.SendStatus(http.StatusNotFound)
		return nil
	}

	responseData := api.ScrollLogStream{
		Key: c.Params("stream"),
		Log: make([]string, 0, steam.Capacity),
	}
	req := make(chan []byte)
	steam.Req <- req

	for {
		res, ok := <-req
		if !ok {
			break
		}
		responseData.Log = append(responseData.Log, string(res))
	}

	return c.JSON(responseData)
}
