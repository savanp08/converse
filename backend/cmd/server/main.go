package main

import (
	"log"
	"net/http"

	"github.com/savanp08/converse/internal/config"
	"github.com/savanp08/converse/internal/database"
	"github.com/savanp08/converse/internal/router"
	"github.com/savanp08/converse/internal/websocket"
)

func main() {
	cfg := config.LoadConfig()
	log.Println("🚀 Starting Converse Backend...")

	redisStore, err := database.NewRedisStore(cfg.RedisAddr, cfg.RedisPass)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisStore.Close()
	log.Println("✅ Connected to Redis")

	var scyllaStore *database.ScyllaStore
	if len(cfg.ScyllaHosts) > 0 {
		scyllaStore, err = database.NewScyllaStore(*cfg)
		if err != nil {
			log.Printf("⚠️  Warning: Could not connect to ScyllaDB: %v", err)
			log.Println("   (Running in 'Ephemeral Only' mode)")
		} else {
			defer scyllaStore.Close()
			log.Println("✅ Connected to ScyllaDB")
		}
	}

	msgService := websocket.NewMessageService(redisStore, scyllaStore)
	hub := websocket.NewHub(msgService)
	go hub.Run()

	mainRouter := router.New(hub, redisStore)

	log.Printf("📡 Server listening on port %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mainRouter); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
