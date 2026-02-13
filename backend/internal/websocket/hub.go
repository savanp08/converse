package websocket

import (
	"context"
	"log"

	"github.com/savanp08/converse/internal/models"
)

type Hub struct {
	rooms      map[string]map[*Client]bool
	broadcast  chan models.Message
	register   chan *Client
	unregister chan *Client

	msgService *MessageService
}

func NewHub(service *MessageService) *Hub {
	return &Hub{
		broadcast:  make(chan models.Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		rooms:      make(map[string]map[*Client]bool),
		msgService: service,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			if _, ok := h.rooms[client.RoomID]; !ok {
				h.rooms[client.RoomID] = make(map[*Client]bool)
			}
			h.rooms[client.RoomID][client] = true
			log.Printf("Client joined room: %s", client.RoomID)

		case client := <-h.unregister:
			if _, ok := h.rooms[client.RoomID]; ok {
				if _, ok := h.rooms[client.RoomID][client]; ok {
					delete(h.rooms[client.RoomID], client)
					close(client.Send)
				}
			}

		case msg := <-h.broadcast:
			// save async
			go func(m models.Message) {
				if err := h.msgService.Save(context.Background(), m); err != nil {
					log.Printf("Error saving message: %v", err)
				}
			}(msg)

			// room fanout
			if clients, ok := h.rooms[msg.RoomID]; ok {
				for client := range clients {
					select {
					case client.Send <- msg:
					default:
						close(client.Send)
						delete(clients, client)
					}
				}
			}
		}
	}
}
