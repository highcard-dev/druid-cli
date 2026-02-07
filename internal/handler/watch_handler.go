package handler

import (
	"encoding/json"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type WatchHandler struct {
	uiWatchService ports.WatchServiceInterface
	scrollService  ports.ScrollServiceInterface
}

func NewWatchHandler(uiWatchService ports.WatchServiceInterface, scrollService ports.ScrollServiceInterface) *WatchHandler {
	return &WatchHandler{
		uiWatchService: uiWatchService,
		scrollService:  scrollService,
	}
}

func (udh *WatchHandler) EnableWatch(c *fiber.Ctx) error {
	if udh.uiWatchService.IsWatching() {
		response := api.WatchModeResponse{
			Status:  "already-active",
			Enabled: true,
		}
		c.Status(fiber.StatusPreconditionFailed)
		return c.JSON(response)
	}

	// Get current scroll to determine watch paths
	scrollDir := udh.scrollService.GetDir()
	if scrollDir == "" {
		logger.Log().Error("Cannot enable development mode: No scroll loaded")
		errorResponse := api.ErrorResponse{
			Status: "error",
			Error:  "No scroll loaded. Please load a scroll before enabling development mode.",
		}
		return c.Status(400).JSON(errorResponse)
	}

	var requestBody api.WatchModeRequest

	err := c.BodyParser(&requestBody)
	if err == nil && requestBody.HotReloadCommands != nil {
		err = udh.uiWatchService.SetHotReloadCommands(*requestBody.HotReloadCommands)
		if err != nil {
			logger.Log().Error("Invalid hot reload commands", zap.Error(err))
			errorResponse := api.ErrorResponse{
				Status: "error",
				Error:  err.Error(),
			}
			return c.Status(400).JSON(errorResponse)
		}
	}

	watchPaths := requestBody.WatchPaths

	if len(watchPaths) == 0 {
		return c.Status(400).JSON(api.ErrorResponse{
			Status: "error",
			Error:  "At least one watch path must be specified",
		})
	}

	// Start file watching with scroll directory as base path
	err = udh.uiWatchService.StartWatching(scrollDir, watchPaths...)
	if err != nil {
		logger.Log().Error("Failed to start file watcher", zap.Error(err))
		errorResponse := api.ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		}
		return c.Status(500).JSON(errorResponse)
	}

	logger.Log().Info("UI development mode enabled")

	response := api.WatchModeResponse{
		Status:  "success",
		Enabled: udh.uiWatchService.IsWatching(),
	}
	return c.JSON(response)
}

func (udh *WatchHandler) DisableWatch(c *fiber.Ctx) error {
	if !udh.uiWatchService.IsWatching() {
		response := api.WatchModeResponse{
			Status:  "success",
			Enabled: false,
		}
		return c.JSON(response)
	}

	// Stop file watching
	err := udh.uiWatchService.StopWatching()
	if err != nil {
		logger.Log().Error("Failed to stop file watcher", zap.Error(err))
		errorResponse := api.ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		}
		return c.Status(500).JSON(errorResponse)
	}

	logger.Log().Info("UI development mode disabled")

	response := api.WatchModeResponse{
		Status:  "success",
		Enabled: udh.uiWatchService.IsWatching(),
	}
	return c.JSON(response)
}

func (udh *WatchHandler) GetWatchStatus(c *fiber.Ctx) error {
	isWatching := udh.uiWatchService.IsWatching()
	response := api.WatchStatusResponse{
		Enabled:      isWatching,
		WatchedPaths: udh.uiWatchService.GetWatchedPaths(),
	}
	return c.JSON(response)
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
