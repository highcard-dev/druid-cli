package domain

import (
	"github.com/highcard-dev/logger"
)

type BroadcastChannel struct {
	// Registered clients.
	Clients map[chan *[]byte]bool

	// Inbound messages from the clients.
	Broadcast chan []byte
}

func NewHub() *BroadcastChannel {
	return &BroadcastChannel{
		Broadcast: make(chan []byte),
		Clients:   make(map[chan *[]byte]bool),
	}
}

func (h *BroadcastChannel) Subscribe() chan *[]byte {
	client := make(chan *[]byte, 10) // buffered channel to avoid blocking
	h.Clients[client] = true
	return client
}

func (h *BroadcastChannel) Unsubscribe(client chan *[]byte) {
	delete(h.Clients, client)
	close(client)
}

func (h *BroadcastChannel) CloseChannel() {
	close(h.Broadcast)
	for client := range h.Clients {
		h.Unsubscribe(client)
	}
}

func (h *BroadcastChannel) Run() {
	for {
		message, more := <-h.Broadcast
		if !more {
			logger.Log().Debug("Broadcast channel closed")
			return
		}
		for client := range h.Clients {
			select {
			case client <- &message: // Try to send the message.
			default:
				logger.Log().Warn("Failed to Broadcast message to channel.. closing channel")
				//h.Unsubscribe(client)
			}
		}
	}
}
