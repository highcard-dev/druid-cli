package handler

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

// WatchModeResponse represents the response for enable/disable dev mode operations
type WatchModeResponse struct {
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
} // @name WatchModeResponse

// WatchStatusResponse represents the response for dev mode status
type WatchStatusResponse struct {
	Enabled      bool     `json:"enabled"`
	WatchedPaths []string `json:"watchedPaths"`
} // @name WatchStatusResponse

// ErrorResponse represents an error response
type ErrorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
} // @name ErrorResponse

type WatchHandler struct {
	uiWatchService ports.WatchServiceInterface
	scrollService  ports.ScrollServiceInterface
}

type WatchModeBody struct {
	HotReloadCommands []string `json:"hotReloadCommands,omitempty"`
	BuildCommands     []string `json:"buildCommands,omitempty"`
}

func NewWatchHandler(uiWatchService ports.WatchServiceInterface, scrollService ports.ScrollServiceInterface) *WatchHandler {
	return &WatchHandler{
		uiWatchService: uiWatchService,
		scrollService:  scrollService,
	}
}

// @Summary Enable development mode
// @ID enableWatch
// @Tags ui, dev, druid, daemon
// @Accept json
// @Produce json
// @Param body body WatchModeBody false "Optional commands to run on file changes"
// @Success 200 {object} WatchModeResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/watch/enable [post]
func (udh *WatchHandler) Enable(ctx *fiber.Ctx) error {
	if udh.uiWatchService.IsWatching() {
		response := WatchModeResponse{
			Status:  "already-active",
			Enabled: true,
		}
		ctx.Status(fiber.StatusPreconditionFailed)
		return ctx.JSON(response)
	}

	var watchPaths []string
	// Get current scroll to determine watch paths
	scrollDir := udh.scrollService.GetDir()
	if scrollDir == "" {
		logger.Log().Error("Cannot enable development mode: No scroll loaded")
		errorResponse := ErrorResponse{
			Status: "error",
			Error:  "No scroll loaded. Please load a scroll before enabling development mode.",
		}
		return ctx.Status(400).JSON(errorResponse)
	}

	var requestBody WatchModeBody

	err := ctx.BodyParser(&requestBody)
	if err == nil {
		udh.uiWatchService.SetHotReloadCommands(requestBody.HotReloadCommands)
	}

	watchPaths = append(watchPaths, filepath.Join(scrollDir, "public/src"), filepath.Join(scrollDir, "private/src"))

	// Start file watching with scroll directory as base path
	err = udh.uiWatchService.StartWatching(scrollDir, watchPaths...)
	if err != nil {
		logger.Log().Error("Failed to start file watcher", zap.Error(err))
		errorResponse := ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		}
		return ctx.Status(500).JSON(errorResponse)
	}

	logger.Log().Info("UI development mode enabled")

	response := WatchModeResponse{
		Status:  "success",
		Enabled: udh.uiWatchService.IsWatching(),
	}
	return ctx.JSON(response)
}

// @Summary Disable development mode
// @ID disableWatch
// @Tags ui, dev, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} WatchModeResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/watch/disable [post]
func (udh *WatchHandler) Disable(ctx *fiber.Ctx) error {
	if !udh.uiWatchService.IsWatching() {
		response := WatchModeResponse{
			Status:  "success",
			Enabled: false,
		}
		return ctx.JSON(response)
	}

	// Stop file watching
	err := udh.uiWatchService.StopWatching()
	if err != nil {
		logger.Log().Error("Failed to stop file watcher", zap.Error(err))
		errorResponse := ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		}
		return ctx.Status(500).JSON(errorResponse)
	}

	logger.Log().Info("UI development mode disabled")

	response := WatchModeResponse{
		Status:  "success",
		Enabled: udh.uiWatchService.IsWatching(),
	}
	return ctx.JSON(response)
}

// @Summary Get development mode status
// @ID getWatchStatus
// @Tags ui, dev, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} WatchStatusResponse
// @Router /api/v1/watch/status [get]
func (udh *WatchHandler) Status(ctx *fiber.Ctx) error {
	isWatching := udh.uiWatchService.IsWatching()
	response := WatchStatusResponse{
		Enabled:      isWatching,
		WatchedPaths: udh.uiWatchService.GetWatchedPaths(),
	}
	return ctx.JSON(response)
}

// NotifyChange handles WebSocket connections for real-time file change notifications
func (udh *WatchHandler) NotifyChange(c *websocket.Conn) {
	defer c.Close()

	// Check if development mode is enabled
	if !udh.uiWatchService.IsWatching() {
		logger.Log().Warn("WebSocket connection attempted but development mode is not enabled")
		c.WriteJSON(map[string]interface{}{
			"type":    "error",
			"message": "Watchelopment mode is not enabled",
		})
		return
	}

	// Subscribe to file change notifications
	changesChan := udh.uiWatchService.Subscribe()
	if changesChan == nil {
		logger.Log().Error("Failed to subscribe to file changes")
		c.WriteJSON(map[string]interface{}{
			"type":    "error",
			"message": "Failed to subscribe to file changes",
		})
		return
	}
	defer udh.uiWatchService.Unsubscribe(changesChan)

	// Set up ping/pong
	c.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.SetPongHandler(func(string) error {
		c.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Send initial connection message
	c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.WriteJSON(map[string]interface{}{
		"type":         "connected",
		"message":      "Connected to file watcher",
		"watchedPaths": udh.uiWatchService.GetWatchedPaths(),
		"timestamp":    time.Now(),
	}); err != nil {
		logger.Log().Debug("Failed to send initial message, client disconnected", zap.Error(err))
		return
	}

	logger.Log().Info("WebSocket client connected for file change notifications")

	// Create ping ticker
	pingTicker := time.NewTicker(54 * time.Second)
	defer pingTicker.Stop()

	// Start reader goroutine to detect disconnects
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				logger.Log().Debug("WebSocket client disconnected", zap.Error(err))
				return
			}
		}
	}()

	// Main event loop
	for {
		select {
		case <-done:
			return

		case data := <-changesChan:
			if data == nil {
				return
			}

			// Parse and send file change event
			var fileEvent map[string]interface{}
			if err := json.Unmarshal(*data, &fileEvent); err != nil {
				logger.Log().Error("Failed to parse file change event", zap.Error(err))
				continue
			}

			c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.WriteJSON(map[string]interface{}{
				"type":      "file_change",
				"data":      fileEvent,
				"timestamp": time.Now(),
			}); err != nil {
				logger.Log().Debug("Failed to send file change, client disconnected", zap.Error(err))
				return
			}

		case <-pingTicker.C:
			c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				logger.Log().Debug("Failed to send ping, client disconnected", zap.Error(err))
				return
			}
		}
	}
}

// Ensure WatchHandler implements WatchHandlerInterface at compile time
var _ ports.WatchHandlerInterface = (*WatchHandler)(nil)
