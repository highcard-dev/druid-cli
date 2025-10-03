package domain

import (
	"sync"

	"github.com/highcard-dev/daemon/internal/utils/logger"
)

type BroadcastChannel struct {
	// Registered clients.
	Clients map[chan *[]byte]bool

	// Inbound messages from the clients.
	Broadcast chan []byte

	// Mutex to protect concurrent access to Clients map
	mu sync.RWMutex
}

func NewHub() *BroadcastChannel {
	return &BroadcastChannel{
		Broadcast: make(chan []byte, 100), // Buffered channel to handle bursts of file changes
		Clients:   make(map[chan *[]byte]bool),
	}
}

func (h *BroadcastChannel) Subscribe() chan *[]byte {
	h.mu.Lock()
	defer h.mu.Unlock()

	client := make(chan *[]byte, 50) // Increased buffer size to handle message bursts
	h.Clients[client] = true
	return client
}

func (h *BroadcastChannel) Unsubscribe(client chan *[]byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.Clients[client]; exists {
		delete(h.Clients, client)
		close(client)
	}
}

func (h *BroadcastChannel) CloseChannel() {
	h.mu.Lock()
	defer h.mu.Unlock()

	close(h.Broadcast)
	for client := range h.Clients {
		close(client)
	}
	h.Clients = make(map[chan *[]byte]bool)
}

func (h *BroadcastChannel) Run() {
	for {
		message, more := <-h.Broadcast
		if !more {
			logger.Log().Debug("Broadcast channel closed")
			return
		}

		h.mu.RLock()
		clients := make([]chan *[]byte, 0, len(h.Clients))
		for client := range h.Clients {
			clients = append(clients, client)
		}
		h.mu.RUnlock()

		// Track clients to remove (dead connections)
		var deadClients []chan *[]byte

		for _, client := range clients {
			select {
			case client <- &message:
				// Successfully sent the message
			default:
				// Client channel is blocked or closed, mark for removal
				logger.Log().Debug("Client channel blocked or closed, marking for removal")
				deadClients = append(deadClients, client)
			}
		}

		// Remove dead clients
		if len(deadClients) > 0 {
			h.mu.Lock()
			for _, deadClient := range deadClients {
				if _, exists := h.Clients[deadClient]; exists {
					delete(h.Clients, deadClient)
					// Don't close the channel here as it might be closed elsewhere
					logger.Log().Debug("Removed dead client from broadcast channel")
				}
			}
			h.mu.Unlock()
		}
	}
}
