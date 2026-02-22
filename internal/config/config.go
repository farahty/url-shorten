package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port    string
	BaseURL string

	DatabaseURL string

	RedisURL      string
	RedisCacheTTL time.Duration

	ClickBufferSize    int
	ClickFlushInterval time.Duration

	OGScrapeTimeout time.Duration
	OGScrapeMaxBody int64

	AdminSecret string
}

func Load() *Config {
	return &Config{
		Port:               getEnv("PORT", "8080"),
		BaseURL:            getEnv("BASE_URL", "http://localhost:8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://user:pass@localhost:5432/shortener?sslmode=disable"),
		RedisURL:           getEnv("REDIS_URL", "redis://localhost:6379/0"),
		RedisCacheTTL:      time.Duration(getEnvInt("REDIS_CACHE_TTL", 3600)) * time.Second,
		ClickBufferSize:    getEnvInt("CLICK_BUFFER_SIZE", 1000),
		ClickFlushInterval: time.Duration(getEnvInt("CLICK_FLUSH_INTERVAL", 5)) * time.Second,
		OGScrapeTimeout:    time.Duration(getEnvInt("OG_SCRAPE_TIMEOUT", 5)) * time.Second,
		OGScrapeMaxBody:    int64(getEnvInt("OG_SCRAPE_MAX_BODY", 1048576)),
		AdminSecret:        getEnv("ADMIN_SECRET", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
