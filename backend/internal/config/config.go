package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port           string
	RedisAddr      string
	RedisPass      string
	ScyllaHosts    []string
	ScyllaKeyspace string
}

func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system env variables")
	}

	return &Config{
		Port:           getEnv("PORT", "8080"),
		RedisAddr:      getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPass:      getEnv("REDIS_PASS", ""),
		ScyllaHosts:    parseCSVEnv("SCYLLA_HOSTS", "127.0.0.1"),
		ScyllaKeyspace: getEnv("SCYLLA_KEYSPACE", "converse"),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func parseCSVEnv(key, fallback string) []string {
	value := getEnv(key, fallback)
	parts := strings.Split(value, ",")
	hosts := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			hosts = append(hosts, trimmed)
		}
	}
	if len(hosts) == 0 {
		return []string{"127.0.0.1"}
	}
	return hosts
}
