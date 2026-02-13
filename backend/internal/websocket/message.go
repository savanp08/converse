package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/savanp08/converse/internal/database"
	"github.com/savanp08/converse/internal/models"
)

const (
	messageQueueKey   = "msg_queue"
	roomHistoryPrefix = "room:history:"
	roomHistoryTTL    = 1800
	roomHistorySize   = 50
	scyllaMessageTTL  = 1296000
)

type MessageService struct {
	Redis  *database.RedisStore
	Scylla *database.ScyllaStore
}

func NewMessageService(redisStore *database.RedisStore, scyllaStore *database.ScyllaStore) *MessageService {
	return &MessageService{Redis: redisStore, Scylla: scyllaStore}
}

func (s *MessageService) CanPersistToDisk() bool {
	return s != nil && s.Redis != nil && s.Redis.Client != nil && s.Scylla != nil && s.Scylla.Session != nil
}

func (s *MessageService) EnqueueMessage(ctx context.Context, msg models.Message) error {
	if s.Redis == nil || s.Redis.Client == nil {
		return fmt.Errorf("redis client is not configured")
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if err := s.Redis.Client.RPush(ctx, messageQueueKey, payload).Err(); err != nil {
		return fmt.Errorf("enqueue message: %w", err)
	}
	log.Printf("[message-service] enqueue room=%s msg_id=%s", msg.RoomID, msg.ID)

	return nil
}

func (s *MessageService) SaveToScylla(msg models.Message) error {
	if s.Scylla == nil || s.Scylla.Session == nil {
		return fmt.Errorf("scylla session is not configured")
	}

	if err := s.Scylla.Session.Query(
		`INSERT INTO messages (room_id, message_id, sender_id, sender_name, content, type, created_at) VALUES (?, ?, ?, ?, ?, ?, ?) USING TTL 1296000`,
		msg.RoomID,
		msg.ID,
		msg.SenderID,
		msg.SenderName,
		msg.Content,
		msg.Type,
		msg.CreatedAt,
	).Exec(); err != nil {
		return fmt.Errorf("save to scylla: %w", err)
	}
	log.Printf("[message-service] scylla saved room=%s msg_id=%s ttl_seconds=%d", msg.RoomID, msg.ID, scyllaMessageTTL)

	return nil
}

func (s *MessageService) CacheRecentMessage(ctx context.Context, msg models.Message) error {
	if s.Redis == nil || s.Redis.Client == nil {
		return fmt.Errorf("redis client is not configured")
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	historyKey := roomHistoryPrefix + msg.RoomID
	pipe := s.Redis.Client.TxPipeline()
	pipe.RPush(ctx, historyKey, payload)
	pipe.LTrim(ctx, historyKey, -roomHistorySize, -1)
	pipe.Expire(ctx, historyKey, roomHistoryTTL*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("cache recent message: %w", err)
	}
	log.Printf("[message-service] cache recent room=%s msg_id=%s", msg.RoomID, msg.ID)

	return nil
}

func (s *MessageService) GetRecentMessages(ctx context.Context, roomID string) ([]models.Message, error) {
	if roomID == "" {
		return []models.Message{}, nil
	}

	redisMessages := make([]models.Message, 0, roomHistorySize)
	if s.Redis != nil && s.Redis.Client != nil {
		historyKey := roomHistoryPrefix + roomID
		cached, err := s.Redis.Client.LRange(ctx, historyKey, 0, -1).Result()
		if err != nil {
			return nil, fmt.Errorf("load cached history: %w", err)
		}

		redisMessages = decodeCachedMessages(cached)
		if len(redisMessages) > roomHistorySize {
			redisMessages = redisMessages[len(redisMessages)-roomHistorySize:]
		}
		log.Printf("[message-service] redis history room=%s count=%d", roomID, len(redisMessages))
	}

	if len(redisMessages) >= roomHistorySize {
		return redisMessages, nil
	}

	needed := roomHistorySize - len(redisMessages)
	if s.Scylla == nil || s.Scylla.Session == nil {
		return redisMessages, nil
	}

	scyllaMessagesDesc, err := s.queryScyllaMessages(roomID, needed)
	if err != nil {
		return nil, err
	}
	log.Printf("[message-service] scylla supplement room=%s needed=%d count=%d", roomID, needed, len(scyllaMessagesDesc))

	for left, right := 0, len(scyllaMessagesDesc)-1; left < right; left, right = left+1, right-1 {
		scyllaMessagesDesc[left], scyllaMessagesDesc[right] = scyllaMessagesDesc[right], scyllaMessagesDesc[left]
	}

	combined := append(scyllaMessagesDesc, redisMessages...)
	combined = dedupeChronological(combined)
	if len(combined) > roomHistorySize {
		combined = combined[len(combined)-roomHistorySize:]
	}

	return combined, nil
}

func decodeCachedMessages(rawMessages []string) []models.Message {
	messages := make([]models.Message, 0, len(rawMessages))
	for _, raw := range rawMessages {
		var msg models.Message
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages
}

func dedupeChronological(messages []models.Message) []models.Message {
	seen := make(map[string]struct{}, len(messages))
	result := make([]models.Message, 0, len(messages))
	for _, msg := range messages {
		key := msg.ID
		if key == "" {
			key = fmt.Sprintf("%s|%s|%d|%s", msg.RoomID, msg.SenderID, msg.CreatedAt.UnixNano(), msg.Content)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, msg)
	}
	return result
}

func (s *MessageService) queryScyllaMessages(roomID string, limit int) ([]models.Message, error) {
	if limit <= 0 {
		return []models.Message{}, nil
	}

	iter := s.Scylla.Session.Query(
		`SELECT room_id, message_id, sender_id, sender_name, content, type, created_at FROM messages WHERE room_id = ? ORDER BY created_at DESC LIMIT ?`,
		roomID,
		limit,
	).Iter()

	messages := make([]models.Message, 0, limit)
	var dbRoomID string
	var messageID string
	var senderID string
	var senderName string
	var content string
	var msgType string
	var createdAt time.Time

	for iter.Scan(&dbRoomID, &messageID, &senderID, &senderName, &content, &msgType, &createdAt) {
		messages = append(messages, models.Message{
			ID:         messageID,
			RoomID:     dbRoomID,
			SenderID:   senderID,
			SenderName: senderName,
			Content:    content,
			Type:       msgType,
			CreatedAt:  createdAt,
		})
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("load scylla history: %w", err)
	}

	return messages, nil
}
