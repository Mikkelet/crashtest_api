package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	ListenAddr  string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	return &Config{
		DatabaseURL: dbURL,
		ListenAddr:  addr,
	}, nil
}
