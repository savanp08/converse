package websocket

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/savanp08/converse/internal/database"
	"github.com/savanp08/converse/internal/models"
)

type MessageService struct {
	Redis  *database.RedisStore
	Scylla *database.ScyllaStore
}

func NewMessageService(redisStore *database.RedisStore, scyllaStore *database.ScyllaStore) *MessageService {
	return &MessageService{Redis: redisStore, Scylla: scyllaStore}
}

func (s *MessageService) Save(ctx context.Context, msg models.Message) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	// hot path
	if s.Redis != nil {
		if err := s.Redis.Publish(ctx, msg.RoomID, payload); err != nil {
			return fmt.Errorf("publish hot path: %w", err)
		}
	}

	// cold path
	if s.Scylla != nil {
		if err := s.Scylla.Session.Query(
			`INSERT INTO messages (room_id, message_id, sender_id, sender_name, content, type, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			msg.RoomID,
			msg.ID,
			msg.SenderID,
			msg.SenderName,
			msg.Content,
			msg.Type,
			msg.CreatedAt,
		).Exec(); err != nil {
			return fmt.Errorf("persist cold path: %w", err)
		}
	}

	return nil
}
