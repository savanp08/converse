package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/savanp08/converse/internal/models"
)

type AuthHandler struct{}

type AuthRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username"`
}

type AuthResponse struct {
	User  models.User `json:"user"`
	Token string      `json:"token"`
}

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{}
}

func (h *AuthHandler) SignUp(w http.ResponseWriter, r *http.Request) {
	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAuthError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	password := strings.TrimSpace(req.Password)
	username := strings.TrimSpace(req.Username)
	log.Printf("[auth] signup requested email=%q username=%q", email, username)

	if email == "" || password == "" || username == "" {
		writeAuthError(w, http.StatusBadRequest, "Email, password, and username are required")
		return
	}

	response, err := buildAuthResponse(email, username)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "Failed to generate auth token")
		return
	}

	writeAuthJSON(w, http.StatusCreated, response)
	log.Printf("[auth] signup success user_id=%s username=%q", response.User.ID, response.User.Username)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAuthError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	password := strings.TrimSpace(req.Password)
	username := strings.TrimSpace(req.Username)
	log.Printf("[auth] login requested email=%q username=%q", email, username)

	if email == "" || password == "" {
		writeAuthError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	if username == "" {
		username = usernameFromEmail(email)
	}

	response, err := buildAuthResponse(email, username)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "Failed to generate auth token")
		return
	}

	writeAuthJSON(w, http.StatusOK, response)
	log.Printf("[auth] login success user_id=%s username=%q", response.User.ID, response.User.Username)
}

func buildAuthResponse(email, username string) (AuthResponse, error) {
	token, err := newToken()
	if err != nil {
		return AuthResponse{}, err
	}

	user := models.User{
		ID:        fmt.Sprintf("user_%d", time.Now().UnixNano()),
		Username:  username,
		Email:     email,
		CreatedAt: time.Now().UTC(),
	}

	return AuthResponse{User: user, Token: token}, nil
}

func writeAuthJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAuthError(w http.ResponseWriter, code int, message string) {
	writeAuthJSON(w, code, map[string]string{"error": message})
}

func newToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func usernameFromEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "Guest"
	}
	return parts[0]
}
