package handler

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

// DevModeResponse represents the response for enable/disable dev mode operations
type DevModeResponse struct {
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
} // @name DevModeResponse

// DevStatusResponse represents the response for dev mode status
type DevStatusResponse struct {
	Enabled      bool     `json:"enabled"`
	WatchedPaths []string `json:"watchedPaths"`
} // @name DevStatusResponse

// ErrorResponse represents an error response
type ErrorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
} // @name ErrorResponse

type UiDevHandler struct {
	uiDevService  ports.UiDevServiceInterface
	scrollService ports.ScrollServiceInterface
}

func NewUiDevHandler(uiDevService ports.UiDevServiceInterface, scrollService ports.ScrollServiceInterface) *UiDevHandler {
	return &UiDevHandler{
		uiDevService:  uiDevService,
		scrollService: scrollService,
	}
}

// isConnectionError checks if the error is related to a broken connection
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common connection error patterns
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "use of closed network connection") {
		return true
	}

	// Check for specific error types
	if netErr, ok := err.(*net.OpError); ok {
		if netErr.Op == "write" {
			return true
		}
	}

	// Check for syscall errors
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EPIPE || errno == syscall.ECONNRESET
	}

	return false
}

// @Summary Enable development mode
// @ID enableDev
// @Tags ui, dev, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} DevModeResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/dev/enable [post]
func (udh *UiDevHandler) Enable(ctx *fiber.Ctx) error {
	if udh.uiDevService.IsWatching() {
		response := DevModeResponse{
			Status:  "success",
			Enabled: true,
		}
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

	watchPaths = append(watchPaths, filepath.Join(scrollDir, "public"), filepath.Join(scrollDir, "private"))

	// Start file watching with scroll directory as base path
	err := udh.uiDevService.StartWatching(scrollDir, watchPaths...)
	if err != nil {
		logger.Log().Error("Failed to start file watcher", zap.Error(err))
		errorResponse := ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		}
		return ctx.Status(500).JSON(errorResponse)
	}

	logger.Log().Info("UI development mode enabled")

	response := DevModeResponse{
		Status:  "success",
		Enabled: udh.uiDevService.IsWatching(),
	}
	return ctx.JSON(response)
}

// @Summary Disable development mode
// @ID disableDev
// @Tags ui, dev, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} DevModeResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/dev/disable [post]
func (udh *UiDevHandler) Disable(ctx *fiber.Ctx) error {
	if !udh.uiDevService.IsWatching() {
		response := DevModeResponse{
			Status:  "success",
			Enabled: false,
		}
		return ctx.JSON(response)
	}

	// Stop file watching
	err := udh.uiDevService.StopWatching()
	if err != nil {
		logger.Log().Error("Failed to stop file watcher", zap.Error(err))
		errorResponse := ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		}
		return ctx.Status(500).JSON(errorResponse)
	}

	logger.Log().Info("UI development mode disabled")

	response := DevModeResponse{
		Status:  "success",
		Enabled: udh.uiDevService.IsWatching(),
	}
	return ctx.JSON(response)
}

// @Summary Get development mode status
// @ID getDevStatus
// @Tags ui, dev, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} DevStatusResponse
// @Router /api/v1/dev/status [get]
func (udh *UiDevHandler) Status(ctx *fiber.Ctx) error {
	isWatching := udh.uiDevService.IsWatching()
	response := DevStatusResponse{
		Enabled:      isWatching,
		WatchedPaths: udh.uiDevService.GetWatchedPaths(),
	}
	return ctx.JSON(response)
}

// NotifyChange handles WebSocket connections for real-time file change notifications
func (udh *UiDevHandler) NotifyChange(c *websocket.Conn) {
	// Set connection timeouts
	const (
		writeWait  = 10 * time.Second
		pongWait   = 60 * time.Second
		pingPeriod = (pongWait * 9) / 10
	)

	// Create a done channel to signal when the connection should be closed
	done := make(chan struct{})

	defer func() {
		close(done)
		if err := c.Close(); err != nil {
			logger.Log().Debug("Error closing WebSocket connection", zap.Error(err))
		}
	}()

	c.SetReadDeadline(time.Now().Add(pongWait))
	c.SetPongHandler(func(string) error {
		c.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Check if development mode is enabled
	if !udh.uiDevService.IsWatching() {
		logger.Log().Warn("WebSocket connection attempted but development mode is not enabled")
		errorMsg := map[string]interface{}{
			"type":    "error",
			"message": "Development mode is not enabled",
		}
		c.WriteJSON(errorMsg)
		return
	}

	// Subscribe to file change notifications
	changesChan := udh.uiDevService.Subscribe()
	if changesChan == nil {
		logger.Log().Error("Failed to subscribe to file changes")
		errorMsg := map[string]interface{}{
			"type":    "error",
			"message": "Failed to subscribe to file changes",
		}
		c.WriteJSON(errorMsg)
		return
	}

	defer udh.uiDevService.Unsubscribe(changesChan)

	// Send initial connection message
	connectMsg := map[string]interface{}{
		"type":         "connected",
		"message":      "Connected to file watcher",
		"watchedPaths": udh.uiDevService.GetWatchedPaths(),
		"timestamp":    time.Now(),
	}

	c.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.WriteJSON(connectMsg); err != nil {
		logger.Log().Error("Failed to send initial connection message", zap.Error(err))
		return
	}

	logger.Log().Info("WebSocket client connected for file change notifications")

	// Start ping ticker
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	// Start a goroutine to read messages (to handle pong responses and detect broken connections)
	go func() {
		defer func() {
			select {
			case <-done:
				// Connection is already being closed
			default:
				close(done)
			}
		}()

		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				if isConnectionError(err) {
					logger.Log().Debug("WebSocket client disconnected", zap.Error(err))
				} else {
					logger.Log().Warn("WebSocket read error", zap.Error(err))
				}
				return
			}
		}
	}()

	// Handle messages and file changes
	for {
		select {
		case <-done:
			logger.Log().Debug("WebSocket connection done signal received")
			return

		case data := <-changesChan:
			if data == nil {
				logger.Log().Info("File change channel closed")
				return
			}

			// Parse the file change event
			var fileEvent map[string]interface{}
			if err := json.Unmarshal(*data, &fileEvent); err != nil {
				logger.Log().Error("Failed to parse file change event", zap.Error(err))
				continue
			}

			// Add message type and send to client
			changeMessage := map[string]interface{}{
				"type":      "file_change",
				"data":      fileEvent,
				"timestamp": time.Now(),
			}

			c.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.WriteJSON(changeMessage); err != nil {
				if isConnectionError(err) {
					logger.Log().Debug("WebSocket client disconnected while sending file change", zap.Error(err))
				} else {
					logger.Log().Error("Failed to write file change to WebSocket", zap.Error(err))
				}
				return
			}

			logger.Log().Debug("File change event sent to WebSocket client",
				zap.String("path", fileEvent["path"].(string)),
				zap.String("operation", fileEvent["operation"].(string)))

		case <-ticker.C:
			c.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				if isConnectionError(err) {
					logger.Log().Debug("WebSocket client disconnected during ping", zap.Error(err))
				} else {
					logger.Log().Error("Failed to send ping", zap.Error(err))
				}
				return
			}
		}
	}
}

// Ensure UiDevHandler implements UiDevHandlerInterface at compile time
var _ ports.UiDevHandlerInterface = (*UiDevHandler)(nil)
