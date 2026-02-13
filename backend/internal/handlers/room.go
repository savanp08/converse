package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/savanp08/converse/internal/database"
)

const (
	roomKeyTTL       = 24 * time.Hour
	roomMaxExtendAge = 14 * 24 * time.Hour
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

type ExtendRoomRequest struct {
	RoomID string `json:"roomId"`
}

type ExtendRoomResponse struct {
	RoomID           string `json:"roomId"`
	ExpiresInSeconds int64  `json:"expiresInSeconds"`
	Message          string `json:"message"`
}

func (h *RoomHandler) JoinRoom(w http.ResponseWriter, r *http.Request) {
	var req JoinRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON format"})
		return
	}
	log.Printf("[room] join requested raw_room=%q username=%q type=%q", req.RoomName, req.Username, req.Type)

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

	if !created {
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
	}

	if !created {
		for attempts := 0; attempts < 10; attempts++ {
			fallbackID := fmt.Sprintf("%s-%d", baseSlug, rng.Intn(9000)+1000)
			fallbackName := fallbackID

			created, err = h.tryCreateRoom(ctx, fallbackID, fallbackName, roomType, createdAt)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Failed to access room storage"})
				return
			}

			if created {
				finalRoomID = fallbackID
				finalRoomName = fallbackName
				break
			}
		}
	}
	log.Printf("[room] join resolved room_id=%s room_name=%s user_id=%s", finalRoomID, finalRoomName, userID)

	if !created {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to allocate unique room name"})
		return
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

func (h *RoomHandler) ExtendRoom(w http.ResponseWriter, r *http.Request) {
	var req ExtendRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON format"})
		return
	}
	log.Printf("[room] extend requested room_id=%q", req.RoomID)

	roomID := slugifyRoomName(req.RoomID)
	if roomID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "roomId is required"})
		return
	}

	ctx := context.Background()
	roomKey := "room:" + roomID

	exists, err := h.redis.Client.Exists(ctx, roomKey).Result()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to access room storage"})
		return
	}
	if exists == 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room not found"})
		return
	}

	createdAtRaw, err := h.redis.Client.HGet(ctx, roomKey, "created_at").Result()
	if err == redis.Nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room metadata is incomplete"})
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read room metadata"})
		return
	}

	createdAtUnix, err := strconv.ParseInt(createdAtRaw, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room metadata is invalid"})
		return
	}

	age := time.Since(time.Unix(createdAtUnix, 0))
	if age >= roomMaxExtendAge {
		log.Printf("[room] extend denied room_id=%s age_hours=%.2f", roomID, age.Hours())
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room has reached its 15-day limit"})
		return
	}

	if err := h.redis.Client.Expire(ctx, roomKey, roomKeyTTL).Err(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to extend room"})
		return
	}
	log.Printf("[room] extend success room_id=%s ttl_seconds=%d", roomID, int64(roomKeyTTL.Seconds()))

	response := ExtendRoomResponse{
		RoomID:           roomID,
		ExpiresInSeconds: int64(roomKeyTTL.Seconds()),
		Message:          "Room extended",
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
	roomKey := "room:" + roomID
	if err := h.redis.Client.HSet(ctx, roomKey, map[string]interface{}{
		"id":         roomID,
		"name":       roomName,
		"type":       roomType,
		"created_at": createdAt,
	}).Err(); err != nil {
		return err
	}

	if err := h.redis.Client.Expire(ctx, roomKey, roomKeyTTL).Err(); err != nil {
		return err
	}

	return nil
}

func (h *RoomHandler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}
