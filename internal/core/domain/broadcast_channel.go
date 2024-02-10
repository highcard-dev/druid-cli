package domain

type BroadcastChannel struct {
	// Registered clients.
	Clients map[chan *[]byte]bool

	// Inbound messages from the clients.
	Broadcast chan []byte

	// Register requests from the clients.
	Register chan chan *[]byte

	// Unregister requests from clients.
	Unregister chan chan *[]byte

	Close chan struct{}
}

func NewHub() *BroadcastChannel {
	return &BroadcastChannel{
		Broadcast:  make(chan []byte),
		Register:   make(chan chan *[]byte),
		Unregister: make(chan chan *[]byte),
		Close:      make(chan struct{}),
		Clients:    make(map[chan *[]byte]bool),
	}
}

func (h *BroadcastChannel) Run() {
	for {
		select {
		case client := <-h.Register:
			h.Clients[client] = true
		case client := <-h.Unregister:
			delete(h.Clients, client)
		case message := <-h.Broadcast:
			for client := range h.Clients {
				client <- &message
			}
		case <-h.Close:
			for client := range h.Clients {
				client <- nil
				delete(h.Clients, client)
				close(client)
			}
			return
		}
	}
}
