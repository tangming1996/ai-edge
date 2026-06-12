package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/deployment"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

func main() {
	cfg := store.Config{
		Host:     envOrDefault("DB_HOST", "localhost"),
		Port:     envOrDefaultInt("DB_PORT", 5432),
		User:     envOrDefault("DB_USER", "postgres"),
		Password: envOrDefault("DB_PASSWORD", "postgres"),
		DBName:   envOrDefault("DB_NAME", "edgeai"),
		SSLMode:  envOrDefault("DB_SSLMODE", "disable"),
	}

	db, err := store.New(cfg)
	if err != nil {
		log.Fatalf("controller: connect database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("controller: close database: %v", err)
		}
	}()

	pollInterval := envOrDefaultDuration("POLL_INTERVAL", 10*time.Second)
	taskCreator := deployment.NewDeploymentTaskCreator(db)
	controller := deployment.NewController(db, taskCreator, deployment.ControllerConfig{
		PollInterval: pollInterval,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("controller: started (poll_interval=%s)", pollInterval)
	controller.Run(ctx)
	log.Println("controller: stopped")
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envOrDefaultDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
