package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/savanp08/converse/internal/database"
)

var roomSuffixWords = []string{
	"hub", "zone", "chat", "base", "net",
	"talk", "lounge", "pulse", "nest", "crew",
	"loop", "dock", "den", "forge", "space",
	"spot", "sync", "stream", "wave", "link",
}

type RoomHandler struct {
	redis *database.RedisStore
}

func NewRoomHandler(redisStore *database.RedisStore) *RoomHandler {
	return &RoomHandler{redis: redisStore}
}

type JoinRoomRequest struct {
	RoomName string `json:"roomName"`
	Username string `json:"username"`
	Type     string `json:"type"`
}

type JoinRoomResponse struct {
	RoomID   string `json:"roomId"`
	RoomName string `json:"roomName"`
	UserID   string `json:"userId"`
	Token    string `json:"token"`
}

func (h *RoomHandler) JoinRoom(w http.ResponseWriter, r *http.Request) {
	var req JoinRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON format"})
		return
	}

	if strings.TrimSpace(req.RoomName) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room name cannot be empty"})
		return
	}

	baseSlug := slugifyRoomName(req.RoomName)
	if baseSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room name must contain letters or numbers"})
		return
	}

	roomType := strings.TrimSpace(req.Type)
	if roomType == "" {
		roomType = "ephemeral"
	}

	userID := fmt.Sprintf("user_%d", time.Now().UnixNano())
	token := "temp_token_for_" + req.Username
	ctx := context.Background()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	createdAt := time.Now().Unix()

	finalRoomID := baseSlug
	finalRoomName := baseSlug

	created, err := h.tryCreateRoom(ctx, baseSlug, baseSlug, roomType, createdAt)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to access room storage"})
		return
	}

	if created {
		finalRoomID = baseSlug
		finalRoomName = baseSlug
	} else {
		// suffix retry
		suffixOrder := rng.Perm(len(roomSuffixWords))
		for i := 0; i < 3 && i < len(suffixOrder); i++ {
			candidateID := fmt.Sprintf("%s-%s", baseSlug, roomSuffixWords[suffixOrder[i]])
			candidateName := candidateID

			created, err = h.tryCreateRoom(ctx, candidateID, candidateName, roomType, createdAt)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Failed to access room storage"})
				return
			}

			if created {
				finalRoomID = candidateID
				finalRoomName = candidateName
				break
			}
		}

		// numeric fallback
		if !created {
			fallbackID := fmt.Sprintf("%s-%d", baseSlug, rng.Intn(9000)+1000)
			fallbackName := fallbackID

			if err := h.createRoom(ctx, fallbackID, fallbackName, roomType, createdAt); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create room"})
				return
			}

			finalRoomID = fallbackID
			finalRoomName = fallbackName
		}
	}

	response := JoinRoomResponse{
		RoomID:   finalRoomID,
		RoomName: finalRoomName,
		UserID:   userID,
		Token:    token,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func slugifyRoomName(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return ""
	}

	var builder strings.Builder
	prevHyphen := false

	for _, ch := range normalized {
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9'):
			builder.WriteRune(ch)
			prevHyphen = false
		case ch == ' ' || ch == '-' || ch == '_':
			if builder.Len() > 0 && !prevHyphen {
				builder.WriteByte('-')
				prevHyphen = true
			}
		}
	}

	return strings.Trim(builder.String(), "-")
}

func (h *RoomHandler) tryCreateRoom(ctx context.Context, roomID, roomName, roomType string, createdAt int64) (bool, error) {
	exists, err := h.roomExists(ctx, roomID)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	if err := h.createRoom(ctx, roomID, roomName, roomType, createdAt); err != nil {
		return false, err
	}

	return true, nil
}

func (h *RoomHandler) roomExists(ctx context.Context, roomID string) (bool, error) {
	count, err := h.redis.Client.Exists(ctx, "room:"+roomID).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *RoomHandler) createRoom(ctx context.Context, roomID, roomName, roomType string, createdAt int64) error {
	return h.redis.Client.HSet(ctx, "room:"+roomID, map[string]interface{}{
		"id":         roomID,
		"name":       roomName,
		"type":       roomType,
		"created_at": createdAt,
	}).Err()
}

func (h *RoomHandler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}
