package utils

import (
	"context"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

// WebSocketConnection is a lean wrapper for WebSocket connections with basic safety
type WebSocketConnection struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWebSocketConnection creates a managed WebSocket connection
func NewWebSocketConnection(c *websocket.Conn) *WebSocketConnection {
	ctx, cancel := context.WithCancel(context.Background())

	wsc := &WebSocketConnection{
		conn:   c,
		ctx:    ctx,
		cancel: cancel,
	}

	// Simple connection monitoring
	go wsc.monitor()

	return wsc
}

// Close safely closes the connection
func (wsc *WebSocketConnection) Close() {
	wsc.cancel()
	wsc.conn.Close()
}

// WriteJSON writes JSON with timeout
func (wsc *WebSocketConnection) WriteJSON(v interface{}) error {
	wsc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return wsc.conn.WriteJSON(v)
}

// WritePing sends a ping
func (wsc *WebSocketConnection) WritePing() error {
	wsc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return wsc.conn.WriteMessage(websocket.PingMessage, nil)
}

// WriteMessage writes a message
func (wsc *WebSocketConnection) WriteMessage(messageType int, data []byte) error {
	wsc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return wsc.conn.WriteMessage(messageType, data)
}

// Context returns the connection context
func (wsc *WebSocketConnection) Context() context.Context {
	return wsc.ctx
}

// monitor detects disconnections
func (wsc *WebSocketConnection) monitor() {
	wsc.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	wsc.conn.SetPongHandler(func(string) error {
		wsc.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		select {
		case <-wsc.ctx.Done():
			return
		default:
			_, _, err := wsc.conn.ReadMessage()
			if err != nil {
				logger.Log().Debug("WebSocket disconnected", zap.Error(err))
				wsc.cancel()
				return
			}
		}
	}
}
