package websocket

import (
	"context"
	"encoding/json"
	"log"
	"time"

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
	hub := &Hub{
		broadcast:  make(chan models.Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		rooms:      make(map[string]map[*Client]bool),
		msgService: service,
	}

	if service != nil && service.CanPersistToDisk() {
		log.Printf("[hub] starting persistence worker")
		go hub.persistenceWorker()
	} else {
		log.Printf("[hub] persistence worker disabled (missing redis or scylla)")
	}

	return hub
}

func (h *Hub) persistenceWorker() {
	ctx := context.Background()

	for {
		if h.msgService == nil || !h.msgService.CanPersistToDisk() {
			time.Sleep(time.Second)
			continue
		}

		result, err := h.msgService.Redis.Client.BLPop(ctx, 0, messageQueueKey).Result()
		if err != nil {
			log.Printf("persistence worker pop error: %v", err)
			time.Sleep(time.Second)
			continue
		}
		if len(result) < 2 {
			continue
		}

		var msg models.Message
		if err := json.Unmarshal([]byte(result[1]), &msg); err != nil {
			log.Printf("persistence worker unmarshal error: %v", err)
			continue
		}

		if err := h.msgService.SaveToScylla(msg); err != nil {
			log.Printf("persistence worker save error: %v", err)
			if requeueErr := h.msgService.EnqueueMessage(ctx, msg); requeueErr != nil {
				log.Printf("persistence worker requeue error: %v", requeueErr)
			}
			time.Sleep(time.Second)
			continue
		}
		log.Printf("[hub] persisted msg room=%s msg_id=%s", msg.RoomID, msg.ID)
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			log.Printf("[hub] register room=%s", client.RoomID)
			if _, ok := h.rooms[client.RoomID]; !ok {
				h.rooms[client.RoomID] = make(map[*Client]bool)
			}
			h.rooms[client.RoomID][client] = true
			log.Printf("[hub] client joined room=%s active_clients=%d", client.RoomID, len(h.rooms[client.RoomID]))

			if h.msgService != nil {
				go client.LoadHistory(context.Background(), h.msgService)
			}

		case client := <-h.unregister:
			if _, ok := h.rooms[client.RoomID]; ok {
				if _, ok := h.rooms[client.RoomID][client]; ok {
					delete(h.rooms[client.RoomID], client)
					close(client.Send)
					log.Printf("[hub] client left room=%s active_clients=%d", client.RoomID, len(h.rooms[client.RoomID]))
				}
			}

		case msg := <-h.broadcast:
			log.Printf("[hub] broadcast recv room=%s msg_id=%s sender=%s type=%s", msg.RoomID, msg.ID, msg.SenderID, msg.Type)
			if msg.CreatedAt.IsZero() {
				msg.CreatedAt = time.Now().UTC()
			}

			if h.msgService != nil {
				go func(m models.Message) {
					if err := h.msgService.EnqueueMessage(context.Background(), m); err != nil {
						log.Printf("enqueue message error: %v", err)
					}
				}(msg)

				go func(m models.Message) {
					if err := h.msgService.CacheRecentMessage(context.Background(), m); err != nil {
						log.Printf("cache message error: %v", err)
					}
				}(msg)
			}

			if clients, ok := h.rooms[msg.RoomID]; ok {
				log.Printf("[hub] fanout room=%s recipients=%d msg_id=%s", msg.RoomID, len(clients), msg.ID)
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
