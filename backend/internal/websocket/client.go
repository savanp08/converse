package websocket

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/savanp08/converse/internal/models"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// dev only
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn
	Send     chan interface{}
	RoomID   string
	UserID   string
	Username string
}

func (c *Client) LoadHistory(ctx context.Context, service *MessageService) {
	if service == nil {
		log.Printf("[ws] history skipped room=%s reason=no_message_service", c.RoomID)
		return
	}

	history, err := service.GetRecentMessages(ctx, c.RoomID)
	if err != nil {
		log.Printf("[ws] history load error room=%s err=%v", c.RoomID, err)
		return
	}
	log.Printf("[ws] history loaded room=%s count=%d", c.RoomID, len(history))

	if len(history) == 0 {
		return
	}

	packet := map[string]interface{}{
		"type":    "history",
		"payload": history,
	}

	select {
	case c.Send <- packet:
		log.Printf("[ws] history sent room=%s count=%d", c.RoomID, len(history))
	case <-ctx.Done():
		log.Printf("[ws] history send canceled room=%s err=%v", c.RoomID, ctx.Err())
	default:
		log.Printf("[ws] history drop room=%s reason=send_buffer_full", c.RoomID)
	}
}

func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomId")
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		userID = "guest_" + time.Now().UTC().Format("20060102150405.000000000")
	}
	username := r.URL.Query().Get("username")
	if username == "" {
		username = "Guest"
	}
	log.Printf("[ws] upgrade requested room=%s remote=%s", roomID, r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed room=%s remote=%s err=%v", roomID, r.RemoteAddr, err)
		return
	}
	log.Printf("[ws] upgrade success room=%s remote=%s", roomID, r.RemoteAddr)

	client := &Client{
		Hub:      hub,
		Conn:     conn,
		Send:     make(chan interface{}, 256),
		RoomID:   roomID,
		UserID:   userID,
		Username: username,
	}
	client.Hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		var msg models.Message
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ws] read unexpected close room=%s err=%v", c.RoomID, err)
			} else {
				log.Printf("[ws] read closed room=%s err=%v", c.RoomID, err)
			}
			break
		}
		msg.CreatedAt = time.Now().UTC()
		msg.SenderID = c.UserID
		msg.SenderName = c.Username
		msg.RoomID = c.RoomID
		if msg.ID == "" {
			msg.ID = c.RoomID + "-" + msg.CreatedAt.Format(time.RFC3339Nano)
		}
		log.Printf("[ws] recv room=%s msg_id=%s sender=%s type=%s chars=%d",
			c.RoomID, msg.ID, msg.SenderID, msg.Type, len(msg.Content))
		c.Hub.broadcast <- msg
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case payload, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				log.Printf("[ws] send channel closed room=%s", c.RoomID)
				return
			}

			if err := c.Conn.WriteJSON(payload); err != nil {
				log.Printf("[ws] write json failed room=%s err=%v", c.RoomID, err)
				return
			}

			switch message := payload.(type) {
			case models.Message:
				log.Printf("[ws] sent room=%s msg_id=%s sender=%s type=%s", c.RoomID, message.ID, message.SenderID, message.Type)
			case map[string]interface{}:
				if packetType, ok := message["type"].(string); ok && packetType == "history" {
					log.Printf("[ws] sent history envelope room=%s", c.RoomID)
				}
			default:
				log.Printf("[ws] sent payload room=%s", c.RoomID)
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws] ping failed room=%s err=%v", c.RoomID, err)
				return
			}
		}
	}
}
