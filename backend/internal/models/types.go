package models

import "time"

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Message struct {
	ID         string    `json:"id" cql:"message_id"`
	RoomID     string    `json:"roomId" cql:"room_id"`
	SenderID   string    `json:"userId" cql:"sender_id"`
	SenderName string    `json:"username" cql:"sender_name"`
	Content    string    `json:"text" cql:"content"`
	Type       string    `json:"type" cql:"type"`
	CreatedAt  time.Time `json:"time" cql:"created_at"`
}

type Room struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Participants []string  `json:"participants"`
	CreatedAt    time.Time `json:"createdAt"`
}
