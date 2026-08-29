package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	DBDSN     string
	BindAddr  string
	Port      string
	JWTSecret string
	AppEnv    string
}

// Load reads configuration from the environment.
// In development, it also loads a .env file if present.
func Load() (*Config, error) {
	// .env is optional — ignore the error (file won't exist in production containers)
	_ = godotenv.Load()

	cfg := &Config{
		DBDSN: getEnv("DB_DSN", "esaproperti:changeme@tcp(localhost:3306)/esaproperti?parseTime=true&loc=Local&charset=utf8mb4&collation=utf8mb4_unicode_ci"),
		// BIND_ADDR default 127.0.0.1: aman secara default — saat dijalankan
		// langsung di host (bukan container), server HANYA menerima koneksi dari
		// komputer ini, tidak dari LAN/WiFi. Di dalam container di-override
		// menjadi 0.0.0.0 (lihat docker-compose*.yml) agar port-forward jalan.
		BindAddr:  getEnv("BIND_ADDR", "127.0.0.1"),
		Port:      getEnv("PORT", "8080"),
		JWTSecret: os.Getenv("JWT_SECRET"),
		AppEnv:    getEnv("APP_ENV", "development"),
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET environment variable is required")
	}

	return cfg, nil
}

func (c *Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
