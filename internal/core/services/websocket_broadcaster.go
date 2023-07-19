package services

// WebsocketBroadcaster maintains the set of active clients and broadcasts messages to the
// clients.
type WebsocketBroadcaster struct {
	// Registered clients.
	clients map[*WebsocketClient]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *WebsocketClient

	// Unregister requests from clients.
	unregister chan *WebsocketClient
}

func NewHub() *WebsocketBroadcaster {
	return &WebsocketBroadcaster{
		broadcast:  make(chan []byte),
		register:   make(chan *WebsocketClient),
		unregister: make(chan *WebsocketClient),
		clients:    make(map[*WebsocketClient]bool),
	}
}

func (h *WebsocketBroadcaster) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			delete(h.clients, client)
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
		}
	}
}
