package domain

import (
	"sync"
	"time"
)

type BroadcastChannel struct {
	clients   map[chan *[]byte]bool
	broadcast chan []byte
	mu        sync.RWMutex
	closed    bool
}

func NewHub() *BroadcastChannel {
	return &BroadcastChannel{
		broadcast: make(chan []byte, 100), // Buffered to prevent blocking
		clients:   make(map[chan *[]byte]bool),
	}
}

func (h *BroadcastChannel) Subscribe() chan *[]byte {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	client := make(chan *[]byte, 25) // Reasonable buffer
	h.clients[client] = true
	return client
}

func (h *BroadcastChannel) Unsubscribe(client chan *[]byte) {
	if client == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.clients[client]; exists {
		delete(h.clients, client)
		close(client)
	}
}

// Broadcast sends data to all clients, returns false if dropped
func (h *BroadcastChannel) Broadcast(data []byte) bool {
	if h.closed {
		return false
	}

	select {
	case h.broadcast <- data:
		return true
	case <-time.After(50 * time.Millisecond): // Quick timeout
		return false
	}
}

func (h *BroadcastChannel) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}

	h.closed = true
	close(h.broadcast)

	for client := range h.clients {
		close(client)
	}
	h.clients = make(map[chan *[]byte]bool)
}

func (h *BroadcastChannel) Run() {
	defer h.Close()

	for data := range h.broadcast {
		h.mu.RLock()
		for client := range h.clients {
			select {
			case client <- &data:
				// Sent successfully
			default:
				// Client blocked, skip (will be cleaned up later if persistent)
			}
		}
		h.mu.RUnlock()
	}
}
